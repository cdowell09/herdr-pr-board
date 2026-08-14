package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTag(t *testing.T) {
	tests := []struct {
		name            string
		tag             string
		manifestVersion string
		wantErr         string
	}{
		{
			name:            "matching versions",
			tag:             "v1.2.3",
			manifestVersion: "1.2.3",
		},
		{
			name:            "tag does not have v prefix",
			tag:             "1.2.3",
			manifestVersion: "1.2.3",
			wantErr:         "must match vX.Y.Z",
		},
		{
			name:            "tag has too few components",
			tag:             "v1.2",
			manifestVersion: "1.2",
			wantErr:         "must match vX.Y.Z",
		},
		{
			name:            "tag has a prerelease suffix",
			tag:             "v1.2.3-rc.1",
			manifestVersion: "1.2.3",
			wantErr:         "must match vX.Y.Z",
		},
		{
			name:            "tag has a leading zero",
			tag:             "v01.2.3",
			manifestVersion: "01.2.3",
			wantErr:         "must match vX.Y.Z",
		},
		{
			name:            "manifest version has invalid form",
			tag:             "v1.2.3",
			manifestVersion: "1.2",
			wantErr:         "manifest version",
		},
		{
			name:            "versions do not match",
			tag:             "v1.2.4",
			manifestVersion: "1.2.3",
			wantErr:         "does not match manifest version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTag(tt.tag, tt.manifestVersion)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateTag() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateTag() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateTag() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestManifestVersion(t *testing.T) {
	tests := []struct {
		name       string
		manifest   string
		want       string
		wantErrSub string
	}{
		{
			name:     "reads version",
			manifest: "id = \"example.plugin\"\nversion = \"1.2.3\"\n",
			want:     "1.2.3",
		},
		{
			name:       "requires version",
			manifest:   "id = \"example.plugin\"\n",
			wantErrSub: "does not define version",
		},
		{
			name:       "requires valid version",
			manifest:   "version = \"1.2\"\n",
			wantErrSub: "must match X.Y.Z",
		},
		{
			name:       "reports malformed TOML",
			manifest:   "version = \"1.2.3\"\n[",
			wantErrSub: "parse plugin manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "herdr-plugin.toml")
			if err := os.WriteFile(path, []byte(tt.manifest), 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := ManifestVersion(path)
			if tt.wantErrSub == "" {
				if err != nil {
					t.Fatalf("ManifestVersion() error = %v", err)
				}
				if got != tt.want {
					t.Fatalf("ManifestVersion() = %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ManifestVersion() error = nil, want %q", tt.wantErrSub)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("ManifestVersion() error = %q, want substring %q", err, tt.wantErrSub)
			}
		})
	}
}
