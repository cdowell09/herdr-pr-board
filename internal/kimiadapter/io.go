package kimiadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

func prepareIO(cmd *exec.Cmd, prompt string) (func(), error) {
	request, err := json.Marshal(struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      string            `json:"id"`
		Method  string            `json:"method"`
		Params  map[string]string `json:"params"`
	}{"2.0", promptID, "prompt", map[string]string{"user_input": prompt}})
	if err != nil {
		return nil, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdin = read
	cmd.Stdout = &wireOutput{writer: cmd.Stdout, input: write}
	// Kimi cancels active work on stdin EOF. Keep the pipe open until its
	// response arrives. A file-backed stdin also lets process exit unblock Wait.
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		if _, err := write.Write(append(request, '\n')); err != nil {
			write.Close()
		}
	}()
	return func() {
		write.Close()
		read.Close()
		<-sent
	}, nil
}

// wireOutput preserves the runner's bounded log and closes stdin on a response
// or unsupported interaction. finalText validates the complete saved transcript.
type wireOutput struct {
	writer io.Writer
	input  io.Closer
	line   []byte
	total  int
}

func (w *wireOutput) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if err != nil {
		w.input.Close()
		return n, err
	}
	w.total += len(data)
	if w.total > agentadapter.MaxEventBytes {
		w.input.Close()
		return n, errors.New("kimi event stream exceeds 32 MiB")
	}
	for len(data) != 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			w.line = append(w.line, data...)
			break
		}
		w.line = append(w.line, data[:i]...)
		var msg wireMessage
		if err := json.Unmarshal(w.line, &msg); err != nil {
			w.input.Close()
			return n, err
		}
		if msg.Method != "event" || msg.JSONRPC != "2.0" || (len(msg.Error) != 0 && string(msg.Error) != "null") {
			w.input.Close()
		}
		w.line = w.line[:0]
		data = data[i+1:]
	}
	return n, nil
}
