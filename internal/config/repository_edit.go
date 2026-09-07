package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/pelletier/go-toml/v2/unstable"
)

// SaveRepository changes only repository values in the active configuration.
// Runtime locks stay in the installation state directory, outside user configuration.
// expected must match the current repository; nil requires that it is still absent.
func SaveRepository(ctx context.Context, path, stateDir string, repo Repository, reviewer *Reviewer, expected *Repository) (Config, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Config{}, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Config{}, err
	}
	if !filepath.IsAbs(stateDir) {
		return Config{}, errors.New("absolute plugin state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return Config{}, err
	}
	lockName := fmt.Sprintf("config-%x.lock", sha256.Sum256([]byte(resolved)))
	lock, err := localstate.Lock(ctx, filepath.Join(stateDir, lockName))
	if err != nil {
		return Config{}, err
	}
	defer lock.Close()
	before, err := os.ReadFile(resolved)
	if err != nil {
		return Config{}, err
	}
	var currentConfig Config
	if err := decodeStrict(before, &currentConfig); err != nil {
		return Config{}, err
	}
	currentRepo, exists := currentConfig.RepositoryFor(repo.Name)
	if (expected == nil && exists) || (expected != nil && (!exists || !reflect.DeepEqual(currentRepo, *expected))) {
		return Config{}, errors.New("repository settings changed; reload before saving")
	}
	prepared := before
	if reviewer != nil {
		for _, defined := range currentConfig.Reviewers {
			if defined.ID == reviewer.ID {
				return Config{}, fmt.Errorf("reviewer %s already exists; reload repository settings", reviewer.ID)
			}
		}
		id, _ := json.Marshal(reviewer.ID)
		command, _ := json.Marshal(reviewer.Command)
		prepared = append(append([]byte(nil), before...), []byte("\n\n[[reviewers]]\nid = "+string(id)+"\ncommand = "+string(command)+"\n")...)
	}
	updated, err := editRepository(prepared, repo)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := decodeStrict(updated, &cfg); err != nil {
		return Config{}, err
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	current, err := os.ReadFile(resolved)
	if err != nil {
		return Config{}, err
	}
	if !bytes.Equal(before, current) {
		return Config{}, errors.New("configuration changed during save; reload repository settings")
	}
	if err := localstate.AtomicWrite(resolved, updated); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type repositoryEdit struct {
	start, end int
	value      string
}
type repositorySection struct {
	end    int
	values map[string]repositoryEdit
}

func nodeKey(n *unstable.Node) string {
	keys := n.Key()
	if !keys.Next() {
		return ""
	}
	key := string(keys.Node().Data)
	if keys.Next() {
		return ""
	}
	return key
}

func editRepository(data []byte, repo Repository) ([]byte, error) {
	var cfg Config
	if err := decodeStrict(data, &cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	index := -1
	for i, existing := range cfg.Repositories {
		if strings.EqualFold(existing.Name, repo.Name) {
			index = i
			break
		}
	}
	if index < 0 {
		encoded, err := json.Marshal(repo.Name)
		if err != nil {
			return nil, err
		}
		text := string(data) + "\n\n[[repositories]]\nname = " + string(encoded) + "\n"
		for _, field := range repositoryFields(repo) {
			text += field.key + " = " + field.value + "\n"
		}
		return []byte(text), nil
	}
	fields := repositoryFields(repo)
	owned := map[string]bool{}
	for _, field := range fields {
		owned[field.key] = true
	}
	var parser unstable.Parser
	parser.KeepComments = true
	parser.Reset(data)
	var selected *repositorySection
	var active *repositorySection
	sectionIndex := -1
	for parser.NextExpression() {
		n := parser.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			keys := n.Key()
			keys.Next()
			offset := int(keys.Node().Raw.Offset)
			start := bytes.LastIndexByte(data[:offset], '\n') + 1
			if active != nil {
				active.end = start
				for active.end > 0 {
					previous := bytes.LastIndexByte(data[:active.end-1], '\n') + 1
					line := bytes.TrimSpace(data[previous:active.end])
					if len(line) != 0 && line[0] != '#' {
						break
					}
					active.end = previous
				}
				active = nil
			}
			if n.Kind == unstable.ArrayTable && nodeKey(n) == "repositories" {
				sectionIndex++
				if sectionIndex == index {
					active = &repositorySection{end: len(data), values: map[string]repositoryEdit{}}
					selected = active
				}
			}
		case unstable.KeyValue:
			if active == nil {
				continue
			}
			key := nodeKey(n)
			if !owned[key] {
				continue
			}
			edit, err := repositoryValueRange(data, n)
			if err != nil {
				return nil, err
			}
			active.values[key] = edit
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, errors.New("repository setup requires [[repositories]] tables; convert inline repository settings before editing")
	}
	var edits []repositoryEdit
	var missing strings.Builder
	for _, field := range repositoryFields(repo) {
		if edit, exists := selected.values[field.key]; exists {
			edit.value = field.value + edit.value
			edits = append(edits, edit)
		} else {
			fmt.Fprintf(&missing, "%s = %s\n", field.key, field.value)
		}
	}
	if missing.Len() > 0 {
		edits = append(edits, repositoryEdit{start: selected.end, end: selected.end, value: "\n" + missing.String()})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	result := append([]byte(nil), data...)
	for _, edit := range edits {
		result = append(append(append([]byte(nil), result[:edit.start]...), []byte(edit.value)...), result[edit.end:]...)
	}
	return result, nil
}

type repositoryField struct{ key, value string }

func repositoryFields(repo Repository) []repositoryField {
	actions := repo.PublishActions
	if actions == nil {
		actions = []PublicationAction{}
	}
	reviewer, _ := json.Marshal(repo.Reviewer)
	publication, _ := json.Marshal(actions)
	automatic, _ := json.Marshal(repo.AutoPublish)
	return []repositoryField{{"reviewer", string(reviewer)}, {"auto_launch", fmt.Sprint(repo.AutoLaunch)}, {"publish_actions", string(publication)}, {"auto_publish", string(automatic)}}
}

// Only the repository setup fields are edited. The TOML parser handles quoted keys,
// strings, and multiline arrays; comments inside replaced arrays are retained.
func repositoryValueRange(data []byte, n *unstable.Node) (repositoryEdit, error) {
	v := n.Value()
	if v.Kind != unstable.Array {
		return repositoryEdit{start: int(v.Raw.Offset), end: int(v.Raw.Offset + v.Raw.Length)}, nil
	}
	keys := n.Key()
	keys.Next()
	key := keys.Node()
	start := int(key.Raw.Offset + key.Raw.Length)
	equals := bytes.IndexByte(data[start:], '=')
	if equals < 0 {
		return repositoryEdit{}, errors.New("repository array has no assignment")
	}
	start += equals + 1
	for start < len(data) && (data[start] == ' ' || data[start] == '\t') {
		start++
	}
	if start >= len(data) || data[start] != '[' {
		return repositoryEdit{}, errors.New("repository publication actions must be an array")
	}
	end := start + 1
	var comments strings.Builder
	children := v.Children()
	for children.Next() {
		child := children.Node()
		end = max(end, int(child.Raw.Offset+child.Raw.Length))
		if child.Kind == unstable.Comment {
			comments.WriteString("\n" + string(child.Data))
		}
	}
	closing := bytes.IndexByte(data[end:], ']')
	if closing < 0 {
		return repositoryEdit{}, errors.New("repository publication array is incomplete")
	}
	return repositoryEdit{start: start, end: end + closing + 1, value: comments.String()}, nil
}
