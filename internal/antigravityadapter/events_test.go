package antigravityadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalTextRequiresCompleteIsolatedTurn(t *testing.T) {
	init := `{"event":"init","conversation_id":"c","init":{"agent":"reviewer"}}` + "\n"
	active := `{"event":"step_update","step_update":{"conversation_id":"c","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"{"}}` + "\n"
	done := `{"event":"step_update","step_update":{"conversation_id":"c","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"}"}}` + "\n"
	result := `{"event":"result","result":{"conversation_id":"c","status":"SUCCESS","num_turns":1,"response":"{}"}}`
	good := init + active + done + result
	if text, err := finalText([]byte(good), "reviewer"); err != nil || string(text) != "{}" {
		t.Fatalf("text=%s err=%v", text, err)
	}
	for name, data := range map[string]string{
		"missing init":           active + done + result,
		"wrong agent":            strings.Replace(good, `"agent":"reviewer"`, `"agent":"default"`, 1),
		"wrong conversation":     strings.Replace(good, `"conversation_id":"c","status"`, `"conversation_id":"other","status"`, 1),
		"partial timeout":        init + active + result,
		"completed then pending": init + active + done + `{"event":"step_update","step_update":{"conversation_id":"c","step_index":2,"state":"ACTIVE","step_type":"tool_call"}}` + "\n" + result,
		"error status":           strings.Replace(good, `"status":"SUCCESS"`, `"status":"ERROR"`, 1),
		"embedded error":         strings.Replace(good, `"status":"SUCCESS"`, `"status":"SUCCESS","error":"failed"`, 1),
		"no turns":               strings.Replace(good, `"num_turns":1`, `"num_turns":0`, 1),
		"different response":     strings.Replace(good, `"response":"{}"`, `"response":"other"`, 1),
		"missing terminal":       init + active + done,
		"failed step":            strings.Replace(good, `"state":"DONE"`, `"state":"ERROR"`, 1),
		"duplicate terminal":     good + "\n" + result,
		"duplicate done":         init + active + done + done + result,
		"truncated":              good[:len(good)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(data), "reviewer"); err == nil {
				t.Fatal("accepted incomplete or unsafe stream")
			}
		})
	}
}

func TestNativeTerminalFixtures(t *testing.T) {
	for name, want := range map[string]string{
		"success": "OFFLINE_RESPONSE\n", "missing-reason": "{}\n", "unknown-reason": "{}\n",
		"timeout": "", "partial-timeout": "", "max-tokens": "", "transport-length": "",
		"transport-chunked": "", "sse-partial": "", "sse-trailing-partial": "",
	} {
		data, err := os.ReadFile(filepath.Join("testdata", name+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		text, err := finalText(data, "reviewer")
		if want != "" {
			if err != nil || string(text) != want {
				t.Fatalf("text=%s err=%v", text, err)
			}
		} else if err == nil {
			t.Fatalf("accepted native %s", name)
		}
	}
}
