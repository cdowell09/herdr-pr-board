package copilotadapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkPolicyDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		// Windows can report a regular file as a missing directory.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("cannot exclude Copilot machine policy hooks: %w", err)
	}
	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			return fmt.Errorf("copilot machine policy hooks prevent isolated reviews: %s", path)
		}
	}
	return nil
}
