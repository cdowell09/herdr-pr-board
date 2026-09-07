// Package reviewmemory owns local review history and process coordination.
package reviewmemory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"golang.org/x/sys/unix"
)

var (
	ErrActive        = errors.New("revision has an active review")
	ErrReviewed      = errors.New("revision already has a completed review")
	ErrRetryRequired = errors.New("previous attempt requires explicit retry")
	ErrCapacity      = errors.New("review concurrency limit reached")
	ErrOwnership     = errors.New("review claim is no longer owned")
)

type Identity struct {
	Repository  string `json:"repository"`
	Number      int    `json:"number"`
	HeadOID     string `json:"head_oid"`
	BaseRefName string `json:"base_ref_name"`
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

func validOID(oid string) bool {
	decoded, err := hex.DecodeString(oid)
	return err == nil && len(decoded) == 20 && oid == strings.ToLower(oid)
}

func (id Identity) valid() bool {
	return repositoryPattern.MatchString(id.Repository) && id.Number > 0 && validOID(id.HeadOID) && strings.TrimSpace(id.BaseRefName) != "" && !strings.HasPrefix(id.BaseRefName, "-") && !strings.ContainsAny(id.BaseRefName, " \t\r\n")
}

type Status string

const (
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Blocked   Status = "blocked"
	Abandoned Status = "abandoned"
)

type Finding struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
}

type Outcome struct {
	Status   Status    `json:"status"`
	Findings []Finding `json:"findings"`
	Message  string    `json:"message,omitempty"`
}

