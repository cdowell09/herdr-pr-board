package ompadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Oh My Pi 18.1.14 explicitly marks the final settled agent_end isTerminal.
// Intermediate agent_end events can schedule continuations and prove nothing.
func finalText(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var output []byte
	for {
		var event struct {
			Type       string            `json:"type"`
			IsTerminal *bool             `json:"isTerminal"`
			Messages   []json.RawMessage `json:"messages"`
		}
		if err := dec.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Oh My Pi event: %w", err)
		}
		if output != nil {
			return nil, errors.New("oh my pi emitted events after its terminal result")
		}
		if event.Type != "agent_end" {
			continue
		}
		if event.IsTerminal == nil {
			return nil, errors.New("oh my pi omitted its terminal status")
		}
		if !*event.IsTerminal {
			continue
		}
		if len(event.Messages) == 0 {
			return nil, errors.New("oh my pi returned no final message")
		}
		var message struct {
			Role       string `json:"role"`
			StopReason string `json:"stopReason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(event.Messages[len(event.Messages)-1], &message); err != nil {
			return nil, err
		}
		if message.Role != "assistant" || message.StopReason != "stop" {
			return nil, errors.New("oh my pi did not finish its assistant response")
		}
		var text strings.Builder
		for _, part := range message.Content {
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		}
		if text.Len() == 0 {
			return nil, errors.New("oh my pi returned no final result")
		}
		output = []byte(text.String())
	}
	if output == nil {
		return nil, errors.New("oh my pi returned no terminal result")
	}
	return output, nil
}
