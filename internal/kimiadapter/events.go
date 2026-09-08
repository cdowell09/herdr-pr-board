package kimiadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const promptID = "pr-board-review"

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Error   json.RawMessage `json:"error"`
	Result  *struct {
		Status string `json:"status"`
	} `json:"result"`
	Params struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	} `json:"params"`
}

// The finished response belongs to our prompt request. TurnEnd alone is not
// sufficient: Kimi emits it from failure cleanup as well as successful turns.
func finalText(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var text strings.Builder
	var begun, stepped, ended, terminal, tools bool
	for {
		var msg wireMessage
		if err := dec.Decode(&msg); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Kimi event: %w", err)
		}
		if terminal || msg.JSONRPC != "2.0" || (len(msg.Error) != 0 && string(msg.Error) != "null") {
			return nil, errors.New("kimi returned an invalid or failed Wire response")
		}
		if msg.Method != "event" {
			if msg.Method != "" || msg.ID != promptID || msg.Result == nil || msg.Result.Status != "finished" || !ended {
				return nil, errors.New("kimi did not return a successful terminal response for the review")
			}
			terminal = true
			continue
		}
		if msg.ID != "" || msg.Result != nil {
			return nil, errors.New("kimi returned an invalid Wire event")
		}
		switch msg.Params.Type {
		case "TurnBegin":
			if begun {
				return nil, errors.New("kimi started an unexpected review turn")
			}
			begun = true
		case "StepBegin", "StepRetry":
			if !begun || ended {
				return nil, errors.New("kimi emitted a step outside the review turn")
			}
			text.Reset()
			stepped, tools = true, false
		case "ContentPart":
			if !stepped || ended {
				return nil, errors.New("kimi emitted text outside a review step")
			}
			var part struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(msg.Params.Payload, &part); err != nil {
				return nil, err
			}
			if part.Type == "text" {
				text.WriteString(part.Text)
			}
		case "ToolCall", "ToolCallPart", "ToolResult":
			if !stepped || ended {
				return nil, errors.New("kimi emitted a tool event outside a review step")
			}
			tools = true
		case "StepInterrupted":
			return nil, errors.New("kimi interrupted the review step")
		case "TurnEnd":
			if !stepped || ended || tools || text.Len() == 0 {
				return nil, errors.New("kimi returned no final assistant result")
			}
			ended = true
		}
	}
	if !terminal {
		return nil, errors.New("kimi returned no terminal review response")
	}
	return []byte(text.String()), nil
}
