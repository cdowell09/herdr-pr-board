# Contributing to PR Board

Use this guide to build, test, and change PR Board.
Read [AGENTS.md](AGENTS.md) for package ownership, invariants, and required review steps.

## Setup

Install these tools:

- Go 1.24 or later.
- Python 3.11 or later.
- `pre-commit`.
- GitHub CLI (`gh`) for manual verification against GitHub.
- Herdr 0.8.0 or later for macOS and Linux plugin testing.
- Herdr 0.9.0 or later for Windows plugin testing.

See [Windows setup](docs/windows.md) for Windows requirements.

Clone the repository:

```sh
git clone https://github.com/cdowell09/herdr-pr-board.git
cd herdr-pr-board
```

Install the repository hooks:

```sh
pre-commit install
```

Run all hooks manually:

```sh
pre-commit run --all-files
```

## Test a local source build

Use this procedure to test the current checkout instead of the GitHub version.

Build and link the plugin:

```sh
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
herdr plugin link "$PWD" --enabled
```

On Windows, use PowerShell:

```powershell
go build -o bin/herdr-pr-board.exe ./cmd/herdr-pr-board
herdr plugin link "$PWD" --enabled
```

`herdr plugin link` registers this checkout as `cdowell09.pr-board`.
It does not copy the source files.
The linked plugin runs the built executable from this checkout.
Herdr preserves the existing configuration and runtime state.

Verify the local link:

```sh
herdr plugin list --plugin cdowell09.pr-board --json
```

The link is ready when all these conditions are true:

- `source.kind` is `local`
- `plugin_root` points to this checkout
- `enabled` is `true`

If the board is open, press `q` first.
The open action focuses an existing board.
The action does not rebuild a running board.

Open the source build:

```sh
herdr plugin action invoke open --plugin cdowell09.pr-board
```

Rebuild the binary after each source change.

Return to the GitHub version:

```sh
herdr plugin unlink cdowell09.pr-board
herdr plugin install cdowell09/herdr-pr-board
```

`herdr plugin unlink` removes the local registration. It preserves the configuration and runtime state.

## Change the owning package

Use the [source map](AGENTS.md#source-map) to locate the owning package.
Keep GitHub transport in `internal/github`.
Keep refresh policy in `internal/discovery`.
Keep configuration rules in `internal/config`.

`internal/plugin` owns native entrypoint behavior on every platform.
`bin/open` and `bin/run` forward arguments to the native binary on macOS and Linux.

Add a regression test at the package boundary that owns the behavior:

| Behavior | Test location |
| --- | --- |
| Configuration defaults and validation | `internal/config/` |
| GitHub commands, queries, caching, and rates | `internal/github/` |
| Shared discovery and API budgeting | `internal/discovery/` |
| Board state, rendering, and input | `internal/board/` |
| Plugin pane reuse and state ownership | `internal/plugin/` |
| Release validation | `scripts/validate_release_test.py` |

Use fake runners for GitHub commands.
Tests must not call GitHub or launch a real browser.

## Update documentation

Update documentation when behavior, configuration, controls, or requirements change:

- `README.md` explains the product and common tasks.
- `config.example.toml` documents the complete configuration.
- `docs/` contains task guides and reference contracts.
- Keyboard and mouse documentation must match the rendered UI.

Write technical documentation in ASD-STE100 Simplified Technical English (STE):

- Write one idea per sentence.
- Keep sentences under 20 words.
- Use the active voice and present tense.
- Write instructions in the imperative mood.
- Use the same word for the same thing.
- Use `must` for requirements.
- Use `do not` for prohibitions.

## Review and completion gates

Run `thermo-nuclear-code-quality-review` on the full branch diff.
Address all P0, P1, and P2 findings.
Review the fixes before reporting completion.

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

CI reads build platforms from `herdr-plugin.toml`.
CI cross-compiles each platform on Linux.
CI also runs native Windows tests and a Herdr plugin smoke test.

## Release

Follow [the release procedure](docs/releasing.md) to change the version, create a tag, and upgrade an installation.
The release workflow generates notes with git-cliff and `cliff.toml`.

## Security

Report security problems through GitHub private vulnerability reporting.
Do not open a public issue.
See [SECURITY.md](SECURITY.md).
