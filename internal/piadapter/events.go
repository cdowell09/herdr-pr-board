// Package piadapter translates Pi's event stream into a validated review result.
package piadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// finalText follows Pi 0.84.2's AgentEvent contract. JSON mode can exit zero
// after an assistant error, so only the final agent_end message proves a turn ended.
func finalText(data []byte) ([]byte, error) {
	type message struct {
		Role       string `json:"role"`
		StopReason string `json:"stopReason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var final *message
	ended, settled := false, false
	for {
		var event struct {
			Type      string            `json:"type"`
			WillRetry bool              `json:"willRetry"`
			Messages  []json.RawMessage `json:"messages"`
		}
		if err := dec.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Pi event: %w", err)
		}
		if event.Type == "agent_settled" {
			if !ended || settled {
				return nil, errors.New("pi emitted agent_settled outside its terminal position")
			}
			settled = true
			continue
		}
		if ended {
			return nil, errors.New("pi emitted events after agent_end")
		}
		if event.Type == "agent_end" {
			if event.WillRetry {
				return nil, errors.New("pi review turn requires a retry")
			}
			ended = true
			if len(event.Messages) > 0 {
				final = new(message)
				if err := json.Unmarshal(event.Messages[len(event.Messages)-1], final); err != nil {
					return nil, fmt.Errorf("decode final Pi message: %w", err)
				}
			}
		}
	}
	if !ended || final == nil || final.Role != "assistant" || final.StopReason != "stop" {
		return nil, errors.New("pi did not finish with a successful assistant result")
	}
	var text strings.Builder
	for _, part := range final.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if text.Len() == 0 {
		return nil, errors.New("pi returned no final result")
	}
	return []byte(text.String()), nil
}
