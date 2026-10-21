package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigHasNoActiveShortcutButShowsTheExample(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Shortcuts) != 0 {
		t.Fatalf("default shortcuts = %#v", cfg.Shortcuts)
	}
	// Uncommenting the example must give a valid shortcut.
	var example []string
	for _, line := range strings.Split(DefaultFile, "\n") {
		if strings.HasPrefix(line, "# [[shortcuts]]") || (len(example) > 0 && strings.HasPrefix(line, "# ")) {
			example = append(example, strings.TrimPrefix(line, "# "))
		}
	}
	if len(example) == 0 {
		t.Fatalf("default file lacks the shortcut example:\n%s", DefaultFile)
	}
	cfg, err = LoadExisting(writeConfigFile(t, DefaultFile+strings.Join(example, "\n")+"\n"))
	if err != nil {
		t.Fatalf("uncommented example: %v", err)
	}
	want := Shortcut{Key: "r", Name: "Pi review", Where: ShortcutTab, Cwd: "~"}
	got := cfg.Shortcuts[0]
	got.Run = ""
	if len(cfg.Shortcuts) != 1 || got != want || !strings.Contains(cfg.Shortcuts[0].Run, "{url}") {
		t.Fatalf("example shortcuts = %#v", cfg.Shortcuts)
	}
}

func TestShortcutWhereDefaultsToTab(t *testing.T) {
	cfg, err := LoadExisting(writeConfigFile(t, DefaultFile+"\n[[shortcuts]]\nkey = \"n\"\nname = \"Shell\"\nrun = \"echo {url}\"\n\n[[shortcuts]]\nkey = \"N\"\nname = \"Split\"\nrun = \"echo\"\nwhere = \"split\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Shortcuts) != 2 || cfg.Shortcuts[0].Where != ShortcutTab || cfg.Shortcuts[1].Where != ShortcutSplit {
		t.Fatalf("shortcuts = %#v", cfg.Shortcuts)
	}
}

func TestLoadRejectsInvalidShortcuts(t *testing.T) {
	entry := func(key, name, run, where string) string {
		return "\n[[shortcuts]]\nkey = " + key + "\nname = " + name + "\nrun = " + run + "\nwhere = " + where + "\n"
	}
	cases := []struct {
		name, shortcuts, want string
	}{
		{"empty key", entry(`""`, `"A"`, `"a"`, `"tab"`), "shortcuts[0].key must be one printable character"},
		{"two characters", entry(`"ab"`, `"A"`, `"a"`, `"tab"`), "shortcuts[0].key must be one printable character"},
		{"space", entry(`" "`, `"A"`, `"a"`, `"tab"`), "shortcuts[0].key must be one printable character"},
		{"control character", entry(`"\u0001"`, `"A"`, `"a"`, `"tab"`), "shortcuts[0].key must be one printable character"},
		{"view number", entry(`"3"`, `"A"`, `"a"`, `"tab"`), `shortcuts[0].key "3" is a built-in board key`},
		{"built-in key", entry(`"v"`, `"A"`, `"a"`, `"tab"`), `shortcuts[0].key "v" is a built-in board key`},
		{"duplicate key", entry(`"r"`, `"A"`, `"a"`, `"tab"`) + entry(`"r"`, `"B"`, `"b"`, `"tab"`), `duplicate shortcut key "r"`},
		{"empty name", entry(`"r"`, `" "`, `"a"`, `"tab"`), "shortcuts[0].name is required"},
		{"empty run", entry(`"r"`, `"A"`, `""`, `"tab"`), "shortcuts[0].run is required"},
		{"unknown where", entry(`"r"`, `"A"`, `"a"`, `"window"`), "shortcuts[0].where must be tab or split"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadExisting(writeConfigFile(t, DefaultFile+tc.shortcuts))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestReservedKeysCoverEveryViewNumber(t *testing.T) {
	for _, key := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		if !Reserved(key) {
			t.Fatalf("view number %q is not reserved", key)
		}
	}
	for _, key := range []string{"r", "n", "N", "0"} {
		if Reserved(key) {
			t.Fatalf("%q is reserved but the board leaves it free", key)
		}
	}
}
