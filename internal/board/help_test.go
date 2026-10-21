package board

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// helpOverlayKeys returns every key literal the ? overlay names.
func helpOverlayKeys() map[string]bool {
	keys := map[string]bool{}
	separator := func(r rune) bool { return r == '/' || r == '–' }
	for _, section := range helpSections {
		for _, entry := range section.entries {
			for _, field := range strings.Fields(entry.keys) {
				keys[field] = true
				for _, part := range strings.FieldsFunc(field, separator) {
					keys[part] = true
				}
			}
		}
	}
	return keys
}

func TestHelpOverlayNamesEveryImplementedControl(t *testing.T) {
	keys := helpOverlayKeys()
	for _, key := range implementedKeys {
		if !keys[key] {
			t.Fatalf("the ? overlay does not name the %q control", key)
		}
	}
}

func TestHelpOverlayOpensAndClosesFromBoardAndReviewPanel(t *testing.T) {
	question := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}
	board := layoutModel(t, 80)
	next, _ := board.Update(question)
	opened := next.(Model)
	if !opened.helpOverlay {
		t.Fatal("? did not open the help overlay on the board")
	}
	overlay := stripANSI(opened.View())
	for _, want := range []string{"Board", "Detail pane", "Esc"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("overlay missing %q:\n%s", want, overlay)
		}
	}
	for _, key := range []tea.KeyMsg{question, {Type: tea.KeyEsc}} {
		closed, _ := opened.Update(key)
		if closed.(Model).helpOverlay {
			t.Fatalf("%q did not close the help overlay", key.String())
		}
	}

	panel := panelModel(t)
	next, _ = panel.Update(question)
	opened = next.(Model)
	if !opened.helpOverlay {
		t.Fatal("? did not open the help overlay in the review panel")
	}
	back, _ := opened.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if closed := back.(Model); closed.helpOverlay || !closed.zoom {
		t.Fatal("Esc did not return from the help overlay to the zoomed pane")
	}
}

func TestHelpModalSitsOverTheDimmedBoard(t *testing.T) {
	// Colors must render for the dimmed backdrop to be visible in the test.
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(profile)
	for _, size := range [][2]int{{50, 22}, {90, 28}, {140, 34}} {
		model := layoutModel(t, size[0])
		model.height = size[1]
		model.helpOverlay = true
		rendered := strings.Split(model.View(), "\n")
		if len(rendered) != size[1] {
			t.Fatalf("%v: help rendered %d lines, want %d", size, len(rendered), size[1])
		}
		for _, line := range rendered {
			if got := lipgloss.Width(line); got > size[0] {
				t.Fatalf("%v: help line is %d cells wide: %q", size, got, stripANSI(line))
			}
		}
		box := model.helpLayout()
		top := stripANSI(rendered[box.y])
		for _, want := range []string{"╭─ Keyboard controls", "[×] ─╮"} {
			if !strings.Contains(top, want) {
				t.Fatalf("%v: top edge %q lacks %q", size, top, want)
			}
		}
		// The board stays visible behind the modal, in one dim color.
		if box.y == 0 || rendered[0] == stripANSI(rendered[0]) || rendered[0] != backdropStyle.Render(stripANSI(rendered[0])) {
			t.Fatalf("%v: the line above the modal is not the dimmed board: %q", size, rendered[0])
		}
		if bottom := stripANSI(rendered[box.y+box.height-1]); !strings.Contains(bottom, "╰") {
			t.Fatalf("%v: the modal has no bottom edge at row %d: %q", size, box.y+box.height-1, bottom)
		}
	}
}

