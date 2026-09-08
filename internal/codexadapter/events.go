package codexadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// finalText accepts one completed Codex exec turn with a final agent message.
// Process success alone also occurs for streams that lack a usable result.
func finalText(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	thread, started, completed := false, false, false
	var final []byte
	for {
		var event struct {
			Type string `json:"type"`
			Item *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Codex event: %w", err)
		}
		if completed {
			return nil, errors.New("codex emitted events after turn.completed")
		}
		switch event.Type {
		case "thread.started":
			if thread || started {
				return nil, errors.New("codex emitted duplicate thread.started")
			}
			thread = true
		case "turn.started":
			if !thread || started {
				return nil, errors.New("codex emitted turn.started outside its initial position")
			}
			started = true
		case "item.started", "item.updated", "item.completed":
			if !started || event.Item == nil || event.Item.Type == "" {
				return nil, errors.New("codex emitted an invalid turn item")
			}
			// Codex settles todo lists and unfinished tool items after the final
			// message. Only a later completed agent message replaces that result.
			if event.Type == "item.completed" && event.Item.Type == "agent_message" {
				final = []byte(event.Item.Text)
			}
		case "turn.completed":
			if !started || len(final) == 0 {
				return nil, errors.New("codex did not finish with a final agent message")
			}
			completed = true
		case "turn.failed", "error":
			return nil, errors.New("codex review turn failed")
		default:
			return nil, fmt.Errorf("unrecognized Codex event %q", event.Type)
		}
	}
	if !completed {
		return nil, errors.New("codex did not emit turn.completed")
	}
	return final, nil
}
