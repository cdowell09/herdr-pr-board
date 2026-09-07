package main

import (
	"errors"
	"flag"
	"io"
)

type options struct {
	monitor      bool
	eligibility  bool
	publication  *publicationOptions
	configPath   string
	validate     bool
	json         bool
	view         string
	review       string
	history      string
	reviewer     string
	rerun        bool
	pi           bool
	piExecutable string
	piSkill      string
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var o options
	f := flag.NewFlagSet("herdr-pr-board", flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.configPath, "config", "", "path to config.toml")
	f.BoolVar(&o.eligibility, "review-eligibility", false, "print fresh automatic review eligibility as JSON")
	f.BoolVar(&o.monitor, "monitor", false, "monitor PRs until interrupted (requires HERDR_PLUGIN_STATE_DIR)")
	f.BoolVar(&o.validate, "validate", false, "validate the configuration and exit")
	f.BoolVar(&o.json, "json", false, "print a fresh PR snapshot as JSON")
	f.StringVar(&o.view, "view", "", "configured view ID (requires --json)")
	f.StringVar(&o.review, "review", "", "review a GitHub PR URL")
	f.StringVar(&o.history, "review-history", "", "print local history for a GitHub PR URL")
	f.StringVar(&o.reviewer, "reviewer", "", "configured reviewer ID (requires --review)")
	f.BoolVar(&o.rerun, "rerun", false, "explicitly retry or repeat a review")
	f.BoolVar(&o.pi, "pi-reviewer", false, "run the Pi reference adapter with JSON input on stdin")
	f.StringVar(&o.piExecutable, "pi-executable", "", "Pi executable (requires --pi-reviewer)")
	f.StringVar(&o.piSkill, "pi-skill", "", "code-review skill path (requires --pi-reviewer)")
	o.publication = addPublicationFlags(f)
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if err := o.publication.validate(f); err != nil {
		return o, err
	}
	specified := map[string]bool{}
	f.Visit(func(flag *flag.Flag) { specified[flag.Name] = true })
	modes := o.publication.modes()
	for _, enabled := range []bool{o.eligibility, o.monitor, o.validate, o.json, specified["review"], specified["review-history"], o.pi} {
		if enabled {
			modes++
		}
	}
	invalid := modes > 1 || f.NArg() != 0
	invalid = invalid || specified["view"] && (!o.json || o.view == "")
	invalid = invalid || specified["review"] && o.review == "" || specified["review-history"] && o.history == ""
	invalid = invalid || (specified["reviewer"] || specified["rerun"]) && o.review == ""
	invalid = invalid || specified["reviewer"] && o.reviewer == ""
	invalid = invalid || (specified["pi-executable"] || specified["pi-skill"]) && !o.pi
	invalid = invalid || o.pi && specified["config"]
	if invalid {
		return o, errors.New("invalid option combination or unexpected positional arguments")
	}
	return o, nil
}
