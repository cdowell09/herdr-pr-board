package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunWritesBuildMatrixFromManifestPlatforms(t *testing.T) {
	manifest := strings.NewReader(`platforms = ["macos", "linux"]`)
	var stdout, stderr bytes.Buffer

	if exitCode := run(manifest, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	want := "{\"include\":[{\"platform\":\"macos\",\"goos\":\"darwin\"},{\"platform\":\"linux\",\"goos\":\"linux\"}]}\n"
	if stdout.String() != want {
		t.Fatalf("matrix = %q, want %q", stdout.String(), want)
	}
}

func TestRunRejectsUnsupportedManifestPlatform(t *testing.T) {
	manifest := strings.NewReader(`platforms = ["freebsd"]`)
	var stdout, stderr bytes.Buffer

	if exitCode := run(manifest, &stdout, &stderr); exitCode == 0 {
		t.Fatal("run succeeded for an unsupported platform")
	}
	if !strings.Contains(stderr.String(), `unsupported manifest platform "freebsd"`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
