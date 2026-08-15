# Troubleshooting and plugin lifecycle

This guide helps you find the cause of common PR Board failures. It also explains how install, configuration, upgrades, and removal affect plugin files.

## Get context

Check versions and current status:

```sh
herdr --version
herdr plugin list --plugin cdowell09.pr-board
gh --version
gh auth status
```

## Troubleshooting

### GitHub CLI is not authenticated

The plugin does not store a GitHub token. It uses GitHub CLI (`gh`). If searches fail with an authentication error, sign in:

```sh
gh auth login
gh auth status
```

### An invalid search query fails

Each view runs a GitHub PR search query. GitHub rejects malformed queries. The board shows the failing view and the `gh` message.

Verify a query outside the board:

```sh
gh search prs --limit 10 --json number,url -- "is:open your-query"
```

Common causes:

- An unclosed `'` or `"` in the query.
- A qualifier that GitHub does not support for `gh search prs`.
- An empty query in a view.

### Search capacity is exhausted

GitHub allows 30 authenticated Search API requests each minute. When the board cannot refresh, it reports how many requests remain and when the limit resets. Wait for the reset. Or reduce the number of searches the refresh needs. Reduce the number of views, configured scopes, or `limit_per_scope`.

Check current capacity with GitHub CLI:

```sh
gh api rate_limit --jq '.resources.search'
```

### GraphQL capacity is exhausted

The plugin enriches PRs with aggregate CI status. It uses GraphQL. GitHub gives an authenticated user 5,000 GraphQL points each hour. When capacity is short, CI columns stay as they were. The board reports the shortfall.

Check current capacity:

```sh
gh api rate_limit --jq '.resources.graphql'
```

### A PR does not open in the browser

When the operating system cannot open the selected PR, the board shows a warning. The selected PR URL stays visible above the footer.

Copy the URL into a browser manually. Or press `Enter` again.

macOS uses `open`. Linux uses `xdg-open`. If neither is available or functional, the board reports the failure. It does not fail silently.

### Where are the plugin logs?

Herdr records each plugin command it runs. Inspect PR Board commands:

```sh
herdr plugin log list --plugin cdowell09.pr-board
```

Add `--limit` to control the number of records:

```sh
herdr plugin log list --plugin cdowell09.pr-board --limit 10
```

For Herdr diagnostics, enable debug logging. Then inspect `~/.config/herdr/`:

```sh
HERDR_LOG=herdr=debug herdr
```

## Plugin lifecycle

### First install

```sh
herdr plugin install cdowell09/herdr-pr-board
```

Herdr clones the plugin from GitHub. It runs the build commands. It registers the plugin. `plugin install` creates the plugin config and state directories.

The first time the board runs, it creates `config.toml` in the config directory:

```text
$(herdr plugin config-dir cdowell09.pr-board)/config.toml
```

### Upgrade by reinstalling

There is no update command in Herdr 0.8. To update the plugin, reinstall it from GitHub:

```sh
herdr plugin install cdowell09/herdr-pr-board
```

Reinstalling replaces the managed plugin checkout. This is the installed program files. Your `config.toml` in the config directory is preserved. Runtime state in the state directory is preserved.

### Uninstall

```sh
herdr plugin uninstall cdowell09.pr-board
```

Uninstalling unregisters the plugin. It removes the managed plugin checkout. The config and state directories stay in place. A later install starts from the configuration you already had.

### Local-development linking

While you edit the plugin source, link the checkout instead of installing:

```sh
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
herdr plugin link "$PWD"
herdr plugin action invoke open --plugin cdowell09.pr-board
```

`plugin link` uses your working directory as the plugin root. It does not copy it.

To stop using the link and leave the files alone:

```sh
herdr plugin unlink cdowell09.pr-board
```

`plugin install` refuses to install over a locally linked plugin. Unlink or uninstall the local plugin first.

## Edit configuration while the board runs

Press `E` to open the active `config.toml` file in an editor.
The board uses `$VISUAL`, then `$EDITOR`, then `vi`.
The board validates the file after the editor exits.
It reloads valid changes and refreshes all views.

The board keeps the previous configuration when the editor or validation fails.
It shows the error in the footer.
If the editor does not start, set `$VISUAL` or `$EDITOR`.

The board applies configuration changes without a Herdr server restart.

## What preserves and what removes files

| Operation | Managed plugin checkout | Config directory | Runtime state directory |
| --- | --- | --- | --- |
| `herdr plugin install` | creates | creates | creates |
| `herdr plugin install` again (upgrade) | replaced | preserved | preserved |
| `herdr plugin uninstall` | removed | preserved | preserved |
| `herdr plugin link` | not used | created | created |
| `herdr plugin unlink` | not used | preserved | preserved |
| Editing `config.toml` | unaffected | changed | unaffected |
