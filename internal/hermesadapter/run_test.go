package hermesadapter

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalText(t *testing.T) {
	valid := `{"type":"result","completed":true,"finalAssistant":true,"text":"{}"}`
	if got, err := finalText([]byte(valid)); err != nil || string(got) != "{}" {
		t.Fatalf("text=%s err=%v", got, err)
	}
	for name, data := range map[string]string{
		"partial":          strings.Replace(valid, `"completed":true`, `"completed":false`, 1),
		"missing complete": strings.Replace(valid, `"completed":true,`, "", 1),
		"pending tools":    strings.Replace(valid, `"finalAssistant":true`, `"finalAssistant":false`, 1),
		"missing final":    strings.Replace(valid, `"finalAssistant":true,`, "", 1),
		"empty":            strings.Replace(valid, `"{}"`, `""`, 1),
		"truncated":        valid[:len(valid)-1],
		"multiple":         valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(data)); err == nil {
				t.Fatal("accepted failed or incomplete result")
			}
		})
	}
}

func TestBridgeNativeBoundary(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("Python is not installed")
	}
	work := t.TempDir()
	cmd, err := command(python, "", work, filepath.Join(work, "checkout"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Args[1] != "-I" {
		t.Fatal("Python startup must exclude ambient import and startup settings")
	}
	testScript, err := filepath.Abs("testdata/bridge_test.py")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Args = append([]string{python, "-I", testScript}, cmd.Args[2:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Hermes native boundary tests: %v\n%s", err, output)
	}
}
