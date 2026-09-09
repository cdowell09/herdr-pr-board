package copilotadapter

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

// Copilot may emit an assistant message and idle after exhausting length retries.
// Require the normalized provider stop, the same assistant turn, and its idle.
type transcript struct {
	response                 int
	session, api, turn, text string
	stopped, ended, idle     bool
}

func (t *transcript) accept(msg rpcMessage) error {
	if msg.JSONRPC != "2.0" || (len(msg.Error) != 0 && string(msg.Error) != "null") {
		return errors.New("copilot RPC failed")
	}
	if msg.ID != 0 {
		if msg.Method != "" || msg.ID != t.response+1 || msg.ID > 5 || len(msg.Result) == 0 {
			return errors.New("unexpected Copilot RPC response")
		}
		t.response = msg.ID
		var result struct {
			OK          bool   `json:"ok"`
			Protocol    int    `json:"protocolVersion"`
			Version     string `json:"version"`
			Session     string `json:"sessionId"`
			Message     string `json:"messageId"`
			Remote      bool   `json:"isRemote"`
			Success     bool   `json:"success"`
			PluginHooks int    `json:"pluginHookCount"`
		}
		if err := json.Unmarshal(msg.Result, &result); err != nil {
			return err
		}
		if msg.ID == 1 && (!result.OK || result.Protocol != 3 || result.Version != "1.0.83") {
			return errors.New("copilot adapter requires native CLI 1.0.83 and SDK protocol 3")
		}
		if msg.ID == 2 {
			if result.Session == "" || result.Remote {
				return errors.New("copilot did not create a local session")
			}
			t.session = result.Session
		}
		if msg.ID == 5 && result.Message == "" {
			return errors.New("copilot did not accept the review prompt")
		}
		if (msg.ID == 3 || msg.ID == 4) && (!result.Success || result.PluginHooks != 0) {
			return errors.New("copilot did not apply isolated session settings")
		}
		return nil
	}
	if msg.Method == "session.lifecycle" {
		return nil
	}
	if msg.Method != "session.event" {
		return errors.New("unsupported Copilot interaction")
	}
	if t.idle {
		return errors.New("copilot emitted an event after completion")
	}
	var params struct {
		Session string `json:"sessionId"`
		Event   struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"event"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return err
	}
	if t.session == "" || params.Session != t.session {
		return errors.New("copilot event belongs to another session")
	}
	var data struct {
		Content   string            `json:"content"`
		API       string            `json:"apiCallId"`
		Turn      string            `json:"turnId"`
		Mode      string            `json:"mode"`
		Aborted   bool              `json:"aborted"`
		Tools     []json.RawMessage `json:"toolRequests"`
		ModelCall struct {
			API string `json:"api_id"`
		} `json:"modelCall"`
		Chunk struct {
			Choices []struct {
				Finish string `json:"finish_reason"`
			} `json:"choices"`
		} `json:"responseChunk"`
	}
	if err := json.Unmarshal(params.Event.Data, &data); err != nil {
		return err
	}
	switch params.Event.Type {
	case "session.error", "abort", "permission.requested", "user_input.requested", "session.shutdown":
		return fmt.Errorf("copilot review did not complete: %s", params.Event.Type)
	case "assistant.turn_start":
		t.turn, t.api, t.text = data.Turn, "", ""
		t.stopped, t.ended = false, false
	case "model.model_call_success":
		t.text, t.ended = "", false
		t.api = data.ModelCall.API
		t.stopped = t.api != "" && len(data.Chunk.Choices) == 1 && data.Chunk.Choices[0].Finish == "stop"
	case "assistant.message":
		t.text = ""
		if t.stopped && data.API == t.api && data.Turn == t.turn && len(data.Tools) == 0 {
			t.text = data.Content
		}
	case "assistant.turn_end":
		t.ended = data.Turn != "" && data.Turn == t.turn
	case "session.idle":
		if t.response != 5 || data.Aborted || data.Mode != "interactive" || !t.stopped || !t.ended || t.text == "" {
			return errors.New("copilot returned no successful final response")
		}
		t.idle = true
	}
	return nil
}

func finalText(data []byte) ([]byte, error) {
	if len(data) > agentadapter.MaxEventBytes {
		return nil, errors.New("copilot event stream exceeds 32 MiB")
	}
	var state transcript
	remaining, err := readMessages(data, state.accept)
	if err != nil {
		return nil, err
	}
	if len(remaining) != 0 {
		return nil, errors.New("copilot exited with a partial RPC frame")
	}
	if !state.idle {
		return nil, errors.New("copilot exited before completing the review")
	}
	return []byte(state.text), nil
}
