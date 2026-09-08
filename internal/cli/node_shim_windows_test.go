package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveNodeShimPreservesArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Node & tools")
	target := filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{target: []byte("unused"), filepath.Join(dir, "node.exe"): []byte("unused"), filepath.Join(dir, "codex.cmd"): []byte(nodeShimFixture)} {
		if err := os.WriteFile(name, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"a & b", "percent%NAME%", `quote"`, "λ", ""}
	t.Chdir(dir)
	for _, launcher := range []string{filepath.Join(dir, "codex.cmd"), `.\codex.cmd`, "./codex.cmd"} {
		cmd := exec.Command(launcher, args...)
		if err := ResolveNodeShim(cmd); err != nil {
			t.Fatal(err)
		}
		if cmd.Path != filepath.Join(dir, "node.exe") || !reflect.DeepEqual(cmd.Args[1:], append([]string{target}, args...)) {
			t.Fatalf("launcher=%q path=%q args=%q", launcher, cmd.Path, cmd.Args)
		}
	}
}
