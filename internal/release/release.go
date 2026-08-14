package release

import (
	"fmt"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

var (
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	tagPattern     = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type pluginManifest struct {
	Version string `toml:"version"`
}

// ManifestVersion reads and validates the plugin version from a manifest.
func ManifestVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read plugin manifest %q: %w", path, err)
	}

	var manifest pluginManifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parse plugin manifest %q: %w", path, err)
	}
	if manifest.Version == "" {
		return "", fmt.Errorf("plugin manifest %q does not define version", path)
	}
	if !versionPattern.MatchString(manifest.Version) {
		return "", fmt.Errorf("plugin manifest version %q must match X.Y.Z", manifest.Version)
	}
	return manifest.Version, nil
}

// ValidateTag checks that a release tag matches the plugin manifest version.
func ValidateTag(tag, manifestVersion string) error {
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("release tag %q must match vX.Y.Z", tag)
	}
	if !versionPattern.MatchString(manifestVersion) {
		return fmt.Errorf("manifest version %q must match X.Y.Z", manifestVersion)
	}

	expectedTag := "v" + manifestVersion
	if tag != expectedTag {
		return fmt.Errorf("release tag %q does not match manifest version %q", tag, manifestVersion)
	}
	return nil
}
