# Troubleshooting and plugin lifecycle

This guide helps you diagnose common PR Board failures and explains how install, configuration, upgrades, and removal affect plugin files.

## Get context

Start with versions and current status:

```sh
herdr --version
herdr plugin list --plugin cdowell09.pr-board
gh --version
gh auth status
```

## Troubleshooting

### GitHub CLI is not authenticated

The plugin does not store a GitHub token. It relies on GitHub CLI (`gh`). If searches fail with an authentication error, sign in:

```sh
gh auth login
gh auth status
```

### An invalid search query fails

Each view runs a GitHub PR search query. GitHub rejects malformed queries and reports the reason. The board shows the failing view and the `gh` message.

Verify a query outside the board:

```sh
gh search prs --limit 10 --json number,url -- "is:open your-query"
```

Common causes:

- An unclosed `'` or `"` in the query.
- A qualifier GitHub does not support for `gh search prs`.
- An empty query in a view.

### Search capacity is exhausted

GitHub permits 30 authenticated Search API requests each minute. When the board cannot refresh, it reports how many requests remain and when the limit resets. Wait for the reset, or reduce the number of searches the refresh needs (fewer views or configured scopes, or a lower `limit_per_scope`).

Check current capacity with GitHub CLI:

```sh
gh api rate_limit --jq '.resources.search'
```

### GraphQL capacity is exhausted

The plugin enriches PRs with aggregate CI status using GraphQL. GitHub gives an authenticated user 5,000 GraphQL points each hour. When capacity is short, CI columns stay as they were and the board reports the shortfall.

Check current capacity:

```sh
gh api rate_limit --jq '.resources.graphql'
```

### A PR does not open in the browser

When the operating system cannot open the selected PR, the board shows a warning. The selected PR URL stays visible above the footer, so copy it into a browser manually or press `Enter` again. macOS uses `open` and Linux uses `xdg-open`; if neither is available or functional on your system, the board reports the failure instead of silently doing nothing.

### Where are the plugin logs?

Herdr records each plugin command it runs. Inspect PR Board commands:

```sh
herdr plugin log list --plugin cdowell09.pr-board
```

Add `--limit` to control how many records are shown:

```sh
herdr plugin log list --plugin cdowell09.pr-board --limit 10
```

For Herdr's own diagnostics, enable debug logging and inspect `~/.config/herdr/` logs:

```sh
HERDR_LOG=herdr=debug herdr
```

## Plugin lifecycle

### First install

```sh
herdr plugin install cdowell09/herdr-pr-board
```

Herdr clones the plugin from GitHub, runs its build commands, and registers it. `plugin install` creates the plugin's config and state directories. The first time the board runs, it creates `config.toml` in the config directory:

```text
$(herdr plugin config-dir cdowell09.pr-board)/config.toml
```

### Upgrade by reinstalling

There is no separate update command in Herdr 0.8. To update the plugin, reinstall from GitHub:

```sh
herdr plugin install cdowell09/herdr-pr-board
```

Reinstalling replaces the managed plugin checkout (the installed program files). Your `config.toml` in the config directory and runtime state in the state directory are preserved.

### Uninstall

```sh
herdr plugin uninstall cdowell09.pr-board
```

Uninstalling unregisters the plugin and removes the managed plugin checkout. The config and state directories are left in place, so a later install starts from the configuration you already had.

### Local-development linking

While you iterate on the plugin source, link the checkout instead of installing:

```sh
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
herdr plugin link "$PWD"
herdr plugin action invoke open --plugin cdowell09.pr-board
```

`plugin link` uses your working directory as the plugin root and does not copy it. To stop using the link and leave the files alone:

```sh
herdr plugin unlink cdowell09.pr-board
```

`plugin install` refuses to install over a locally linked plugin. Unlink or uninstall the local plugin first.

## When configuration changes take effect

The board reads `config.toml` each time it starts. After editing configuration, close the board with `q` and open it again with the `open` action; the new configuration applies on the next launch. You do not need to restart the Herdr server for plugin configuration changes.

## What preserves and what removes files

| Operation | Managed plugin checkout | Config directory | Runtime state directory |
| --- | --- | --- | --- |
| `herdr plugin install` | creates | creates | creates |
| `herdr plugin install` again (upgrade) | replaced | preserved | preserved |
| `herdr plugin uninstall` | removed | preserved | preserved |
| `herdr plugin link` | not used | created | created |
| `herdr plugin unlink` | not used | preserved | preserved |
| Editing `config.toml` | unaffected | changed | unaffected |
