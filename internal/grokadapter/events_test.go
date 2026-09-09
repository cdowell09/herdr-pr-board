package grokadapter

import (
	"encoding/json"
	"strings"
	"testing"
)

// Shape captured from the native 1.0.24 probe, without usage or UUID metadata.
func transcript(text string) []byte {
	quoted, _ := json.Marshal(text)
	return []byte(`{"type":"system","subtype":"init","session_id":"session","tools":["read_file","list_dir","grep"],"mcp_servers":[],"skills":[]}` + "\n" +
		`{"type":"assistant","session_id":"session","parent_tool_use_id":null,"message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":` + string(quoted) + `}]}}` + "\n" +
		`{"type":"result","subtype":"success","session_id":"session","is_error":false,"stop_reason":"end_turn","result":` + string(quoted) + `}` + "\n")
}

func TestFinalTextRequiresCorrelatedNativeCompletion(t *testing.T) {
	data := transcript(`{"review":"complete"}`)
	text, err := finalText(data)
	if err != nil || string(text) != `{"review":"complete"}` {
		t.Fatalf("text=%s error=%v", text, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for name, input := range map[string]string{
		"empty":                 "",
		"missing result":        strings.Join(lines[:2], "\n"),
		"result without init":   lines[2],
		"missing assistant":     lines[0] + "\n" + lines[2],
		"malformed event":       strings.Join(lines[:2], "\n") + "\n{",
		"truncated provider":    strings.Replace(string(data), `"stop_reason":"end_turn"`, `"stop_reason":"max_tokens"`, 1),
		"truncated result":      strings.Join(lines[:2], "\n") + "\n" + strings.Replace(lines[2], "end_turn", "max_tokens", 1),
		"error result":          strings.Replace(string(data), `"is_error":false`, `"is_error":true`, 1),
		"contradictory errors":  strings.Replace(string(data), `"is_error":false`, `"is_error":false,"errors":["failed"]`, 1),
		"missing error status":  strings.Replace(string(data), `"is_error":false,`, "", 1),
		"failed subtype":        strings.Replace(string(data), `"subtype":"success"`, `"subtype":"error_during_execution"`, 1),
		"result text mismatch":  strings.Join(lines[:2], "\n") + "\n" + strings.Replace(lines[2], "complete", "different", 1),
		"foreign session":       strings.Join(lines[:2], "\n") + "\n" + strings.Replace(lines[2], `"session_id":"session"`, `"session_id":"another"`, 1),
		"subagent output":       strings.Replace(string(data), `"parent_tool_use_id":null`, `"parent_tool_use_id":"parent"`, 1),
		"tool in final message": strings.Replace(string(data), `"type":"text"`, `"type":"tool_use"`, 1),
		"inherited tool":        strings.Replace(string(data), `"read_file","list_dir","grep"`, `"read_file","list_dir","grep","run_terminal_cmd"`, 1),
		"inherited skill":       strings.Replace(string(data), `"skills":[]`, `"skills":["unselected"]`, 1),
		"inherited MCP":         strings.Replace(string(data), `"mcp_servers":[]`, `"mcp_servers":[{}]`, 1),
		"missing tool manifest": strings.Replace(string(data), `"tools":["read_file","list_dir","grep"],`, "", 1),
		"duplicate init":        lines[0] + "\n" + string(data),
		"duplicate result":      string(data) + lines[2],
		"event after result":    string(data) + `{"type":"user","session_id":"session"}`,
		"unknown event":         lines[0] + "\n" + `{"type":"error","session_id":"session"}` + "\n" + strings.Join(lines[1:], "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalText([]byte(input)); err == nil {
				t.Fatal("incomplete or unisolated native transcript accepted")
			}
		})
	}
}

func TestToolRoundClearsEarlierAssistantText(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(string(transcript("final"))), "\n")
	tool := `{"type":"assistant","session_id":"session","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use"}]}}`
	response := `{"type":"user","session_id":"session","message":{"role":"user","content":[{"type":"tool_result"}]}}`
	input := lines[0] + "\n" + lines[1] + "\n" + tool + "\n" + response + "\n" + lines[2]
	if _, err := finalText([]byte(input)); err == nil {
		t.Fatal("earlier assistant text survived an unfinished tool round")
	}
	input = lines[0] + "\n" + tool + "\n" + response + "\n" + strings.Join(lines[1:], "\n")
	if text, err := finalText([]byte(input)); err != nil || string(text) != "final" {
		t.Fatalf("completed tool round: text=%s error=%v", text, err)
	}
}
