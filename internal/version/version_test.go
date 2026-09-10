package version

import (
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestCurrentMatchesThePluginManifest(t *testing.T) {
	data, err := os.ReadFile("../../herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version string `toml:"version"`
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version == "" {
		t.Fatal("herdr-plugin.toml defines no top-level version")
	}
	if Current != manifest.Version {
		t.Fatalf("Current = %q, want the herdr-plugin.toml version %q", Current, manifest.Version)
	}
}

func TestDescribeReportsTheShortRevision(t *testing.T) {
	cases := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{name: "clean checkout", info: buildInfo("e633d4f01b00e4b6fa0cf1a826e32d92dfecdf16", "false"), ok: true, want: Current + " (e633d4f)"},
		{name: "modified checkout", info: buildInfo("e633d4f01b00e4b6fa0cf1a826e32d92dfecdf16", "true"), ok: true, want: Current + " (e633d4f-dirty)"},
		{name: "short revision", info: buildInfo("e633d", "false"), ok: true, want: Current + " (e633d)"},
		{name: "no revision", info: &debug.BuildInfo{}, ok: true, want: Current},
		{name: "no build information", info: nil, ok: false, want: Current},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := describe(tc.info, tc.ok)
			if got != tc.want {
				t.Fatalf("describe() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("describe() = %q, want one line", got)
			}
		})
	}
}

func TestStringStartsWithTheCurrentVersion(t *testing.T) {
	got := String()
	if !strings.HasPrefix(got, Current) {
		t.Fatalf("String() = %q, want the %q prefix", got, Current)
	}
}

func buildInfo(revision, modified string) *debug.BuildInfo {
	return &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.modified", Value: modified},
	}}
}
