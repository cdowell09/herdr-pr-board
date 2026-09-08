package codexadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

func TestCodexCommandIsolationAndSchema(t *testing.T) {
	work := t.TempDir()
	checkout := filepath.Join(t.TempDir(), "captured source")
	cmd, err := command("/configured/codex", "/selected/skill", work, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/configured/codex" || cmd.Env != nil {
		t.Fatalf("executable or authentication environment changed: %#v", cmd)
	}
	for _, flag := range []string{"exec", "--json", "--ephemeral", "--ignore-user-config", "--ignore-rules"} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing %s: %v", flag, cmd.Args)
		}
	}
	pairs := [][2]string{
		{"--sandbox", "read-only"}, {"--color", "never"}, {"--output-schema", filepath.Join(work, "result-schema.json")},
		{"-c", `approval_policy="never"`}, {"-c", `project_doc_max_bytes=0`},
		{"-c", "projects." + strconv.Quote(checkout) + `.trust_level="untrusted"`},
		{"-c", `allow_login_shell=false`}, {"-c", `web_search="disabled"`},
		{"--enable", "skip_host_skill_discovery"}, {"--enable", "multi_agent"},
	}
	for _, feature := range []string{"hooks", "plugins", "remote_plugin", "apps", "memories", "skill_search", "skill_mcp_dependency_install", "shell_snapshot", "browser_use", "computer_use"} {
		pairs = append(pairs, [2]string{"--disable", feature})
	}
	for _, pair := range pairs {
		found := false
		for i := 0; i+1 < len(cmd.Args); i++ {
			if cmd.Args[i] == pair[0] && cmd.Args[i+1] == pair[1] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing option %v: %v", pair, cmd.Args)
		}
	}
	schema, err := os.ReadFile(filepath.Join(work, "result-schema.json"))
	if err != nil || string(schema) != agentadapter.ResultSchema || !json.Valid(schema) {
		t.Fatalf("schema=%s error=%v", schema, err)
	}
	info, err := os.Stat(filepath.Join(work, "result-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows uses inherited directory ACLs, not Unix permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("schema permissions: %v %v", info, err)
	}
}
