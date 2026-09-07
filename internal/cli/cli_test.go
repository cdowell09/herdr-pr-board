package cli

import (
	"context"
	"strings"
	"testing"
)

func TestCommandReturnsStdout(t *testing.T) {
	output, err := Command("sh")(context.Background(), "-c", "printf body")
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "body" {
		t.Fatalf("output = %q", output)
	}
}

func TestCommandKeepsStdoutAndUsesStderrForErrors(t *testing.T) {
	output, err := Command("sh")(context.Background(), "-c", "printf body; printf 'gh: oops\n' >&2; exit 1")
	if err == nil || err.Error() != "gh: oops" {
		t.Fatalf("error = %v, want stderr text", err)
	}
	if string(output) != "body" {
		t.Fatalf("output = %q, want stdout kept on failure", output)
	}
}

func TestCommandFallsBackToStdoutThenExitStatus(t *testing.T) {
	_, err := Command("sh")(context.Background(), "-c", "printf 'only stdout'; exit 3")
	if err == nil || err.Error() != "only stdout" {
		t.Fatalf("error = %v, want stdout text", err)
	}
	_, err = Command("sh")(context.Background(), "-c", "exit 4")
	if err == nil || !strings.Contains(err.Error(), "exit status 4") {
		t.Fatalf("error = %v, want exit status", err)
	}
}
