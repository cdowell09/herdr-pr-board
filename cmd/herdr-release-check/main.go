package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cdowell09/herdr-pr-board/internal/release"
)

func main() {
	tag := flag.String("tag", os.Getenv("GITHUB_REF_NAME"), "release tag to validate")
	manifest := flag.String("manifest", "herdr-plugin.toml", "path to the plugin manifest")
	flag.Parse()

	if *tag == "" {
		fail("release tag is required; pass --tag or set GITHUB_REF_NAME")
	}
	version, err := release.ManifestVersion(*manifest)
	if err != nil {
		fail(err.Error())
	}
	if err := release.ValidateTag(*tag, version); err != nil {
		fail(err.Error())
	}

	fmt.Printf("release tag %s matches plugin version %s\n", *tag, version)
}

func fail(message string) {
	fmt.Fprintf(os.Stderr, "release validation failed: %s\n", message)
	os.Exit(1)
}
