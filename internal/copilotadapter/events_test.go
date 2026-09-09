package copilotadapter

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func framedTranscript(data string) []byte {
	var output strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		fmt.Fprintf(&output, "Content-Length: %d\r\n\r\n%s", len(line), line)
	}
	return []byte(output.String())
}

func nativeTranscript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/native-sdk.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFinalTextRequiresProviderStopAndTerminalIdle(t *testing.T) {
	base := nativeTranscript(t)
	for _, test := range []struct{ name, old, replacement string }{
		{"completed", "", ""},
		{"version", `"version":"1.0.83"`, `"version":"1.0.84"`},
		{"protocol", `"protocolVersion":3`, `"protocolVersion":4`},
		{"length", `"finish_reason":"stop"`, `"finish_reason":"length"`},
		{"tools", `"toolRequests":[]`, `"toolRequests":[{}]`},
		{"wrong_api", `"apiCallId":"offline-completion"`, `"apiCallId":"other"`},
		{"aborted", `"mode":"interactive"`, `"mode":"interactive","aborted":true`},
		{"autopilot", `"mode":"interactive"`, `"mode":"autopilot"`},
		{"missing_idle", `"type":"session.idle"`, `"type":"assistant.idle"`},
		{"missing_end", `"type":"assistant.turn_end"`, `"type":"ignored"`},
		{"foreign_session", `"sessionId":"session-1"`, `"sessionId":"foreign"`},
		{"failed_patch", `"success":true`, `"success":false`},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := base
			if test.old != "" {
				data = strings.Replace(data, test.old, test.replacement, 1)
			}
			final, err := finalText(framedTranscript(data))
			if test.name == "completed" {
				if err != nil || string(final) != "OFFLINE_RESULT" {
					t.Fatalf("final=%s err=%v", final, err)
				}
			} else if err == nil {
				t.Fatal("incomplete review accepted")
			}
		})
	}
}

func TestRejectsPartialAndPostTerminalFrames(t *testing.T) {
	base := framedTranscript(nativeTranscript(t))
	for _, suffix := range [][]byte{
		[]byte("Content-Length:"), []byte("Content-Length: 5\r\n\r\n{"),
		framedTranscript(`{"jsonrpc":"2.0","method":"session.event","params":{"sessionId":"session-1","event":{"type":"assistant.message","data":{"content":"replacement"}}}}`),
	} {
		if _, err := finalText(append(append([]byte{}, base...), suffix...)); err == nil {
			t.Fatal("trailing output accepted")
		}
	}
}

func TestLaterModelResponseCannotReuseEarlierAssistantText(t *testing.T) {
	data := nativeTranscript(t)
	marker := `{"jsonrpc":"2.0","method":"session.event","params":{"sessionId":"session-1","event":{"type":"assistant.turn_end"`
	later := `{"jsonrpc":"2.0","method":"session.event","params":{"sessionId":"session-1","event":{"type":"model.model_call_success","data":{"modelCall":{"api_id":"later"},"responseChunk":{"choices":[{"finish_reason":"stop"}]}}}}}`
	if !strings.Contains(data, marker) {
		t.Fatal("fixture lacks turn end")
	}
	data = strings.Replace(data, marker, later+"\n"+marker, 1)
	if _, err := finalText(framedTranscript(data)); err == nil {
		t.Fatal("reused an earlier assistant response")
	}
}
