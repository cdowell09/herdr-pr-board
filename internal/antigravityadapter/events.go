package antigravityadapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type step struct {
	Conversation string `json:"conversation_id"`
	Index        *int   `json:"step_index"`
	State        string `json:"state"`
	Type         string `json:"step_type"`
	Text         string `json:"text_delta"`
}

type stepState struct {
	Type, State string
	Text        strings.Builder
}

func finalText(data []byte, agent string) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	conversation := ""
	steps := map[int]*stepState{}
	last := -1
	var final []byte
	for {
		var event struct {
			Event        string `json:"event"`
			Conversation string `json:"conversation_id"`
			Init         struct {
				Agent string `json:"agent"`
			} `json:"init"`
			Step   step `json:"step_update"`
			Result struct {
				Conversation string          `json:"conversation_id"`
				Status       string          `json:"status"`
				Response     string          `json:"response"`
				Error        json.RawMessage `json:"error"`
				Turns        int             `json:"num_turns"`
			} `json:"result"`
		}
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Antigravity event: %w", err)
		}
		if final != nil {
			return nil, fmt.Errorf("antigravity emitted events after its terminal result")
		}
		switch event.Event {
		case "init":
			if conversation != "" || event.Conversation == "" || agent == "" || event.Init.Agent != agent {
				return nil, fmt.Errorf("antigravity did not select the isolated reviewer")
			}
			conversation = event.Conversation
		case "step_update":
			s := event.Step
			if conversation == "" || s.Conversation != conversation || s.Index == nil || *s.Index < 0 ||
				(s.State != "ACTIVE" && s.State != "DONE") || s.Type == "" {
				return nil, fmt.Errorf("antigravity returned an invalid or failed step")
			}
			previous := steps[*s.Index]
			if previous != nil && (previous.Type != s.Type || previous.State == "DONE") {
				return nil, fmt.Errorf("antigravity changed a completed step")
			}
			if previous == nil {
				previous = &stepState{Type: s.Type}
				steps[*s.Index] = previous
			}
			previous.State = s.State
			previous.Text.WriteString(s.Text)
			last = max(last, *s.Index)
		case "result":
			r := event.Result
			s := steps[last]
			if conversation == "" || r.Conversation != conversation || r.Status != "SUCCESS" || r.Turns < 1 || s == nil ||
				(len(r.Error) != 0 && !bytes.Equal(r.Error, []byte("null"))) ||
				s.State != "DONE" || s.Type != "agent_response" || strings.TrimSpace(s.Text.String()) == "" ||
				!strings.HasSuffix(r.Response, s.Text.String()) {
				return nil, fmt.Errorf("antigravity did not return a completed final response")
			}
			for _, pending := range steps {
				if pending.State != "DONE" {
					return nil, fmt.Errorf("antigravity left an incomplete step")
				}
			}
			final = []byte(s.Text.String())
		default:
			return nil, fmt.Errorf("antigravity returned an unsupported event %q", event.Event)
		}
	}
	if final == nil {
		return nil, fmt.Errorf("antigravity returned no terminal result")
	}
	return final, nil
}
