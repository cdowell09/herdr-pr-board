package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestColorsComeFromThePalette keeps theme.go the only place that names a
// color, so every style uses a palette role.
func TestColorsComeFromThePalette(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file == "theme.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), "lipgloss.Color(") {
			t.Errorf("%s names a color; add a role to theme.go instead", file)
		}
	}
}
