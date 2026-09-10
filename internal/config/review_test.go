package config

import (
	"strings"
	"testing"
	"time"
)

func TestReviewDefaultsAndReusableReviewer(t *testing.T) {
	cfg, err := Load(writeConfigFile(t, DefaultFile))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Review.MaxConcurrency != 1 {
		t.Fatal(cfg.Review)
	}
	if duration, err := cfg.Review.TimeoutDuration(); err != nil || duration != 30*time.Minute {
		t.Fatalf("%v %v", duration, err)
	}
	if !strings.Contains(DefaultFile, "[review]") {
		t.Fatal("default file omits review settings")
	}
	if cfg.Review.Notify != NotifyAll || !strings.Contains(DefaultFile, `notify = "all"`) {
		t.Fatalf("notify default = %q", cfg.Review.Notify)
	}
	data := DefaultFile + `
[[reviewers]]
id = "agent"
command = ["reviewer", "argument with spaces"]
[[repositories]]
name = "acme/one"
reviewer = "agent"
[[repositories]]
name = "acme/two"
reviewer = "agent"
`
	cfg, err = LoadExisting(writeConfigFile(t, data))
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"ACME/one", "acme/two"} {
		reviewer, err := cfg.ReviewerFor(repo, "")
		if err != nil || reviewer.ID != "agent" || reviewer.Command[1] != "argument with spaces" {
			t.Fatalf("%+v %v", reviewer, err)
		}
	}
}

func TestInvalidReviewSettings(t *testing.T) {
	for _, suffix := range []string{
		`[[reviewers]]
id = "bad id"
command = ["reviewer"]`,
		`[[reviewers]]
id = "agent"
command = []`,
		`[[reviewers]]
id = "agent"
command = ["reviewer"]
[[reviewers]]
id = "agent"
command = ["other"]`,
		`[[repositories]]
name = "acme/one"
reviewer = "missing"`,
	} {
		if _, err := LoadExisting(writeConfigFile(t, DefaultFile+"\n"+suffix)); err == nil {
			t.Fatalf("accepted %s", suffix)
		}
	}
	for _, replacement := range []struct{ old, new string }{
		{"max_concurrency = 1", "max_concurrency = -1"},
		{"max_concurrency = 1", "max_concurrency = 9"},
		{`timeout = "30m"`, `timeout = "0"`},
		{`timeout = "30m"`, `timeout = "25h"`},
		{`notify = "all"`, `notify = "sometimes"`},
	} {
		if _, err := LoadExisting(writeConfigFile(t, strings.Replace(DefaultFile, replacement.old, replacement.new, 1))); err == nil {
			t.Fatalf("accepted %s", replacement.new)
		}
	}
}

func TestReviewNotifyModes(t *testing.T) {
	for _, mode := range []NotifyMode{NotifyAll, NotifyProblems, NotifyOff} {
		data := strings.Replace(DefaultFile, `notify = "all"`, `notify = "`+string(mode)+`"`, 1)
		cfg, err := LoadExisting(writeConfigFile(t, data))
		if err != nil || cfg.Review.Notify != mode {
			t.Fatalf("%s: %+v %v", mode, cfg.Review, err)
		}
	}
	absent := strings.Replace(DefaultFile, "notify = \"all\"\n", "", 1)
	cfg, err := LoadExisting(writeConfigFile(t, absent))
	if err != nil || cfg.Review.Notify != NotifyAll {
		t.Fatalf("absent notify: %+v %v", cfg.Review, err)
	}
}
