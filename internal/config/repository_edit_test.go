package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositorySetupPreservesConfigurationAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chosen.toml")
	content := DefaultFile + `
# Reusable command stays verbatim.
[[reviewers]]
id = "pi"
command = ['pi-wrapper', '--custom'] # command comment

[[repositories]] # repository comment
name = 'acme/api'
"reviewer" = 'pi' # reviewer comment
auto_launch = false # launch comment
publish_actions = [
  # Keep this explanation.
  "comment", # Keep the action comment.
] # publication comment

# Another repository stays verbatim.
[[repositories]]
name = 'other/repo'
reviewer = 'pi'
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "ACME/API", Reviewer: "pi", AutoLaunch: true, PublishActions: []PublicationAction{PublishComment, PublishApprove}}, nil, repositoryExpectation(t, path, "acme/api"), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := got.RepositoryFor("acme/api")
	if !repo.AutoLaunch || len(repo.PublishActions) != 2 {
		t.Fatalf("%+v", repo)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{DefaultFile, "command = ['pi-wrapper', '--custom'] # command comment", "# repository comment", "# reviewer comment", "# launch comment", "# Keep this explanation.", "# Keep the action comment.", "# publication comment", "# Another repository stays verbatim.\n[[repositories]]\nname = 'other/repo'\nreviewer = 'pi'"} {
		if !strings.Contains(string(data), preserved) {
			t.Fatalf("lost %q:\n%s", preserved, data)
		}
	}
}

func TestFirstRepositorySetupAddsReusableReviewerAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(DefaultFile), 0600); err != nil {
		t.Fatal(err)
	}
	reviewer := Reviewer{ID: "pi", Command: []string{"/plugin/board", "--pi-reviewer"}}
	got, err := SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: "pi"}, &reviewer, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reviewers) != 1 || len(got.Repositories) != 1 || got.Repositories[0].AutoLaunch || len(got.Repositories[0].PublishActions) != 0 {
		t.Fatalf("unsafe defaults: %+v", got)
	}
	before, _ := os.ReadFile(path)
	_, err = SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: "missing"}, nil, repositoryExpectation(t, path, "acme/api"), nil)
	if err == nil {
		t.Fatal("saved unknown reviewer")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed validation changed configuration")
	}
}

func TestRepositoryPermissionDefaultsAndValidation(t *testing.T) {
	cfg := Config{Reviewers: []Reviewer{{ID: "pi", Command: []string{"pi"}}}, Repositories: []Repository{{Name: "acme/api", Reviewer: "pi"}}}
	if _, err := cfg.ResolveLaunch("acme/api", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolveLaunch("acme/api", "", true); err == nil {
		t.Fatal("default automatic permission")
	}
	for _, action := range PublicationActions() {
		if err := cfg.AllowPublication("acme/api", action); err == nil {
			t.Fatal("default publication permission")
		}
	}
	for _, actions := range [][]PublicationAction{{"unknown"}, {PublishComment, PublishComment}} {
		if err := (Repository{Name: "acme/api", PublishActions: actions}).validatePermissions(); err == nil {
			t.Fatal("accepted invalid actions")
		}
	}
	cfg.Repositories[0].AutoLaunch = true
	if _, err := cfg.ResolveLaunch("ACME/API", "", true); err != nil {
		t.Fatal(err)
	}
}

func TestInlineRepositorySetupLeavesExistingFileUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "reviewers = [{ id = \"pi\", command = [\"pi-wrapper\"] }]\n" +
		"repositories = [{ name = \"acme/api\", reviewer = \"pi\" }] # Keep inline settings.\n" + DefaultFile
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExisting(path); err != nil {
		t.Fatalf("fixture must be valid configuration: %v", err)
	}
	_, err := SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: "pi", AutoLaunch: true}, nil, repositoryExpectation(t, path, "acme/api"), nil)
	if err == nil || !strings.Contains(err.Error(), "inline repository settings") {
		t.Fatalf("expected explicit inline editing limitation: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Fatal("unsupported inline editing changed the configuration")
	}
}

func repositoryExpectation(t *testing.T, path, name string) *Repository {
	t.Helper()
	cfg, err := LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	repo, exists := cfg.RepositoryFor(name)
	if !exists {
		t.Fatal("missing expected repository")
	}
	return &repo
}

func TestRepositorySaveRejectsStalePermissionSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := DefaultFile + "\n[[reviewers]]\nid = \"pi\"\ncommand = [\"pi-wrapper\"]\n[[reviewers]]\nid = \"other\"\ncommand = [\"other-wrapper\"]\n[[repositories]]\nname = \"acme/api\"\nreviewer = \"pi\"\npublish_actions = [\"comment\"]\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	expected := repositoryExpectation(t, path, "acme/api")
	revoked := *expected
	revoked.PublishActions = nil
	// A concurrent edit outside this repository must survive the save.
	content = strings.Replace(content, `title = "Pull Requests"`, `title = "Changed outside setup"`, 1)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	saved, err := SaveRepository(context.Background(), path, state, revoked, nil, expected, nil)
	if err != nil || saved.UI.Title != "Changed outside setup" {
		t.Fatalf("unrelated change was lost: %+v %v", saved, err)
	}
	before, _ := os.ReadFile(path)
	stale := *expected
	stale.Reviewer = "other"
	if _, err := SaveRepository(context.Background(), path, state, stale, nil, expected, nil); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale save accepted: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("stale save restored revoked publication permission")
	}
	if _, err := SaveRepository(context.Background(), path, state, stale, nil, nil, nil); err == nil {
		t.Fatal("stale absence overwrote an existing repository")
	}
}
