package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

const testPR = "https://github.com/acme/api/pull/7"
const testHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testBase = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type fakeGH struct {
	posts               int
	actor, head, target string
	postErr             error
	status              int
	block               bool
	onCapture           func()
	created             gh.PublishedReview
	reviews             []gh.PublishedReview
}

func (f *fakeGH) run(ctx context.Context, args ...string) ([]byte, error) {
	if args[0] == "pr" {
		if f.onCapture != nil {
			f.onCapture()
		}
		return json.Marshal(map[string]any{"number": 7, "url": testPR, "state": "OPEN", "headRefOid": f.head, "baseRefName": f.target, "baseRefOid": testBase})
	}
	if args[1] == "user" {
		return []byte(f.actor), nil
	}
	if strings.Contains(args[1], "?per_page=100") {
		return json.Marshal([][]gh.PublishedReview{f.reviews})
	}
	f.posts++
	fields := map[string]string{}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-f" {
			key, value, _ := strings.Cut(args[i+1], "=")
			fields[key] = value
		}
	}
	state := map[string]string{"COMMENT": "COMMENTED", "APPROVE": "APPROVED", "REQUEST_CHANGES": "CHANGES_REQUESTED"}[fields["event"]]
	f.created = gh.PublishedReview{ID: int64(f.posts), URL: fmt.Sprintf("%s#pullrequestreview-%d", testPR, f.posts), Body: fields["body"], CommitID: fields["commit_id"], State: state}
	f.created.User.Login = f.actor
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.status != 0 {
		return []byte(fmt.Sprintf("HTTP/1.1 %d Rejected\r\nContent-Type: application/json\r\n\r\n{\"message\":\"denied\"}", f.status)), errors.New("gh exited 1")
	}
	if f.postErr != nil {
		return nil, f.postErr
	}
	data, _ := json.Marshal(f.created)
	return append([]byte("HTTP/1.1 201 Created\r\nContent-Type: application/json\r\n\r\n"), data...), nil
}

