package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type pluginManifest struct {
	Platforms []string `toml:"platforms"`
}

type buildTarget struct {
	Platform string `json:"platform"`
	GOOS     string `json:"goos"`
}

type buildMatrix struct {
	Include []buildTarget `json:"include"`
}

var goOSByPlatform = map[string]string{
	"linux": "linux",
	"macos": "darwin",
}

func main() {
	os.Exit(runFile(os.Args[1:], os.Stdout, os.Stderr))
}

func runFile(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: ci-platform-matrix path/to/herdr-plugin.toml")
		return 2
	}
	manifest, err := os.Open(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "ci-platform-matrix:", err)
		return 1
	}
	defer manifest.Close()
	return run(manifest, stdout, stderr)
}

func run(manifest io.Reader, stdout, stderr io.Writer) int {
	var metadata pluginManifest
	if err := toml.NewDecoder(manifest).Decode(&metadata); err != nil {
		fmt.Fprintln(stderr, "ci-platform-matrix: decode manifest:", err)
		return 1
	}
	if len(metadata.Platforms) == 0 {
		fmt.Fprintln(stderr, "ci-platform-matrix: manifest has no platforms")
		return 1
	}

	matrix := buildMatrix{Include: make([]buildTarget, 0, len(metadata.Platforms))}
	for _, platform := range metadata.Platforms {
		goos, ok := goOSByPlatform[platform]
		if !ok {
			fmt.Fprintf(stderr, "ci-platform-matrix: unsupported manifest platform %q\n", platform)
			return 1
		}
		matrix.Include = append(matrix.Include, buildTarget{Platform: platform, GOOS: goos})
	}

	if err := json.NewEncoder(stdout).Encode(matrix); err != nil {
		fmt.Fprintln(stderr, "ci-platform-matrix: encode matrix:", err)
		return 1
	}
	return 0
}
