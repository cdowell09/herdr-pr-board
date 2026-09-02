# Contributing to PR Board

This guide explains how the plugin is organized. It also explains what a change must include before review.

## Setup

Clone the repository:

```sh
git clone https://github.com/cdowell09/herdr-pr-board.git
cd herdr-pr-board
```

Build the plugin:

```sh
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
```

Install the pre-commit framework. Then install the repository hooks:

```sh
pre-commit install
```

Run all hooks manually:

```sh
pre-commit run --all-files
```

You need these tools:

- Go 1.24 or later
- Python 3.11 or later
- `pre-commit`
- GitHub CLI (`gh`) for manual verification against GitHub
- Herdr 0.8.0 or later for manual plugin testing

Install the plugin as a link for local work:

```sh
herdr plugin link "$PWD"
herdr plugin action invoke open --plugin cdowell09.pr-board
```

## Package ownership

Keep the behavior in its owning package:

- `cmd/herdr-pr-board/` — process startup and dependency wiring.
- `cmd/ci-platform-matrix/` — convert manifest platforms into CI cross-compilation targets.
- `scripts/validate_release.py` — release tag and plugin manifest validation.
- `internal/config/` — TOML defaults, parsing, validation, scope modes, and Search request counts.
- `internal/github/` — `gh` execution, query tokenization, Search results, GraphQL CI enrichment, caching, and rate-limit decoding.
- `internal/board/` — refresh orchestration, API budgeting, Bubble Tea state, rendering, keyboard input, and mouse input.
- `bin/open` — focus an existing plugin pane or open one dedicated tab.
- `bin/run` — record pane ownership, name the tab, run the board, and clean owned state.
- `internal/plugin/` — end-to-end tests for the `bin/open` and `bin/run` entrypoints.
- `config.example.toml` — public configuration reference.
- `.github/workflows/ci.yml` — release gates enforced on pull requests and `main`.
- `.github/workflows/release.yml` — run release validation and publish source releases.

Keep GitHub transport in `internal/github`. Keep refresh policy in `internal/board`. Keep configuration rules in `internal/config`. Keep `bin/open` and `bin/run` in plain `bash`.

## Regression tests

Add a regression test at the package boundary that owns the behavior:

- Configuration rules → `internal/config/config_test.go`.
- `gh` execution, tokenization, CI enrichment, and rate limits → `internal/github/client_test.go` (and `query_test.go` for tokenization).
- Refresh orchestration and API budgeting → `internal/board/service_test.go`.
- Bubble Tea state, rendering, and input → `internal/board/model_test.go`.
- Plugin entrypoint pane reuse and state ownership → `internal/plugin/entrypoint_test.go`.
- Release validation → `scripts/validate_release_test.py`.

Tests use fake `gh` runners. Tests never call GitHub. Tests never launch a real browser.

## Documentation expectations

Update these files when behavior, configuration, controls, or requirements change:

- `README.md` — user behavior and configuration.
- `config.example.toml` — public configuration reference.
- `docs/` — troubleshooting and plugin lifecycle guidance.
- Keyboard and mouse documentation must match the rendered UI.

## Documentation style

Write all technical documentation in ASD-STE100 Simplified Technical English (STE):

- Write one idea per sentence. Keep sentences under 20 words.
- Use the active voice.
- Write instructions in the imperative mood.
- Use the same word for the same thing. Do not use synonyms.
- Use the present tense.
- Use `must` for requirements. Use `do not` for prohibitions.

## Release

Follow [`docs/releasing.md`](docs/releasing.md) to change the version, create a tag, and upgrade an installation.

## Completion gates

Run every gate from the repository root before you open a pull request:

```sh
gofmt -w cmd internal
go test ./...
python3 -B -m unittest discover -s scripts -p '*_test.py'
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...
go test -race ./...
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
bash -n bin/open bin/run
git diff --check
```

A change is complete when these are true:

- All applicable gates pass.
- Regression coverage exercises the changed behavior.
- Documentation matches the UI and configuration.
- `git status --short` contains only intended files.

Do not commit the built `bin/herdr-pr-board` file. The `.gitignore` ignores it.

CI reads each build platform from `herdr-plugin.toml`. CI cross-compiles each platform on Linux.

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

Report security problems through GitHub private vulnerability reporting. Do not open a public issue. See [`SECURITY.md`](SECURITY.md).
