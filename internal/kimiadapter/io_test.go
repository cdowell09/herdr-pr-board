package kimiadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "kimi-wire" {
		if err := runWireFixture(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runWireFixture() error {
	mode := os.Getenv("KIMI_TEST_MODE")
	if mode == "blocked_input" {
		time.Sleep(30 * time.Second)
		return nil
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	var request struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Input string `json:"user_input"`
		} `json:"params"`
	}
	if err := json.Unmarshal(line, &request); err != nil {
		return err
	}
	if request.JSONRPC != "2.0" || request.ID != promptID || request.Method != "prompt" || request.Params.Input != os.Getenv("KIMI_TEST_PROMPT") {
		return errors.New("wrong Wire prompt")
	}
	if mode == "early_exit" {
		return errors.New("provider exited")
	}
	eof := make(chan error, 1)
	go func() { _, err := reader.ReadByte(); eof <- err }()
	select {
	case <-eof:
		return errors.New("stdin closed before the terminal response")
	case <-time.After(20 * time.Millisecond):
	}
	data := transcript(`{"version":1}`)
	if mode == "failure" {
		data = strings.Replace(data, `"finished"`, `"cancelled"`, 1)
	}
	for i := 0; i < len(data); i += 7 {
		if _, err := io.WriteString(os.Stdout, data[i:min(i+7, len(data))]); err != nil {
			return err
		}
	}
	if err := <-eof; err != io.EOF {
		return fmt.Errorf("stdin did not close after response: %v", err)
	}
	return nil
}

func TestWireIOKeepsInputOpenUntilResponseAndCleansUp(t *testing.T) {
	for _, mode := range []string{"completed", "failure", "early_exit", "blocked_input", "launch_failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
			t.Setenv("KIMI_TEST_MODE", mode)
			prompt := "Review quoted text \"$(ignored)\"\nwith literal `backticks`."
			t.Setenv("KIMI_TEST_PROMPT", prompt)
			if mode == "blocked_input" {
				prompt = strings.Repeat("large prompt", 1024*1024)
			}
			cmd := exec.Command(testutil.Executable(t, t.TempDir(), "kimi-wire"))
			var logs bytes.Buffer
			cmd.Stdout = &cli.LimitedWriter{Writer: &logs, Remaining: agentadapter.MaxEventBytes}
			cmd.Stderr = io.Discard
			cleanup, err := prepareIO(cmd, prompt)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "launch_failure" {
				cmd.Path = filepath.Join(t.TempDir(), "missing-kimi")
			}
			timeout := 10 * time.Second
			if mode == "blocked_input" {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			err = cli.RunProcess(ctx, cmd, 10*time.Millisecond)
			cleaned := make(chan struct{})
			go func() { cleanup(); close(cleaned) }()
			select {
			case <-cleaned:
			case <-time.After(2 * time.Second):
				t.Fatal("protocol cleanup left the input writer blocked")
			}
			if mode == "blocked_input" {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("blocked input did not stop on cancellation: %v", err)
				}
				return
			}
			if mode == "early_exit" || mode == "launch_failure" {
				if err == nil || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected process failure before deadline: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = finalText(logs.Bytes())
			if (err == nil) != (mode == "completed") {
				t.Fatalf("terminal validation error=%v", err)
			}
		})
	}
}

type closeFlag struct{ closed bool }

func (c *closeFlag) Close() error { c.closed = true; return nil }

func TestWireOutputClosesInputOnMalformedOrOversizedOutput(t *testing.T) {
	for _, data := range [][]byte{[]byte("invalid json\n"), bytes.Repeat([]byte("x"), agentadapter.MaxEventBytes+1)} {
		input := &closeFlag{}
		var logs bytes.Buffer
		output := &wireOutput{writer: &cli.LimitedWriter{Writer: &logs, Remaining: agentadapter.MaxEventBytes}, input: input}
		if _, err := output.Write(data); err == nil || !input.closed {
			t.Fatalf("error=%v closed=%v", err, input.closed)
		}
		if logs.Len() > agentadapter.MaxEventBytes {
			t.Fatal("protocol bypassed the bounded log")
		}
	}
}