type Run struct {
	ID         string     `json:"id"`
	Identity   Identity   `json:"identity"`
	BaseOID    string     `json:"base_oid"`
	Reviewer   string     `json:"reviewer"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Outcome
}

type Request struct {
	Identity      Identity
	BaseOID       string
	Reviewer      string
	Rerun         bool
	MaxConcurrent int
}

type Store struct{ dir string }

// Open requires the installation's HERDR_PLUGIN_STATE_DIR, never its program directory.
func Open(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" || !filepath.IsAbs(stateDir) {
		return nil, errors.New("absolute plugin state directory is required")
	}
	dir := filepath.Join(stateDir, "reviews")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

type history struct {
	Version int   `json:"version"`
	Runs    []Run `json:"runs"`
}

// transaction also recovers claims whose owning processes have exited.
// ponytail: history scans are O(n); use indexed storage if measured history size requires it.
func (s *Store) transaction(fn func(*history) error) error {
	lock, err := localstate.Lock(context.Background(), filepath.Join(s.dir, "history.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	var h history
	data, err := os.ReadFile(filepath.Join(s.dir, "history.json"))
	if err == nil {
		if err := json.Unmarshal(data, &h); err != nil {
			return fmt.Errorf("invalid review history: %w", err)
		}
		if err := validateHistory(h); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		h = history{Version: 1, Runs: []Run{}}
	} else {
		return err
	}
	before, err := json.Marshal(h)
	if err != nil {
		return err
	}
	for i := range h.Runs {
		r := &h.Runs[i]
		if r.Status != Running {
			continue
		}
		owner, err := localstate.TryLock(s.runLock(r.ID))
		if errors.Is(err, localstate.ErrLocked) {
			continue
		}
		if err != nil {
			return err
		}
		owner.Close()
		now := time.Now().UTC()
		r.Status, r.FinishedAt, r.Message = Abandoned, &now, "review owner exited without a result"
	}
	operationErr := fn(&h)
	after, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		if err := localstate.AtomicWrite(filepath.Join(s.dir, "history.json"), after); err != nil {
			return err
		}
	}
	return operationErr
}

func validateHistory(h history) error {
	if h.Version != 1 || h.Runs == nil {
		return errors.New("invalid review history version or runs")
	}
	seen := map[string]bool{}
	for _, r := range h.Runs {
		id, err := hex.DecodeString(r.ID)
		if err != nil || len(id) != 16 || seen[r.ID] || !r.Identity.valid() || !validOID(r.BaseOID) || strings.TrimSpace(r.Reviewer) == "" || r.StartedAt.IsZero() {
			return errors.New("invalid review run")
		}
		seen[r.ID] = true
		if r.Status == Running {
			if r.FinishedAt != nil || len(r.Findings) != 0 {
				return errors.New("invalid active review run")
			}
		} else if r.FinishedAt == nil || r.FinishedAt.Before(r.StartedAt) || !validOutcome(r.Outcome) {
			return errors.New("invalid review outcome")
		}
	}
	return nil
}

func validOutcome(o Outcome) bool {
	for _, f := range o.Findings {
		if f.Severity != "P0" && f.Severity != "P1" && f.Severity != "P2" && f.Severity != "P3" {
			return false
		}
		if strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Body) == "" || f.Line < 0 || (f.Line > 0 && strings.TrimSpace(f.Path) == "") {
			return false
		}
	}
	switch o.Status {
	case Completed:
		return o.Findings != nil && strings.TrimSpace(o.Message) != ""
	case Failed, Blocked, Abandoned:
		return strings.TrimSpace(o.Message) != ""
	default:
		return false
	}
}

// ValidateIdentity checks the revision identity before a claim or reviewer invocation.
func ValidateIdentity(id Identity) error {
	if !id.valid() {
		return errors.New("invalid review identity")
	}
	return nil
}

// ValidateRevision checks the captured head, target, and comparison commit.
func ValidateRevision(id Identity, baseOID string) error {
	if err := ValidateIdentity(id); err != nil {
		return err
	}
	if !validOID(baseOID) {
		return errors.New("invalid comparison commit")
	}
	return nil
}

// ValidateOutcome validates a terminal reviewer result independently of execution.
func ValidateOutcome(outcome Outcome) error {
	if !validOutcome(outcome) || outcome.Status == Abandoned {
		return errors.New("invalid review outcome")
	}
	return nil
}

func (s *Store) runLock(id string) string { return filepath.Join(s.dir, id+".lock") }

// History returns attempts for this exact revision, oldest first.
func (s *Store) History(id Identity) ([]Run, error) {
	if !id.valid() {
		return nil, errors.New("invalid review identity")
	}
	id.Repository = strings.ToLower(id.Repository)
	runs := []Run{}
	err := s.transaction(func(h *history) error {
		for _, r := range h.Runs {
			if r.Identity == id {
				runs = append(runs, r)
			}
		}
		return nil
	})
	return runs, err
}

// PRHistory returns all revisions for one PR, oldest first.
func (s *Store) PRHistory(repository string, number int) ([]Run, error) {
	if !repositoryPattern.MatchString(repository) || number <= 0 {
		return nil, errors.New("invalid PR identity")
	}
	repository = strings.ToLower(repository)
	runs := []Run{}
	err := s.transaction(func(h *history) error {
		for _, r := range h.Runs {
			if r.Identity.Repository == repository && r.Identity.Number == number {
				runs = append(runs, r)
			}
		}
		return nil
	})
	return runs, err
}

type Claim struct {
	mu    sync.Mutex
	store *Store
	id    string
	file  *os.File
}

// Claim atomically reserves a revision and an installation-wide concurrency slot.
// Zero MaxConcurrent defaults to one. Rerun bypasses history, never active claims.
func (s *Store) Claim(req Request) (*Claim, error) {
	if !req.Identity.valid() || !validOID(req.BaseOID) || strings.TrimSpace(req.Reviewer) == "" || req.MaxConcurrent < 0 {
		return nil, errors.New("invalid review request")
	}
	req.Identity.Repository = strings.ToLower(req.Identity.Repository)
	limit := max(1, req.MaxConcurrent)
	c := &Claim{store: s}
	err := s.transaction(func(h *history) error {
		active := 0
		var previous error
		for _, r := range h.Runs {
			if r.Status == Running {
				active++
			}
			if r.Identity != req.Identity {
				continue
			}
			if r.Status == Running {
				return ErrActive
			}
			if r.Status == Completed {
				previous = ErrReviewed
			} else if previous == nil {
				previous = ErrRetryRequired
			}
		}
		if !req.Rerun && previous != nil {
			return previous
		}
		if active >= limit {
			return ErrCapacity
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		c.id = hex.EncodeToString(token[:])
		var err error
		c.file, err = localstate.TryLock(s.runLock(c.id))
		if err != nil {
			return err
		}
		h.Runs = append(h.Runs, Run{ID: c.id, Identity: req.Identity, BaseOID: req.BaseOID, Reviewer: req.Reviewer, StartedAt: time.Now().UTC(), Outcome: Outcome{Status: Running}})
		return nil
	})
	if err != nil {
		if c.file != nil {
			c.file.Close()
		}
		return nil, err
	}
	return c, nil
}

func (c *Claim) ID() string { return c.id }

// LockFile duplicates the ownership descriptor for a reviewer's exec.Cmd.ExtraFiles.
// Close the returned descriptor after Start. The child must retain its inherited descriptor.
func (c *Claim) LockFile() (*os.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return nil, ErrOwnership
	}
	fd, err := unix.FcntlInt(c.file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), c.file.Name()), nil
}

// Finish records an outcome only while this handle owns its active attempt.
// The caller validates the reviewer contract before supplying completed findings.
func (c *Claim) Finish(outcome Outcome) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return ErrOwnership
	}
	if !validOutcome(outcome) || outcome.Status == Abandoned {
		return errors.New("invalid review outcome")
	}
	err := c.store.transaction(func(h *history) error {
		for i := range h.Runs {
			r := &h.Runs[i]
			if r.ID != c.id {
				continue
			}
			if r.Status != Running {
				return ErrOwnership
			}
			now := time.Now().UTC()
			r.Outcome, r.FinishedAt = outcome, &now
			return nil
		}
		return ErrOwnership
	})
	if err != nil {
		return err
	}
	err = c.file.Close()
	c.file = nil
	return err
}

// Close releases this handle. Recovery waits until every inherited descriptor closes.
func (c *Claim) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return nil
	}
	err := c.file.Close()
	c.file = nil
	return err
}
