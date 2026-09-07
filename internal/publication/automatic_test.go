package publication

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

func TestAutomaticSelectorRevocationPreventsPostWithoutRevokingManualPermission(t *testing.T) {
	service, github, run := publicationFixture(t, `"comment"`)
	data, err := os.ReadFile(service.configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("auto_publish = \"comment\"\n")...)
	if err := os.WriteFile(service.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	github.onCapture = func() {
		if err := os.WriteFile(service.configPath, []byte(strings.Replace(string(data), `auto_publish = "comment"`, `auto_publish = ""`, 1)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.PublishAutomatic(context.Background(), testPR, run.ID, config.PublishComment); err == nil || github.posts != 0 {
		t.Fatalf("revoked automatic publication: error=%v posts=%d", err, github.posts)
	}
	github.onCapture = nil
	if _, err := service.Publish(context.Background(), testPR, run.ID, config.PublishComment); err != nil || github.posts != 1 {
		t.Fatalf("manual permission changed: error=%v posts=%d", err, github.posts)
	}
}
