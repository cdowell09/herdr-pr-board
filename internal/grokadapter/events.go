package grokadapter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

func finalText(data []byte) ([]byte, error) {
	if len(data) > agentadapter.MaxEventBytes {
		return nil, errors.New("grok event stream exceeds 32 MiB")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), agentadapter.MaxEventBytes)
	var session, text string
	var complete bool
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type, Subtype string
			Session       string `json:"session_id"`
			IsError       *bool  `json:"is_error"`
			StopReason    string `json:"stop_reason"`
			Result        string `json:"result"`
			Errors        []json.RawMessage
			Parent        *string `json:"parent_tool_use_id"`
			Tools         []string
			Skills        []json.RawMessage
			MCP           []json.RawMessage `json:"mcp_servers"`
			Message       struct {
				Role       string
				StopReason string `json:"stop_reason"`
				Content    []struct{ Type, Text string }
			}
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("decode Grok event: %w", err)
		}
		if complete || event.Session == "" || (session != "" && event.Session != session) || event.Parent != nil {
			return nil, errors.New("unexpected Grok session event")
		}
		switch event.Type {
		case "system":
			slices.Sort(event.Tools)
			if session != "" || event.Subtype != "init" || !slices.Equal(event.Tools, []string{"grep", "list_dir", "read_file"}) ||
				event.Skills == nil || len(event.Skills) != 0 || event.MCP == nil || len(event.MCP) != 0 {
				return nil, errors.New("grok did not start an isolated review session")
			}
			session = event.Session
		case "assistant":
			if session == "" || event.Message.Role != "assistant" {
				return nil, errors.New("unexpected Grok assistant message")
			}
			text = ""
			if event.Message.StopReason == "end_turn" {
				var content strings.Builder
				for _, block := range event.Message.Content {
					if block.Type == "text" {
						content.WriteString(block.Text)
					} else if block.Type != "thinking" {
						return nil, errors.New("grok final message contains unfinished tool activity")
					}
				}
				text = content.String()
			}
		case "user":
			if session == "" {
				return nil, errors.New("grok tool response preceded initialization")
			}
			text = ""
		case "result":
			if session == "" || event.Subtype != "success" || event.IsError == nil || *event.IsError || len(event.Errors) != 0 ||
				event.StopReason != "end_turn" || text == "" || event.Result != text {
				return nil, errors.New("grok did not return a successful final response")
			}
			complete = true
		default:
			return nil, fmt.Errorf("unsupported Grok event type %q", event.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !complete {
		return nil, errors.New("grok exited before completing the review")
	}
	return []byte(text), nil
}
