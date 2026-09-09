# PR Board

Review pull requests across repositories without leaving [Herdr](https://herdr.dev).
PR Board brings GitHub searches, CI status, and agent reviews into one reusable terminal tab.

- Find your open PRs, review requests, and team queues in configurable views.
- Check CI, local review progress, and submitted GitHub reviews together.
- Run agent reviews with your own prompts and skills.
- Keep findings local, or enable publication and automatic reviews per repository.
- Retrieve JSON snapshots for scripts and coding agents.

![PR Board with repository views, CI status, and review controls](docs/images/herdr-pr-board.png)

## Install and open

Install Herdr, Git, GitHub CLI (`gh`), and Go 1.24 or later.
Herdr plugins build from source on your machine.
Go must already be installed before the build step runs.

| Platform | Requirements |
| --- | --- |
| macOS and Linux | Herdr 0.8.0 or later. |
| Windows x64 | Herdr 0.9.0 or later; Windows 10 version 1809 or later, or Windows 11. |

Windows configuration and state directories must use local NTFS.
See [Windows setup](docs/windows.md) for native commands and reviewer requirements.

Authenticate GitHub CLI, install the plugin, and open the board:

```sh
gh auth login
herdr plugin install cdowell09/herdr-pr-board
herdr plugin action invoke open --plugin cdowell09.pr-board
```

The open action creates a `PR Board` tab or focuses the existing tab.
PR Board uses GitHub CLI authentication and preserves configuration during reinstallations.

## Use the board

The default views show your open PRs, your review requests, and open PRs in configured scopes.
The board refreshes every five minutes by default.

| Control | Action |
| --- | --- |
| `1`–`9`, `Tab`, or click a view | Select a view. |
| `↑`, `↓`, or click a PR | Select a PR. |
| `Enter`, `o`, or click the URL | Open the selected PR in a browser. |
| `/` | Filter the current view locally. |
| `r` / `R` | Refresh the current view / all views. |
| `v` | Open reviews and repository settings. |
| `E` | Edit configuration and reload valid changes. |
| `q` | Close the board. |

The CI column shows check status.
REV shows local review progress for the current revision.
POSTED shows reviews submitted through GitHub, this PR Board installation, or both.
Select a PR to inspect its revision details.
See [board controls and status](docs/board.md) for all controls, symbols, and layouts.

## Configure your queues

The first run creates `config.toml` in the plugin configuration directory.
Press `E` to edit it, or locate the directory:

```sh
herdr plugin config-dir cdowell09.pr-board
```

A **view** is a named GitHub search.
A **scope** is a GitHub user, organization, or repository.
Set `github.scopes` to your scopes, for example `["org:your-company", "repo:owner/repository"]`.

Add a view for a team queue:

```toml
[[views]]
id = "security"
title = "Security queue"
query = 'is:open label:"security" -is:draft'
scope = "configured"
```

`configured` runs the search across your configured scopes.
`global` runs the search once without those scopes.
Each view must have a unique ID.

See [configuration](docs/configuration.md) for defaults, validation, refresh settings, API budgeting, and Herdr sidebar counts.
Use [config.example.toml](config.example.toml) as the complete configuration reference.

## Review with an agent

Built-in adapters support Pi, Codex, Claude Code, Qwen Code, Oh My Pi, Kimi, Qoder CLI, Copilot, and Mastra Code.
Install and authenticate the selected agent CLI before you start a review.
See [reviewer setup](docs/reviews.md#configure-reviewers) for supported versions and custom reviewer commands.
See [agent compatibility](docs/agent-compatibility.md) for additional CLI requirements and assessed limitations.

Select a PR and press `v`.
The panel opens repository setup when the repository has no saved settings.
Choose a reviewer and save the settings.
Press `n` in the review panel to run the review.
Press `s` in the review panel to change settings later.

Default reviews check repository standards and the PR specification.
The specification comes from the PR body and linked closing issues.
Missing required evidence blocks the review.
Defaults require no custom instruction files.

Select **Prompt file** and **Skill file** during setup to customize the review:

- A custom prompt replaces the default review criteria.
- An optional skill adds compatible requirements.
- The prompt takes precedence when the selected instructions conflict.
- Named reviewer profiles share instructions across their selected repositories.

Press Space or click a path row to edit it.
Press Enter to accept the path, then press Enter again to save settings.
You can edit the same `prompt_file` and `skill_file` selections directly in TOML.
See [review instructions](docs/review-instructions.md) for complete examples, file rules, and precedence.

Reviews run in temporary checkouts at captured revisions and return validated local findings.
The review prompt prohibits source changes, setup scripts, and direct GitHub publication.
Defaults keep launches manual and findings local.

Enable GitHub permissions separately in repository settings.
Select an **After review** action to publish newly completed reviews automatically.
Permission alone does not enable automatic posting.
See [publication settings](docs/repository-publication.md) for manual actions and failure recovery.

For unattended reviews, select global automatic views and enable automatic launches for each repository.
The board starts a background monitor when those settings are enabled.
Closing the board leaves the monitor and its reviews running.
See [automatic reviews](docs/automatic-reviews.md) for eligibility, concurrency, and retry rules.

## Scripts and monitoring

Retrieve a fresh JSON snapshot from a source checkout:

```sh
bin/herdr-pr-board --json --config path/to/config.toml
```

On Windows, use `bin/herdr-pr-board.exe`.
Snapshots include PRs, CI, submitted reviews, observation times, rates, and structured retrieval errors.
Partial results return a nonzero status.
See [JSON snapshots](docs/json-snapshots.md) for the versioned contract.

Use the [headless monitor](docs/monitor.md) to share discovery observations without keeping a board open.

## Reference and development

- [Core user journeys](docs/journeys.md) and [architecture decisions](docs/adr/)
- [Troubleshooting and plugin lifecycle](docs/troubleshooting.md)
- [Local source builds and contribution checks](CONTRIBUTING.md)
- [Local review history and ownership](docs/review-memory.md)
- [Release history](CHANGELOG.md) and [release procedure](docs/releasing.md)
- [Security reporting](SECURITY.md)