func publicationFixture(t *testing.T, actions string) (*Service, *fakeGH, reviewmemory.Run) {
	t.Helper()
	state := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.toml")
	content := config.DefaultFile + "\n[[reviewers]]\nid = \"test\"\ncommand = [\"test-reviewer\"]\n[[repositories]]\nname = \"acme/api\"\nreviewer = \"test\"\npublish_actions = [" + actions + "]\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	f := &fakeGH{actor: "ada", head: testHead, target: "main"}
	s, err := New(state, path, gh.NewClient(f.run, config.GitHubConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.store.Claim(reviewmemory.Request{Identity: reviewmemory.Identity{Repository: "acme/api", Number: 7, HeadOID: testHead, BaseRefName: "main"}, BaseOID: testBase, Reviewer: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := claim.Finish(reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Reviewed captured revision.", Findings: []reviewmemory.Finding{{Severity: "P2", Title: "A finding", Body: "Fix this condition.", Path: "main.go", Line: 8}}}); err != nil {
		t.Fatal(err)
	}
	runs, err := s.store.PRHistory("acme/api", 7)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, runs[0]
}

func TestPermissionsEachActionAndRevocation(t *testing.T) {
	for _, action := range config.PublicationActions() {
		t.Run(string(action), func(t *testing.T) {
			s, f, run := publicationFixture(t, fmt.Sprintf("%q", action))
			got, err := s.Publish(context.Background(), testPR, run.ID, action)
			if err != nil || got.Status != Published || f.posts != 1 {
				t.Fatalf("%+v %v posts=%d", got, err, f.posts)
			}
			if !strings.Contains(f.created.Body, "Fix this condition.") || !strings.Contains(f.created.Body, testHead) {
				t.Fatal("missing local findings or revision")
			}
			again, err := s.Publish(context.Background(), testPR, run.ID, action)
			if err != nil || again.ID != got.ID || f.posts != 1 {
				t.Fatal("duplicate publication")
			}
		})
	}
	s, f, run := publicationFixture(t, "")
	if _, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment); err == nil || f.posts != 0 {
		t.Fatal("default permitted publication")
	}
	s, f, run = publicationFixture(t, `"comment"`)
	f.onCapture = func() {
		data, _ := os.ReadFile(s.configPath)
		data = []byte(strings.Replace(string(data), `["comment"]`, `[]`, 1))
		if err := os.WriteFile(s.configPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment); err == nil || f.posts != 0 {
		t.Fatal("revocation during preflight ignored")
	}
}

func TestStaleFindingsAndCorruptRecordsFailClosed(t *testing.T) {
	for _, change := range []string{"head", "target"} {
		s, f, run := publicationFixture(t, `"approve"`)
		if change == "head" {
			f.head = testBase
		} else {
			f.target = "release"
		}
		if _, err := s.Publish(context.Background(), testPR, run.ID, config.PublishApprove); err == nil || f.posts != 0 {
			t.Fatal("stale approval posted")
		}
	}
	s, f, run := publicationFixture(t, `"comment"`)
	if err := os.WriteFile(s.path(run.ID, config.PublishComment), []byte(`{"version":1,"attempts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment); err == nil || f.posts != 0 {
		t.Fatal("corrupt record allowed publication")
	}
}

func TestUncertainOutcomeReconcilesWithoutResending(t *testing.T) {
	s, f, run := publicationFixture(t, `"comment"`)
	f.postErr = errors.New("connection lost")
	first, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment)
	if !errors.Is(err, ErrUncertain) || first.Status != Uncertain {
		t.Fatalf("%+v %v", first, err)
	}
	// A process restart and an immediately empty GET do not authorize a second POST.
	restarted, err := New(filepath.Dir(s.dir), s.configPath, s.github)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Publish(context.Background(), testPR, run.ID, config.PublishComment); !errors.Is(err, ErrUncertain) || f.posts != 1 {
		t.Fatal("uncertain attempt resent")
	}
	f.reviews = []gh.PublishedReview{f.created}
	f.actor = "other"
	if _, err := restarted.Publish(context.Background(), testPR, run.ID, config.PublishComment); !errors.Is(err, ErrUncertain) {
		t.Fatal("different actor reconciled review")
	}
	f.actor = "ada"
	result, err := restarted.Publish(context.Background(), testPR, run.ID, config.PublishComment)
	if err != nil || result.Status != Published || f.posts != 1 {
		t.Fatalf("%+v %v posts=%d", result, err, f.posts)
	}
	local, err := s.store.PRHistory("acme/api", 7)
	if err != nil || len(local) != 1 || local[0].Status != reviewmemory.Completed {
		t.Fatal("publication damaged local completion")
	}
}

func TestDefiniteRejectionCanRetryAndKeepsHistory(t *testing.T) {
	s, f, run := publicationFixture(t, `"comment"`)
	f.status = 403
	first, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment)
	var rejected *gh.PublicationRejected
	if !errors.As(err, &rejected) || first.Status != Failed {
		t.Fatalf("%+v %v", first, err)
	}
	f.status = 0
	second, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment)
	if err != nil || second.Status != Published || second.ID == first.ID {
		t.Fatalf("%+v %v", second, err)
	}
	history, err := s.History(testPR)
	if err != nil || len(history) != 2 || history[0].Status != Failed || history[1].Status != Published {
		t.Fatalf("%+v %v", history, err)
	}
}

func TestConcurrentPublicationSendsOnce(t *testing.T) {
	s, f, run := publicationFixture(t, `"comment"`)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Publish(context.Background(), testPR, run.ID, config.PublishComment)
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.posts != 1 {
		t.Fatalf("posted %d times", f.posts)
	}
}

func TestPublicationDeadlineBoundsNetworkAndLockWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, f, run := publicationFixture(t, `"comment"`)
	s.timeout = 100 * time.Millisecond
	lockPath := filepath.Join(s.dir, "publication.lock")
	held, err := localstate.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, err := s.Publish(ctx, testPR, run.ID, config.PublishComment); !errors.Is(err, context.DeadlineExceeded) || f.posts != 0 {
		t.Fatalf("lock wait escaped deadline: %v posts=%d", err, f.posts)
	}
	held.Close()
	f.block = true
	result, err := s.Publish(ctx, testPR, run.ID, config.PublishComment)
	if !errors.Is(err, ErrUncertain) || result.Status != Uncertain || f.posts != 1 {
		t.Fatalf("network escaped deadline: %+v %v", result, err)
	}
	released, err := localstate.TryLock(lockPath)
	if err != nil {
		t.Fatalf("deadline retained publication lock: %v", err)
	}
	released.Close()
}
