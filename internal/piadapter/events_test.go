package piadapter

import (
	"os"
	"strings"
	"testing"
)

func TestPiEventContract(t *testing.T) {
	data, err := os.ReadFile("testdata/completed.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	final, err := finalText(data)
	if err != nil || !strings.Contains(string(final), `"version":1`) {
		t.Fatalf("final=%s err=%v", final, err)
	}
	for name, broken := range map[string]string{
		"error":          strings.ReplaceAll(string(data), `"stopReason":"stop"`, `"stopReason":"error"`),
		"aborted":        strings.ReplaceAll(string(data), `"stopReason":"stop"`, `"stopReason":"aborted"`),
		"length":         strings.ReplaceAll(string(data), `"stopReason":"stop"`, `"stopReason":"length"`),
		"unfinished":     `{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"done"}]}}`,
		"malformed":      string(data) + "oops",
		"trailing event": string(data) + `{"type":"agent_start"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(broken)); err == nil {
				t.Fatal("accepted incomplete/error stream")
			}
		})
	}
}

func TestRecordedPiSessionSettlesAfterFinalResult(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-0.84.2-recorded.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	final, err := finalText(data)
	if err != nil || !strings.Contains(string(final), `"number":56`) {
		t.Fatalf("final=%s err=%v", final, err)
	}
	for name, broken := range map[string]string{
		"new run":            string(data) + `{"type":"agent_start"}`,
		"message":            string(data) + `{"type":"message_end"}`,
		"error":              string(data) + `{"type":"error"}`,
		"duplicate settled":  string(data) + `{"type":"agent_settled"}`,
		"settled before end": `{"type":"agent_settled"}` + string(data),
		"retry":              strings.ReplaceAll(string(data), `"willRetry":false`, `"willRetry":true`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(broken)); err == nil {
				t.Fatal("accepted invalid terminal event sequence")
			}
		})
	}
}
