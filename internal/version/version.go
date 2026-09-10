// Package version reports the PR Board version and the build revision.
package version

import "runtime/debug"

// Current is the PR Board version. It must equal the top-level version in
// herdr-plugin.toml. The manifest build command runs a plain go build without
// link-time variables, so the binary carries the version in this constant.
// TestCurrentMatchesThePluginManifest keeps the two values equal. Pull-request
// CI and the release workflow both run that test.
const Current = "0.7.0"

// revisionLength is the number of leading characters kept from a VCS revision.
const revisionLength = 7

// String returns the version and the short build revision, for example
// "0.6.0 (e633d4f)". It returns only the version when the build carries no
// revision.
func String() string {
	return describe(debug.ReadBuildInfo())
}

// describe formats the version from build information.
func describe(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return Current
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return Current
	}
	if len(revision) > revisionLength {
		revision = revision[:revisionLength]
	}
	if modified {
		revision += "-dirty"
	}
	return Current + " (" + revision + ")"
}
