package copilotadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func prepareIO(cmd *exec.Cmd, prompt string) (func(), error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	requests := make(chan []byte, 8)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case data := <-requests:
				if _, err := fmt.Fprintf(write, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
					write.Close()
					return
				}
				if _, err := write.Write(data); err != nil {
					write.Close()
					return
				}
			}
		}
	}()
	output := &rpcOutput{writer: cmd.Stdout, input: write, requests: requests, prompt: prompt, checkout: cmd.Dir}
	cmd.Stdin, cmd.Stdout = read, output
	cleanup := func() { close(stop); write.Close(); read.Close(); <-done }
	if err := output.request(1, "connect", map[string]any{}); err != nil {
		cleanup()
		return nil, err
	}
	return cleanup, nil
}

// The separate input writer keeps large prompts from blocking event draining.
// The runner owns process lifetime; EOF stops the native host after its final idle.
type rpcOutput struct {
	writer           io.Writer
	input            io.Closer
	requests         chan<- []byte
	prompt, checkout string
	buffer           []byte
	total            int
	state            transcript
}

func (w *rpcOutput) request(id int, method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	select {
	case w.requests <- data:
		return nil
	default:
		return errors.New("copilot request queue is full")
	}
}

func (w *rpcOutput) Write(data []byte) (int, error) {
	if err := w.consume(data); err != nil {
		w.input.Close()
		return 0, err
	}
	return len(data), nil
}

func (w *rpcOutput) consume(data []byte) error {
	w.total += len(data)
	if w.total > agentadapter.MaxEventBytes {
		return errors.New("copilot event stream exceeds 32 MiB")
	}
	// Preserve raw frames so finalText can reject any partial trailing frame.
	if _, err := w.writer.Write(data); err != nil {
		return err
	}
	w.buffer = append(w.buffer, data...)
	var err error
	w.buffer, err = readMessages(w.buffer, func(msg rpcMessage) error {
		if err := w.state.accept(msg); err != nil {
			return err
		}
		if msg.ID != 0 {
			if err := w.next(msg.ID); err != nil {
				return err
			}
		}
		if w.state.idle {
			w.input.Close()
		}
		return nil
	})
	return err
}

func readMessages(buffer []byte, accept func(rpcMessage) error) ([]byte, error) {
	for {
		end := bytes.Index(buffer, []byte("\r\n\r\n"))
		if end < 0 {
			if len(buffer) > 8192 {
				return nil, errors.New("copilot RPC header exceeds 8 KiB")
			}
			return buffer, nil
		}
		if end > 8192 {
			return nil, errors.New("copilot RPC header exceeds 8 KiB")
		}
		length := 0
		for _, line := range strings.Split(string(buffer[:end]), "\r\n") {
			key, value, found := strings.Cut(line, ":")
			if !found {
				return nil, errors.New("invalid Copilot RPC header")
			}
			if strings.EqualFold(key, "Content-Length") {
				if length != 0 {
					return nil, errors.New("duplicate Copilot Content-Length")
				}
				var err error
				length, err = strconv.Atoi(strings.TrimSpace(value))
				if err != nil || length <= 0 || length > agentadapter.MaxEventBytes {
					return nil, errors.New("invalid Copilot Content-Length")
				}
			}
		}
		if length == 0 {
			return nil, errors.New("missing Copilot Content-Length")
		}
		end += 4
		if len(buffer)-end < length {
			return buffer, nil
		}
		body := buffer[end : end+length]
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			return nil, fmt.Errorf("invalid Copilot RPC message: %w", err)
		}
		if err := accept(msg); err != nil {
			return nil, err
		}
		buffer = buffer[end+length:]
	}
}

func (w *rpcOutput) next(id int) error {
	params := map[string]any{"sessionId": w.state.session}
	switch id {
	case 1:
		return w.request(2, "session.create", sessionOptions(w.checkout))
	case 2:
		params["skipCustomInstructions"] = true
		params["installedPlugins"] = []any{}
		params["includedBuiltinSkills"] = []string{}
		return w.request(3, "session.options.update", params)
	case 3:
		params["approveAllReadPermissionRequests"] = true
		return w.request(4, "session.permissions.configure", params)
	case 4:
		params["prompt"] = w.prompt
		params["agentMode"] = "interactive"
		return w.request(5, "session.send", params)
	}
	return nil
}
