package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomaticConfigurationValidatesViewsAndPublicationSelection(t *testing.T) {
	for _, test := range []struct {
		name, views, permissions, action string
		valid                            bool
	}{
		{"defaults", "[]", "[]", "", true},
		{"selected", "[\"review\"]", "[\"comment\"]", "comment", true},
		{"unknown view", "[\"missing\"]", "[]", "", false},
		{"duplicate view", "[\"review\",\"review\"]", "[]", "", false},
		{"unpermitted action", "[]", "[\"comment\"]", "approve", false},
		{"explicit approval", "[]", "[\"approve\"]", "approve", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := strings.Replace(DefaultFile, "auto_views = []", "auto_views = "+test.views, 1) + "\n[[reviewers]]\nid = \"fake\"\ncommand = [\"unused\"]\n[[repositories]]\nname = \"owner/repo\"\nreviewer = \"fake\"\nauto_launch = true\npublish_actions = " + test.permissions + "\nauto_publish = \"" + test.action + "\"\n"
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadExisting(path)
			if (err == nil) != test.valid {
				t.Fatalf("config error=%v", err)
			}
			if test.name == "defaults" && len(cfg.Review.AutoViews) != 0 {
				t.Fatal("default enables automatic views")
			}
		})
	}
}

func TestAutomaticPublicationSelectionSurvivesSafeRepositorySave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := DefaultFile + "\n[[reviewers]]\nid = \"fake\"\ncommand = [\"unused\"]\n[[repositories]]\nname = \"owner/repo\"\nreviewer = \"fake\"\npublish_actions = [\"comment\"]\nauto_publish = \"comment\" # selection\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	expected := repositoryExpectation(t, path, "owner/repo")
	changed := *expected
	changed.AutoLaunch = true
	cfg, err := SaveRepository(context.Background(), path, t.TempDir(), changed, nil, expected)
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := cfg.RepositoryFor("owner/repo")
	if repo.AutoPublish != PublishComment {
		t.Fatalf("selection lost: %+v", repo)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# selection") {
		t.Fatal("selection comment lost")
	}
}
