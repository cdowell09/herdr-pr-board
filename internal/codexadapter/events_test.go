package codexadapter

import (
	"os"
	"strings"
	"testing"
)

const completedEvents = `{"type":"thread.started","thread_id":"thread"}
{"type":"turn.started"}
{"type":"item.started","item":{"type":"command_execution"}}
{"type":"item.completed","item":{"type":"command_execution","status":"completed"}}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"version\":1}"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
`

func TestCodexTerminalEventContract(t *testing.T) {
	final, err := finalText([]byte(completedEvents))
	if err != nil || string(final) != `{"version":1}` {
		t.Fatalf("final=%s error=%v", final, err)
	}
	for name, data := range map[string]string{
		"empty":               "",
		"malformed":           completedEvents + "oops",
		"trailing event":      completedEvents + `{"type":"item.completed","item":{"type":"agent_message","text":"extra"}}`,
		"missing thread":      strings.Replace(completedEvents, `{"type":"thread.started","thread_id":"thread"}`, "", 1),
		"duplicate thread":    `{"type":"thread.started"}` + completedEvents,
		"missing turn":        strings.Replace(completedEvents, `{"type":"turn.started"}`, "", 1),
		"duplicate turn":      strings.Replace(completedEvents, `{"type":"turn.started"}`, `{"type":"turn.started"}{"type":"turn.started"}`, 1),
		"truncated":           strings.Split(completedEvents, `{"type":"turn.completed"`)[0],
		"failed":              strings.Replace(completedEvents, `"turn.completed"`, `"turn.failed"`, 1),
		"error":               strings.Replace(completedEvents, `"turn.completed"`, `"error"`, 1),
		"no final text":       strings.Replace(completedEvents, `"agent_message"`, `"reasoning"`, 1),
		"empty later message": strings.Replace(completedEvents, `{"type":"turn.completed"`, `{"type":"item.completed","item":{"type":"agent_message","text":""}}`+`{"type":"turn.completed"`, 1),
		"invalid item":        strings.Replace(completedEvents, `"item":{"type":"command_execution"}`, `"item":null`, 1),
		"unknown event":       strings.Replace(completedEvents, `"turn.started"`, `"invented"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if final, err := finalText([]byte(data)); err == nil {
				t.Fatalf("accepted invalid stream: %s", final)
			}
		})
	}
}

// Derived from Codex 0.153.4's TurnCompleted event mapping: settle the plan
// and unfinished tool items before emitting the terminal turn.completed event.
func TestCodexKeepsFinalMessageWhileSettlingItems(t *testing.T) {
	data, err := os.ReadFile("testdata/codex-0.153.4-settled.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, settled := range []string{"", `{"type":"item.completed","item":{"id":"command","type":"command_execution","status":"completed","exit_code":0}}`} {
		events := strings.Replace(string(data), `{"type":"turn.completed"`, settled+`{"type":"turn.completed"`, 1)
		if settled != "" {
			events = strings.Replace(events, `{"type":"item.completed","item":{"id":"result"`, `{"type":"item.started","item":{"id":"command","type":"command_execution","status":"in_progress"}}`+`{"type":"item.completed","item":{"id":"result"`, 1)
		}
		final, err := finalText([]byte(events))
		if err != nil || !strings.Contains(string(final), `"message":"Both review axes completed"`) {
			t.Fatalf("settled result=%s error=%v", final, err)
		}
	}
	later := `{"type":"item.completed","item":{"type":"agent_message","text":"replacement"}}`
	events := strings.Replace(string(data), `{"type":"turn.completed"`, later+`{"type":"turn.completed"`, 1)
	if final, err := finalText([]byte(events)); err != nil || string(final) != "replacement" {
		t.Fatalf("latest message=%s error=%v", final, err)
	}
}

func TestCodexStartupWarning(t *testing.T) {
	warning := `{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Under-development features enabled: skip_host_skill_discovery."}}`
	events := strings.Replace(completedEvents, `{"type":"turn.started"}`, warning+`{"type":"turn.started"}`, 1)
	final, err := finalText([]byte(events))
	if err != nil || string(final) != `{"version":1}` {
		t.Fatalf("final=%s error=%v", final, err)
	}
	for name, invalid := range map[string]string{
		"before thread":      warning + completedEvents,
		"unfinished warning": strings.Replace(events, `"type":"item.completed"`, `"type":"item.started"`, 1),
		"early message":      strings.Replace(events, `"type":"error"`, `"type":"agent_message"`, 1),
		"missing turn":       strings.Replace(events, `{"type":"turn.started"}`, "", 1),
		"missing completion": strings.Split(events, `{"type":"turn.completed"`)[0],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(invalid)); err == nil {
				t.Fatal("accepted invalid stream")
			}
		})
	}
}
