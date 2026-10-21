package config

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ShortcutWhere is where a command shortcut opens its Herdr pane.
type ShortcutWhere string

const (
	// ShortcutTab opens a new focused Herdr tab. It is the default.
	ShortcutTab ShortcutWhere = "tab"
	// ShortcutSplit splits the board pane to the right.
	ShortcutSplit ShortcutWhere = "split"
)

// Shortcut is a command shortcut: one key that runs a command for the
// selected PR in a new Herdr pane. Run may contain {url}, {repo}, and
// {number}.
type Shortcut struct {
	Key   string        `toml:"key"`
	Name  string        `toml:"name"`
	Run   string        `toml:"run"`
	Where ShortcutWhere `toml:"where"`
	Cwd   string        `toml:"cwd"`
}

// ReservedKeys are the single-character keys the board session binds, besides
// the view numbers 1 to 9. A command shortcut cannot use one. The board builds
// its documented key list from it, and a board test fails when the board binds
// a key outside it.
var ReservedKeys = []string{
	"q", "h", "l", "j", "k", "g", "G", "/", "?", "v", "E", "R", "o",
	"t", "s", "c", "a", "x", "y",
}

// Reserved reports whether the board session binds key: a view number from
// 1 to 9 or one of ReservedKeys.
func Reserved(key string) bool {
	return (len(key) == 1 && key[0] >= '1' && key[0] <= '9') || slices.Contains(ReservedKeys, key)
}

// shortcutExample is the commented command shortcut in a new configuration
// file. No command shortcut is active by default.
const shortcutExample = `
# Command shortcuts run your own command for the selected PR in a new Herdr pane.
# [[shortcuts]]
# key   = "r"
# name  = "Pi review"
# run   = 'pi --skill ~/.agents/skills/code-review "Use the code-review skill to review {url}."'
# where = "tab"   # or "split"
# cwd   = "~"     # optional
`

func validateShortcuts(shortcuts []Shortcut) error {
	seen := make(map[string]bool, len(shortcuts))
	for i, shortcut := range shortcuts {
		prefix := fmt.Sprintf("shortcuts[%d]", i)
		r, size := utf8.DecodeRuneInString(shortcut.Key)
		if size == 0 || size != len(shortcut.Key) || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%s.key must be one printable character", prefix)
		}
		if Reserved(shortcut.Key) {
			return fmt.Errorf("%s.key %q is a built-in board key", prefix, shortcut.Key)
		}
		if seen[shortcut.Key] {
			return fmt.Errorf("duplicate shortcut key %q", shortcut.Key)
		}
		seen[shortcut.Key] = true
		if strings.TrimSpace(shortcut.Name) == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if strings.TrimSpace(shortcut.Run) == "" {
			return fmt.Errorf("%s.run is required", prefix)
		}
		if shortcut.Where != ShortcutTab && shortcut.Where != ShortcutSplit {
			return fmt.Errorf("%s.where must be tab or split", prefix)
		}
	}
	return nil
}
