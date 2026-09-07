package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
)

func TestEligibilityCommandUsesFreshSnapshotAndSharedReasons(t *testing.T) {
	log := fakeSnapshotGH(t)
	scriptPath := filepath.Join(filepath.Dir(log), "gh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(data), "head123", strings.Repeat("a", 40))
	script = strings.ReplaceAll(script, "base123", strings.Repeat("b", 40))
	script = strings.ReplaceAll(script, `"isDraft":false`, `"isDraft":false,"state":"open"`)
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	path := writeConfig(t, validConfigTOML+`\n`)
	if err := os.WriteFile(path, []byte(validConfigTOML+`
[review]
auto_views = ["all"]
[[reviewers]]
id = "fake"
command = ["unused"]
[[repositories]]
name = "acme/api"
reviewer = "fake"
auto_launch = true
`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "enrichment"} {
		t.Setenv("GH_TEST_MODE", mode)
		var out, diagnostics bytes.Buffer
		code := run([]string{"--review-eligibility", "--config", path}, &out, &diagnostics)
		var result struct {
			Version   int                 `json:"version"`
			Decisions []dispatch.Decision `json:"decisions"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("decode: %v output=%s diagnostics=%s", err, &out, &diagnostics)
		}
		want := dispatch.Ready
		wantCode := 0
		if mode != "" {
			want = dispatch.ObservationFailed
			wantCode = 1
		}
		if code != wantCode || len(result.Decisions) != 1 || result.Decisions[0].Reason != want {
			t.Fatalf("code=%d result=%+v diagnostics=%s", code, result, &diagnostics)
		}
	}
	var out, diagnostics bytes.Buffer
	if code := run([]string{"--review-eligibility", "--json"}, &out, &diagnostics); code != 2 {
		t.Fatalf("mixed mode code=%d", code)
	}
}
