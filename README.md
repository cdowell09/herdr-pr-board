# herdr-pr-board

`herdr-pr-board` is a plugin for [Herdr](https://herdr.dev). It shows GitHub pull requests from multiple repositories in one Herdr tab.

## Terms

This document uses these technical terms:

- **PR**: A GitHub pull request.
- **View**: A named list of PRs from one GitHub search.
- **Scope**: A GitHub user, organization, or repository.
- **CI**: Continuous integration checks for a PR.

## Functions

The plugin supplies these default views:

- **Opened by me** shows each open PR that your GitHub account created.
- **Review requested** shows each open PR that requests your review.
- **All open** shows each open PR in the configured scopes.

You can add, remove, rename, or move views in `config.toml`.

The board shows the URL of the selected PR. Ctrl+click the URL to send the PR to [herdr-pickr](https://github.com/tomasvarga/herdr-pickr).

The CI column uses a symbol and a color:

| Symbol | Color | Meaning |
| --- | --- | --- |
| `✓` | Green | All checks passed. |
| `●` | Yellow | One or more checks are pending. |
| `✗` | Red | One or more checks failed. |
| `–` | Dim | The PR has no checks. |
| `?` | Dim | The plugin cannot get the check status. |

## Requirements

Install these tools:

- Herdr 0.8.0 or later
- GitHub CLI (`gh`)
- Go 1.24 or later

Authenticate GitHub CLI before you use the plugin:

```sh
gh auth login
```

The plugin does not store a GitHub token. GitHub CLI supplies the authentication.

## Install from GitHub

Run this command:

```sh
herdr plugin install cdowell09/herdr-pr-board
```

## Install for local development

Build and link the plugin:

```sh
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
herdr plugin link "$PWD"
```

## Open the board

Run this command:

```sh
herdr plugin action invoke open --plugin cdowell09.pr-board
```

The action opens a dedicated Herdr tab. If the tab is open, the action moves focus to that tab.

You can add this key binding to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+shift+p"
type = "plugin_action"
command = "cdowell09.pr-board.open"
description = "open PR board"
```

Reload the Herdr configuration:

```sh
herdr server reload-config
```

## Configure the board

The first run creates this file:

```text
$(herdr plugin config-dir cdowell09.pr-board)/config.toml
```

See [`config.example.toml`](config.example.toml) for a complete example.

The default configuration has this structure:

```toml
[ui]
title = "Pull Requests"

[github]
refresh_interval = "5m"
limit_per_scope = 100
max_concurrency = 4
ci_batch_size = 25
scopes = [
  "user:@me",
  # "org:your-company",
  # "repo:owner/repository",
]

[[views]]
id = "authored"
title = "Opened by me"
query = "is:open author:@me"
scope = "global"

[[views]]
id = "review"
title = "Review requested"
query = "is:open review-requested:@me"
scope = "global"

[[views]]
id = "all"
title = "All open"
query = "is:open"
scope = "configured"
```

Each view uses a GitHub PR search query.

`scope = "global"` runs the query one time. `scope = "configured"` runs the query one time for each configured scope.

The plugin combines the scoped results. The plugin removes duplicate PR URLs.

### Add a custom view

Add another `[[views]]` section:

```toml
[[views]]
id = "security"
title = "Security queue"
query = 'is:open label:"security" -is:draft'
scope = "configured"
```

Use a unique lowercase `id`. Use a GitHub PR search for `query`.

### Stop automatic refresh

Set the refresh interval to zero:

```toml
[github]
refresh_interval = "0"
```

## Use the board

| Key | Action |
| --- | --- |
| `1`–`9` | Select a view. |
| `Tab` | Select the next view. |
| `Shift+Tab` | Select the previous view. |
| `j`, `k`, arrow keys | Select a PR. |
| `/` | Filter the active view. |
| `Esc` | Clear the filter. |
| `r` | Refresh the active view. |
| `R` | Refresh all views. |
| `Enter`, `o` | Open the selected PR in a browser. |
| Ctrl+click URL | Send the PR to the applicable Herdr link handler. |
| `q` | Close the board. |

## API limits

The plugin refreshes the board when it starts. The default automatic refresh interval is five minutes.

GitHub permits 30 authenticated Search API requests each minute. GitHub returns a maximum of 1,000 results for each search.

The plugin limits concurrent searches. It also removes duplicate PRs before it requests CI data.

The plugin gets CI data with batched GraphQL queries. GitHub gives an authenticated user 5,000 GraphQL points each hour.

The board shows the remaining Search API and GraphQL capacity. The board keeps old data when a refresh fails.

## Develop the plugin

Run these checks:

```sh
go test ./...
go vet ./...
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
bash -n bin/open bin/run
```

Herdr runs each plugin command from the plugin directory. Store user configuration in `HERDR_PLUGIN_CONFIG_DIR`.

Store runtime state in `HERDR_PLUGIN_STATE_DIR`. Do not store user data in `HERDR_PLUGIN_ROOT`.
