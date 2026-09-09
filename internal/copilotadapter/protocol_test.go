package copilotadapter

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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "copilot-peer" {
		if err := runPeer(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
	if err != nil || n < 0 || n > agentadapter.MaxEventBytes {
		return nil, errors.New("invalid frame size")
	}
	if _, err := reader.ReadString('\n'); err != nil {
		return nil, err
	}
	data := make([]byte, n)
	_, err = io.ReadFull(reader, data)
	return data, err
}

func runPeer() error {
	reader := bufio.NewReader(os.Stdin)
	mode := os.Getenv("COPILOT_TEST_MODE")
	data, err := os.ReadFile(os.Getenv("COPILOT_TEST_FIXTURE"))
	if err != nil {
		return err
	}
	var events [][]byte
	responses := make(map[int][]byte)
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			return err
		}
		if msg.ID != 0 {
			responses[msg.ID] = line
		} else {
			events = append(events, line)
		}
	}
	for id := 1; id <= 5; id++ {
		if id == 5 && mode == "blocked_input" {
			// Consume only the prompt frame header. Its large body fills the pipe.
			for range 2 {
				if _, err := reader.ReadString('\n'); err != nil {
					return err
				}
			}
			if err := os.WriteFile(os.Getenv("COPILOT_TEST_READY"), []byte("ready"), 0600); err != nil {
				return err
			}
			time.Sleep(30 * time.Second)
			return nil
		}
		data, err := readFrame(reader)
		if err != nil {
			return err
		}
		var msg struct {
			ID     int                        `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		if msg.ID != id {
			return fmt.Errorf("unexpected request id %d", msg.ID)
		}
		if id == 2 && string(msg.Params["enableConfigDiscovery"]) != "false" {
			return errors.New("config discovery enabled")
		}
		if id == 3 && string(msg.Params["installedPlugins"]) != "[]" {
			return errors.New("plugins enabled")
		}
		if id == 4 && string(msg.Params["approveAllReadPermissionRequests"]) != "true" {
			return errors.New("read permissions unavailable")
		}
		if id == 5 {
			var prompt string
			if err := json.Unmarshal(msg.Params["prompt"], &prompt); err != nil {
				return err
			}
			if prompt != os.Getenv("COPILOT_TEST_PROMPT") {
				return errors.New("prompt changed")
			}
		}
		if mode == "early_exit" {
			return errors.New("early provider exit")
		}
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(responses[id]), responses[id]); err != nil {
			return err
		}
	}
	for _, event := range events {
		frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(event), event)
		for i := 0; i < len(frame); i += 7 {
			if _, err := io.WriteString(os.Stdout, frame[i:min(i+7, len(frame))]); err != nil {
				return err
			}
		}
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		return fmt.Errorf("expected stdin EOF, got %v", err)
	}
	return nil
}

func TestRPCIOAndCleanup(t *testing.T) {
	for _, mode := range []string{"completed", "early_exit", "blocked_input", "launch_failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
			t.Setenv("COPILOT_TEST_MODE", mode)
			ready := filepath.Join(t.TempDir(), "ready")
			t.Setenv("COPILOT_TEST_READY", ready)
			fixture, err := filepath.Abs("testdata/native-sdk.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("COPILOT_TEST_FIXTURE", fixture)
			prompt := "Review \"quotes\", literal `backticks`, and $(text).\n"
			t.Setenv("COPILOT_TEST_PROMPT", prompt)
			if mode == "blocked_input" {
				prompt = strings.Repeat(prompt, 300000)
			}
			cmd := exec.Command(testutil.Executable(t, t.TempDir(), "copilot-peer"))
			cmd.Dir = t.TempDir()
			var log, diagnostic bytes.Buffer
			cmd.Stdout = &cli.LimitedWriter{Writer: &log, Remaining: agentadapter.MaxEventBytes}
			cmd.Stderr = &diagnostic
			cleanup, err := prepareIO(cmd, prompt)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "launch_failure" {
				cmd.Path = filepath.Join(t.TempDir(), "missing")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			process := make(chan error, 1)
			go func() { process <- cli.RunProcess(ctx, cmd, 10*time.Millisecond) }()
			if mode == "blocked_input" {
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					select {
					case <-ticker.C:
					case err := <-process:
						cleanup()
						t.Fatalf("peer exited before blocking on prompt input: %v", err)
					case <-ctx.Done():
						<-process
						cleanup()
						t.Fatal("peer never reached the prompt input barrier")
					}
				}
				cancel()
			}
			err = <-process
			done := make(chan struct{})
			go func() { cleanup(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("input writer remains blocked")
			}
			if mode != "completed" {
				if err == nil {
					t.Fatal("expected process failure")
				}
				if errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("startup timeout does not establish %s: %v", mode, err)
				}
				if mode == "blocked_input" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("blocked input did not stop on cancellation: %v", err)
					}
					var state transcript
					if _, err := readMessages(log.Bytes(), state.accept); err != nil || state.response != 4 {
						t.Fatalf("cancellation preceded four RPC responses: response=%d err=%v", state.response, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("process: %v: %s", err, &diagnostic)
			}
			final, err := finalText(log.Bytes())
			if err != nil || string(final) != "OFFLINE_RESULT" {
				t.Fatalf("final=%s error=%v", final, err)
			}
		})
	}
}

type closeFlag struct{ closed bool }

func (c *closeFlag) Close() error { c.closed = true; return nil }

func TestMalformedRPCClosesInput(t *testing.T) {
	for _, data := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", "Content-Length: 1\r\n\r\nx", strings.Repeat("x", 8193)} {
		closed := &closeFlag{}
		w := &rpcOutput{writer: io.Discard, input: closed}
		if _, err := w.Write([]byte(data)); err == nil || !closed.closed {
			t.Fatalf("error=%v closed=%v", err, closed.closed)
		}
	}
}
