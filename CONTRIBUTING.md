# Contributing to PR Board

Thank you for contributing to `herdr-pr-board`. This guide explains how the plugin is organized and what a change must include before it is ready for review.

## Setup

Clone the repository and build the plugin:

```sh
git clone https://github.com/cdowell09/herdr-pr-board.git
cd herdr-pr-board
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
```

Requirements:

- Go 1.24 or later
- GitHub CLI (`gh`) for any manual verification against GitHub
- Herdr 0.8.0 or later for manual plugin testing

Install the plugin as a link so you can iterate locally:

```sh
herdr plugin link "$PWD"
herdr plugin action invoke open --plugin cdowell09.pr-board
```

## Package ownership

Keep the behavior in its owning package:

- `cmd/herdr-pr-board/` — process startup and dependency wiring.
- `internal/config/` — TOML defaults, parsing, validation, scope modes, and Search request counts.
- `internal/github/` — `gh` execution, query tokenization, Search results, GraphQL CI enrichment, caching, and rate-limit decoding.
- `internal/board/` — refresh orchestration, API budgeting, Bubble Tea state, rendering, keyboard input, and mouse input.
- `bin/open` — focus an existing plugin pane or open one dedicated tab.
- `bin/run` — record pane ownership, name the tab, run the board, and clean owned state.
- `config.example.toml` — public configuration reference.
- `.github/workflows/ci.yml` — release gates enforced on pull requests and `main`.

Keep GitHub transport in `internal/github`, refresh policy in `internal/board`, and configuration rules in `internal/config`. `bin/open` and `bin/run` stay plain `bash`.

## Regression tests

Add or update a regression test at the package boundary that owns the behavior:

- Configuration rules → `internal/config/config_test.go`.
- `gh` execution, tokenization, CI enrichment, and rate limits → `internal/github/client_test.go` (and `query_test.go` for tokenization).
- Refresh orchestration and API budgeting → `internal/board/service_test.go`.
- Bubble Tea state, rendering, and input → `internal/board/model_test.go`.

Tests exercise the real packages with fake `gh` runners; they never call out to GitHub or launch a real browser.

## Documentation expectations

Update these when user-visible behavior, configuration, controls, or requirements change:

- `README.md` — user behavior and configuration.
- `config.example.toml` — public configuration reference.
- `docs/` — troubleshooting and plugin lifecycle guidance.
- Keyboard and mouse documentation must stay synchronized with the rendered UI.

## Completion gates

Run every gate from the repository root before opening a pull request:

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go test -race ./...
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
bash -n bin/open bin/run
git diff --check
```

A change is complete when all applicable gates pass, regression coverage exercises the changed behavior, documentation matches the UI and configuration, and `git status --short` contains only intended files. The built `bin/herdr-pr-board` file is ignored and must not be committed.

## Invariants

### Persistent data

- Store user configuration only in `HERDR_PLUGIN_CONFIG_DIR`.
- Store runtime state only in `HERDR_PLUGIN_STATE_DIR`.
- Treat `HERDR_PLUGIN_ROOT` as installed program files. Do not write user data there.
- Preserve an existing configuration during install, upgrade, and pane reuse.

### GitHub requests

- Build `gh search prs` arguments with options first, then `--`, then tokenized query terms. This keeps negative qualifiers such as `-is:draft` from becoming CLI flags.
- Budget Search requests across views, configured scopes, and pagination. GitHub Search pages contain at most 100 results.
- Keep rate-limit state current after full and active-view refreshes.
- Bind GraphQL capacity checks to the actual uncached CI batches inside `EnrichCI`. Recheck capacity between batches and propagate updated rates after failures.
- Keep CI cache access synchronized. Run the race test after concurrency or cache changes.

### Configuration

- Use `ScopeMode`, `ScopeGlobal`, and `ScopeConfigured` instead of new scope string literals in production code.
- Keep defaults single-sourced in `internal/config/config.go`; generate `DefaultFile` from those values.
- Validate new fields and test both default creation and invalid input.

### Herdr and UI

- Keep the board in one reusable tab named `PR Board`.
- Keep pane-state cleanup ownership-safe: one pane must not remove another pane's state.
- Keep rendered tab labels and mouse hitboxes derived from the same label function.
- When layout lines change, verify mouse row and URL coordinates against actual `View()` output.
- Keep keyboard and mouse documentation synchronized with behavior.
- Keep the selected PR URL visible and keep browser-open behavior generic.

## Security

Report security problems privately through GitHub's private vulnerability reporting instead of opening a public issue. See [`SECURITY.md`](SECURITY.md).
