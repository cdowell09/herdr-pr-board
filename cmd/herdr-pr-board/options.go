package main

import (
	"errors"
	"flag"
	"io"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

type options struct {
	pluginAction string
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
	adapter      adapterOptions
}

type adapterOptions struct {
	name, executable, skill string
	enabled                 bool
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var o options
	f := flag.NewFlagSet("herdr-pr-board", flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.pluginAction, "plugin-action", "", "native Herdr entrypoint: open or run")
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
	var adapters []*adapterOptions
	for _, builtin := range config.BuiltinReviewers("") {
		a := &adapterOptions{name: builtin.ID}
		f.BoolVar(&a.enabled, a.name+"-reviewer", false, "run the "+a.name+" adapter with JSON input on stdin")
		f.StringVar(&a.executable, a.name+"-executable", "", "agent executable (requires --"+a.name+"-reviewer)")
		f.StringVar(&a.skill, a.name+"-skill", "", "review skill path (requires --"+a.name+"-reviewer)")
		adapters = append(adapters, a)
	}
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
	for _, enabled := range []bool{specified["plugin-action"], o.eligibility, o.monitor, o.validate, o.json, specified["review"], specified["review-history"]} {
		if enabled {
			modes++
		}
	}
	invalid := false
	for _, a := range adapters {
		if a.enabled {
			modes++
			o.adapter = *a
		}
		invalid = invalid || (specified[a.name+"-executable"] || specified[a.name+"-skill"]) && !a.enabled
	}
	invalid = invalid || specified["plugin-action"] && o.pluginAction != "open" && o.pluginAction != "run"
	invalid = invalid || specified["plugin-action"] && specified["config"]
	invalid = invalid || modes > 1 || f.NArg() != 0
	invalid = invalid || specified["view"] && (!o.json || o.view == "")
	invalid = invalid || specified["review"] && o.review == "" || specified["review-history"] && o.history == ""
	invalid = invalid || (specified["reviewer"] || specified["rerun"]) && o.review == ""
	invalid = invalid || specified["reviewer"] && o.reviewer == ""
	invalid = invalid || o.adapter.enabled && specified["config"]
	if invalid {
		return o, errors.New("invalid option combination or unexpected positional arguments")
	}
	return o, nil
}
