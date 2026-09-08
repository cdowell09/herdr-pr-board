package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type postingBackend struct {
	publicationFake
	calls int
	err   error
}

func (p *postingBackend) PublishConfigured(_ context.Context, _, id string) (publication.Attempt, error) {
	p.calls++
	p.runID = id
	return publication.Attempt{}, p.err
}

func TestBoardManualReviewAndRerunPostWithoutPublicationKey(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "publication failure"}[fail], func(t *testing.T) {
			m := panelModel(t)
			backend := &postingBackend{}
			if fail {
				backend.err = errors.New("posting denied")
			}
			m = m.WithPublications(t.TempDir(), backend)
			if m.cfg.Repositories[0].AutoLaunch {
				t.Fatal("fixture must not allow automatic launches")
			}
			for _, key := range []string{"n", "N"} {
				next, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
				m = next.(Model)
				if command == nil {
					t.Fatalf("%s did not start review", key)
				}
				message := command().(reviewDoneMsg)
				if message.run.Status != reviewmemory.Completed || backend.runID != message.run.ID {
					t.Fatalf("wrong run posted: %+v", message)
				}
				if fail && (message.err == nil || !strings.Contains(message.err.Error(), "review completed; publication failed")) {
					t.Fatalf("lost posting error: %v", message.err)
				}
				request := m.reviews.(*reviewFake).request
				if request.Automatic || request.Rerun != (key == "N") {
					t.Fatalf("manual launch semantics changed: %+v", request)
				}
				next, _ = m.Update(message)
				m = next.(Model)
			}
			if backend.calls != 2 {
				t.Fatalf("manual completions required c: %d", backend.calls)
			}
		})
	}
}
