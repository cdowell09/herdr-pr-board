package copilotadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSessionIsolationPreservesHostAuthentication(t *testing.T) {
	work := t.TempDir()
	checkout := filepath.Join(work, "checkout")
	cmd, err := command("copilot", "", work, checkout)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Env != nil || strings.Contains(strings.Join(cmd.Args, " "), "no-auto-login") {
		t.Fatal("host authentication was replaced")
	}
	settings, err := os.ReadFile(filepath.Join(work, "copilot-config", "settings.json"))
	if err != nil || string(settings) != `{"disableAllHooks":true}` {
		t.Fatalf("settings=%s error=%v", settings, err)
	}
	opts := sessionOptions(checkout)
	for _, key := range []string{"enableConfigDiscovery", "enableFileHooks", "enableSkills", "enableManagedSettings", "enableOnDemandInstructionDiscovery", "enableSessionStore", "enableHostGitOperations", "requestExtensions"} {
		if opts[key] != false {
			t.Errorf("%s is not disabled", key)
		}
	}
	if opts["configDir"] != filepath.Join(work, "copilot-config") || opts["skipCustomInstructions"] != true {
		t.Fatal("session configuration is not isolated")
	}
	if !reflect.DeepEqual(opts["availableTools"], []string{"view", "grep", "glob"}) {
		t.Fatal("unexpected tools")
	}
	if _, err := json.Marshal(opts); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyDirectoryFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := checkPolicyDirectory(filepath.Join(dir, "absent")); err != nil {
		t.Fatal(err)
	}
	if err := checkPolicyDirectory(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "policy.JSON")
	if err := os.WriteFile(file, []byte("not even valid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkPolicyDirectory(dir); err == nil {
		t.Fatal("policy source accepted")
	}
	if err := checkPolicyDirectory(file); err == nil {
		t.Fatal("unreadable directory accepted")
	}
}
