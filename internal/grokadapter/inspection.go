package grokadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
)

func prepare(ctx context.Context, binary string, env []string, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.Command(binary, args...)
	cmd.Dir, cmd.Env = dir, env
	if err := cli.ResolveNodeShim(cmd); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	limited := &cli.LimitedWriter{Writer: &output, Remaining: 1024 * 1024}
	cmd.Stdout, cmd.Stderr = limited, io.Discard
	if err := cli.RunProcess(ctx, cmd, time.Second); err != nil {
		return nil, err
	}
	if limited.Truncated {
		return nil, errors.New("grok preparation output exceeds 1 MiB")
	}
	return output.Bytes(), nil
}

// The native inspector uses the runtime's configuration resolvers, including
// macOS forced preferences. Reject other layers before starting an agent.
func checkInspection(data []byte, config string) error {
	var report struct {
		Version       string `json:"grokVersion"`
		ConfigSources struct {
			Layers []struct{ Role, Path, Note string } `json:"layers"`
		} `json:"configSources"`
		Permissions *struct {
			ManagedSettingsExists *bool `json:"managedSettingsExists"`
		} `json:"permissions"`
		ExternalCompat struct {
			RemoteSettingsLoaded *bool                     `json:"remoteSettingsLoaded"`
			Cells                []struct{ Enabled *bool } `json:"cells"`
		} `json:"externalCompat"`
		Agents []struct {
			Source struct{ Type string }
		}
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode Grok configuration inspection: %w", err)
	}
	if report.Version != "1.0.24" {
		return errors.New("grok adapter requires native CLI 1.0.24")
	}
	layers := report.ConfigSources.Layers
	if len(layers) != 1 || layers[0].Role != "user" || layers[0].Note != "" {
		return errors.New("grok discovered configuration outside the isolated review settings")
	}
	// Native reports can use another spelling of the same file on Windows or macOS.
	actual, err := os.Stat(layers[0].Path)
	if err != nil {
		return err
	}
	expected, err := os.Stat(config)
	if err != nil || !os.SameFile(actual, expected) {
		return errors.New("grok did not load the isolated review settings")
	}
	if report.Permissions == nil || report.Permissions.ManagedSettingsExists == nil || *report.Permissions.ManagedSettingsExists ||
		report.ExternalCompat.RemoteSettingsLoaded == nil || *report.ExternalCompat.RemoteSettingsLoaded || len(report.ExternalCompat.Cells) == 0 {
		return errors.New("grok managed settings prevent isolated reviews")
	}
	for _, cell := range report.ExternalCompat.Cells {
		if cell.Enabled == nil || *cell.Enabled {
			return errors.New("grok compatibility discovery is still enabled")
		}
	}
	if report.Agents == nil {
		return errors.New("grok did not report agent discovery")
	}
	for _, agent := range report.Agents {
		if agent.Source.Type != "builtin" {
			return errors.New("grok discovered an unselected agent")
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"projectInstructions", "hooks", "skills", "plugins", "marketplaces", "mcpServers", "lspServers"} {
		var entries []json.RawMessage
		if err := json.Unmarshal(fields[name], &entries); err != nil || entries == nil || len(entries) != 0 {
			return fmt.Errorf("grok discovered unexpected %s", name)
		}
	}
	return nil
}
