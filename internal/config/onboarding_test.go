package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupSavesGlobalViewsAndRepositoryTogether(t *testing.T) {
	for _, style := range []string{"array", "missing field", "missing table", "inline"} {
		t.Run(style, func(t *testing.T) {
			text := strings.Replace(DefaultFile, "auto_views = []", "auto_views = [\n # selected-view comment\n] # trailing comment", 1)
			switch style {
			case "missing field":
				text = strings.Replace(DefaultFile, "auto_views = []\n", "", 1)
			case "missing table":
				text, _, _ = strings.Cut(DefaultFile, "[review]")
			case "inline":
				text, _, _ = strings.Cut(DefaultFile, "[review]")
				text = "review = { auto_views = [] }\n" + text
			}
			text += "\n# Preserve this final comment.\n"
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadExisting(path)
			if err != nil {
				t.Fatal(err)
			}
			edit := AutomaticViewsEdit{Selected: []string{"review"}, Expected: cfg}
			saved, err := SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: "pi", AutoLaunch: true}, &Reviewer{ID: "pi", Command: []string{"pi"}}, nil, &edit)
			after, _ := os.ReadFile(path)
			if style == "inline" {
				if err == nil || !strings.Contains(err.Error(), "explicit review") || string(after) != text {
					t.Fatalf("inline partial save: %s %v", after, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Review.AutoViews) != 1 || saved.Review.AutoViews[0] != "review" || len(saved.Repositories) != 1 || len(saved.Reviewers) != 1 {
				t.Fatalf("%+v", saved)
			}
			if !strings.Contains(string(after), "# Preserve this final comment.") {
				t.Fatal("lost comment")
			}
			if style == "array" && (!strings.Contains(string(after), "# selected-view comment") || !strings.Contains(string(after), "# trailing comment")) {
				t.Fatal("lost array comments")
			}
		})
	}
}

func TestSetupRejectsChangedGlobalMeaningWithoutPartialWrite(t *testing.T) {
	for _, change := range []string{"selection", "query", "scopes", "invalid selected"} {
		t.Run(change, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			edit := AutomaticViewsEdit{Selected: []string{"review"}, Expected: cfg}
			text := DefaultFile
			switch change {
			case "selection":
				text = strings.Replace(text, "auto_views = []", `auto_views = ["authored"]`, 1)
			case "query":
				text = strings.Replace(text, "review-requested:@me", "review-requested:someone", 1)
			case "scopes":
				text = strings.Replace(text, "user:@me", "org:acme", 1)
			case "invalid selected":
				edit.Selected = []string{"missing"}
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			_, err = SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: "pi"}, &Reviewer{ID: "pi", Command: []string{"pi"}}, nil, &edit)
			after, _ := os.ReadFile(path)
			if err == nil || string(after) != text {
				t.Fatalf("stale/invalid write: %v\n%s", err, after)
			}
		})
	}
}

func TestSetupUsesDefaultsForExpectedDiscovery(t *testing.T) {
	text := strings.Replace(DefaultFile, "max_concurrency = 4\n", "", 1)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	edit := AutomaticViewsEdit{Selected: []string{"review"}, Expected: cfg}
	_, err = SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: "pi"}, &Reviewer{ID: "pi", Command: []string{"pi"}}, nil, &edit)
	if err != nil {
		t.Fatal(err)
	}
}
