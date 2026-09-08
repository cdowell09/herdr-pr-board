package board

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRepositoryGroupsPreserveRowsAndMouseTargets(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 10}} {
		m := onboardingModel(t, size[0], size[1], 3)
		s := m.reviewPanel.setup
		for _, heading := range []string{"Reviews", "GitHub permissions", "Automatic posting", "Global views"} {
			found := -1
			for i, line := range m.repositoryContent() {
				if stripANSI(line.text) == heading {
					found = i
					break
				}
			}
			if found < 0 {
				t.Fatalf("missing section %s", heading)
			}
			s.offset = found
			m.clampRepositoryOffset()
			y := renderedRepositoryLine(t, m, heading)
			before, row := s.repo, s.row
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: y})
			m = next.(Model)
			if s.row != row || !reflect.DeepEqual(before, s.repo) {
				t.Fatalf("section %s acted as a control", heading)
			}
		}
		s.row, s.offset = 0, 0
		for row := 0; row < len(s.rows()); row++ {
			if s.row != row {
				t.Fatalf("keyboard row order changed: %d != %d", s.row, row)
			}
			m.revealRepositoryRow()
			rendered := stripANSI(m.View())
			if len(strings.Split(rendered, "\n")) > size[1] {
				t.Fatalf("height overflow:\n%s", rendered)
			}
			for _, line := range strings.Split(rendered, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("width overflow: %q", line)
				}
			}
			if !strings.Contains(strings.Split(rendered, "\n")[1], "https://github.com/") {
				t.Fatal("URL is not pinned")
			}
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			m = next.(Model)
		}
	}
}

func TestPostingLabelsDistinguishPermissionFromAutomaticAction(t *testing.T) {
	m := onboardingModel(t, 80, 24, 3)
	s := m.reviewPanel.setup
	s.row = repositoryPostingRow
	m.revealRepositoryRow()
	for _, tc := range []struct {
		action config.PublicationAction
		label  string
	}{{"", "Keep local"}, {config.PublishComment, "Post comment"}, {config.PublishApprove, "Approve PR"}, {config.PublishRequestChanges, "Request changes"}} {
		s.repo.AutoPublish = tc.action
		renderedRepositoryLine(t, m, "After review: "+tc.label)
		renderedRepositoryLine(t, m, "[x] Comments")
	}
	s.repo.AutoPublish = ""
	if !strings.Contains(stripANSI(m.View()), "Allowed actions, not automatic posts.") {
		t.Fatal("permission explanation missing")
	}
}

func TestMonitorCommandFollowsItsInstruction(t *testing.T) {
	m := onboardingModel(t, 80, 24, 3)
	content := m.repositoryContent()
	for i, line := range content {
		if line.text == "Run in another terminal:" {
			if i+1 >= len(content) || content[i+1].text != m.monitorCommandLines()[0] {
				t.Fatal("advice separates command from instruction")
			}
			return
		}
	}
	t.Fatal("monitor command instruction missing")
}