func TestHelpCloseControlIsTheOnlyMouseTargetInTheHelp(t *testing.T) {
	for _, width := range []int{50, 140} {
		model := layoutModel(t, width)
		model.helpOverlay = true
		lines := strings.Split(model.View(), "\n")
		closeY, closeX := -1, 0
		for y, line := range lines {
			if x, ok := displayColumn(stripANSI(line), "[×]"); ok {
				closeY, closeX = y, x
				break
			}
		}
		if closeY < 0 {
			t.Fatalf("width %d: no rendered close control:\n%s", width, stripANSI(model.View()))
		}
		for _, x := range []int{closeX - 1, closeX + 3} {
			if stayed, _ := click(model, x, closeY); !stayed.helpOverlay {
				t.Fatalf("width %d: a click beside the close control at x %d closed the help", width, x)
			}
		}
		for x := closeX; x < closeX+3; x++ {
			if closed, _ := click(model, x, closeY); closed.helpOverlay {
				t.Fatalf("width %d: a click on the rendered close control at %d,%d did not close the help", width, x, closeY)
			}
		}
		// A click on a board row behind the help reaches neither the row
		// nor the browser.
		opened := ""
		model.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
		behind := model
		behind.helpOverlay = false
		y := renderedRow(t, behind, "#42 ")
		for _, x := range []int{1, width / 2} {
			next, _ := click(model, x, y)
			if !next.helpOverlay || next.cursor != model.cursor || opened != "" {
				t.Fatalf("width %d: a click at %d,%d reached the board", width, x, y)
			}
		}
	}
}

func TestHelpOverlayKeepsTheFilterLineBehavior(t *testing.T) {
	model := layoutModel(t, 80)
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	filtering := next.(Model)
	if filtering.helpOverlay {
		t.Fatal("? opened the help overlay during filter input")
	}
	if filtering.filter != "?" {
		t.Fatalf("filter = %q, want %q", filtering.filter, "?")
	}
	if view := stripANSI(filtering.View()); !strings.Contains(view, "/ ?") {
		t.Fatalf("filter line missing:\n%s", view)
	}
}

func TestHelpOverlayFitsNarrowTerminalsAndKeepsEveryControl(t *testing.T) {
	// Wrapping keeps every character, but it can consume a space at a break.
	// Comparing without spaces makes the check exact at every width.
	compact := func(value string) string { return strings.ReplaceAll(value, " ", "") }
	for _, size := range [][2]int{{tierNarrow, 24}, {30, 10}, {12, 6}, {8, 6}, {5, 6}, {80, 24}, {120, 40}} {
		model := layoutModel(t, size[0])
		model.height = size[1]
		model.helpOverlay = true
		lines := model.helpLayout().lines
		model.helpOffset = len(lines)
		model.clampHelpOffset()
		rendered := strings.Split(model.View(), "\n")
		if len(rendered) > size[1] {
			t.Fatalf("%v: overlay rendered %d lines", size, len(rendered))
		}
		for _, line := range rendered {
			if got := lipgloss.Width(stripANSI(line)); got > size[0] {
				t.Fatalf("%v: overlay line is %d cells wide: %q", size, got, line)
			}
		}
		var plain strings.Builder
		for _, line := range lines {
			plain.WriteString(stripANSI(line))
		}
		content := compact(plain.String())
		for _, section := range helpSections {
			for _, entry := range section.entries {
				for _, want := range []string{section.title, entry.keys, entry.action} {
					if !strings.Contains(content, compact(want)) {
						t.Fatalf("%v: overlay lost %q", size, want)
					}
				}
			}
		}
		if last := stripANSI(lines[len(lines)-1]); !strings.Contains(stripANSI(model.View()), last) {
			t.Fatalf("%v: scrolling does not reach %q:\n%s", size, last, stripANSI(model.View()))
		}
	}
}

func TestHelpBackdropKeepsWideGlyphsInsideTheScreen(t *testing.T) {
	// A wide glyph that the right modal edge splits must not widen the row.
	for width := 120; width <= 150; width++ {
		model := layoutModel(t, width)
		model.views[0].PRs[0].Title = strings.Repeat("日本語のタイトル", 12)
		model.helpOverlay = true
		for y, line := range strings.Split(model.View(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: help row %d is %d cells wide: %q", width, y, got, stripANSI(line))
			}
		}
	}
}

func TestCutLeftReplacesASplitGraphemeWithSpaces(t *testing.T) {
	for _, tc := range []struct {
		line string
		from int
		want string
	}{
		{"ab日本x", 3, " 本x"},
		{"❤️x", 1, " x"},
		{"abc", 1, "bc"},
		{"abc", 5, ""},
	} {
		if got := cutLeft(tc.line, tc.from); got != tc.want {
			t.Fatalf("cutLeft(%q, %d) = %q, want %q", tc.line, tc.from, got, tc.want)
		}
	}
}
