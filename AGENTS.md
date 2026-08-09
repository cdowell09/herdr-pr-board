# AGENTS.md

## Mission

Maintain PR Board as a small, reliable Herdr plugin. Preserve its config-driven views, accurate API budgeting, responsive Bubble Tea UI, and direct GitHub installation.

Read [`README.md`](README.md) for user behavior and configuration. Read [`herdr-plugin.toml`](herdr-plugin.toml) before changing plugin metadata, entrypoints, placement, platforms, or build commands.

## Source map

- `cmd/herdr-pr-board/`: process startup and dependency wiring.
- `internal/config/`: TOML defaults, parsing, validation, scope modes, and Search request counts.
- `internal/github/`: `gh` execution, query tokenization, Search results, GraphQL CI enrichment, caching, and rate-limit decoding.
- `internal/board/`: refresh orchestration, API budgeting, Bubble Tea state, rendering, keyboard input, and mouse input.
- `bin/open`: focus an existing plugin pane or open one dedicated tab.
- `bin/run`: record pane ownership, name the tab, run the board, and clean owned state.
- `config.example.toml`: public configuration reference.
- `.github/workflows/ci.yml`: release gates enforced on pull requests and `main`.

## Workflow

1. Locate the owning package from the source map. Keep GitHub transport in `internal/github`, refresh policy in `internal/board`, and configuration rules in `internal/config`.
2. Add or update a regression test at the package boundary that owns the behavior.
3. Update `README.md` and `config.example.toml` when user-visible configuration, controls, requirements, or behavior changes.
4. Run the completion gates before reporting the change as complete.

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
- Keep PR URLs visible so Herdr link handlers can route them. Ctrl+click review requires `herdr-pickr`.

## Completion gates

Run from the repository root:

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
