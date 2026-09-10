# Repository setup and publication

Review launch and publication use separate permissions.
Each repository starts with manual launches and local findings.
Repository visibility grants no publication permission.

## Set up a repository

Select a PR and press `v`.
The first review action opens repository settings when the repository has no saved settings.
The panel groups settings under Reviews, GitHub permissions, Automatic posting, Global views, and Advanced.
The rows keep that order.
Select a reviewer under Reviews with the arrow keys or Space.
The reviewer row shows the selection, its position, and the total.
`Reviewer: claude (3/13)` selects the third reviewer of 13.
Press Right or Space to select the next reviewer.
Press Left to select the previous reviewer.
Setup looks for each built-in agent CLI on PATH when it opens.
The reviewer row starts at the first built-in reviewer that PATH holds.
Setup keeps a saved reviewer, even when its agent CLI is absent.
The reviewer row shows `not installed` for an absent agent CLI.
Setup shows an install hint when PATH holds no built-in agent CLI.
Setup does not check a custom reviewer command.
Enable automatic launches only when you want unattended reviews.
Enable Comments under GitHub permissions to allow comments.
Permission allows publication but does not schedule publication.
Approval and change requests each require a separate selection.
Select After review: Post comment under Automatic posting to post each newly completed review.
Select After review: Keep local to keep findings local.
The saved posting choice applies to manual reviews, explicit reruns, CLI reviews, and monitor reviews.
Automatic launch permission remains separate.
Reading history or saving settings does not publish existing runs.
Select existing global automatic views for unattended reviews.
These selections apply to all repositories that allow automatic launches.
The panel selects no views by default.
Select Prompt file and Skill file under Advanced to choose instructions or keep the defaults.
Press Space or click a path row to edit it.
Press Enter to accept the path before saving settings.
Press Esc to discard only the active path edit.
See [instruction setup](review-instructions.md#select-files-in-the-board) for file rules and editing controls.
Press Enter to save.
Press Esc to discard changes.
Use PgUp, PgDn, or the mouse wheel to scroll through long settings.
The header shows that manual reviews are ready when automatic launches are off.
It shows the monitor state and any missing setup requirement when automatic launches or global views are on.
Successful saves start a stopped monitor when saved automatic views and repository launches are enabled.
The board reuses an existing monitor in the same state directory.
Run the displayed monitor command in another terminal if background startup fails.
New selections can take effect on the next monitor scan.

The panel offers missing Pi, Codex, and Claude Code reviewers alongside existing profiles.
Saving creates only the selected missing reviewer definition.
Install and authenticate the selected agent CLI before running a review.
The defaults require no custom instruction files.
Saving validates selected files and reports missing requirements.

Press `s` in the review panel to edit saved settings.
Saving does not change unrelated settings or comments.
Settings use the active configuration file, including an explicit `--config` path.
Runtime locks use `HERDR_PLUGIN_STATE_DIR`.
The panel and setup command edit `[[repositories]]` tables.
They do not edit inline repository arrays.
Convert inline repository arrays to tables before using setup.
A rejected save preserves the existing configuration file.
The panel saves repository settings and global view selections in one operation.
Concurrent changes to the same repository reject a stale save.
Concurrent changes to an edited reviewer profile also reject a stale save.
Instruction edits affect every repository that selects that profile.
Changed global selections, view definitions, or GitHub settings also reject a stale panel save.
The global view editor requires an explicit `[review]` table.
Convert inline or dotted review settings before changing global selections through the panel.
A missing `[review]` table is added when you select views.
Reload repository settings before saving again.

## Configure without a terminal

Create repository settings with the built-in Pi reviewer:

```sh
bin/herdr-pr-board --repository-settings owner/repository --use-pi-reviewer
```

Use `--use-codex-reviewer` or `--use-claude-reviewer` to add another built-in adapter.
Choose only one `--use-AGENT-reviewer` flag.
Use `--set-reviewer ID` when that reviewer ID already exists.
Setup does not replace existing reviewer commands.
See [agent requirements](agent-adapters.md) before starting a review.


Use an existing reviewer and allow comment publication:

```sh
bin/herdr-pr-board --repository-settings owner/repository \
  --set-reviewer pi --auto-launch=false --publish-actions comment
```

Permit approval explicitly:

```sh
bin/herdr-pr-board --repository-settings owner/repository \
  --publish-actions comment,approve
```

Revoke all publication permissions:

```sh
bin/herdr-pr-board --repository-settings owner/repository --publish-actions ''
```

Omitted options preserve existing selections.
New repositories default to manual launches and local findings.
Use `--set-reviewer pi` when the Pi definition already exists.
Set `HERDR_PLUGIN_STATE_DIR` to an absolute installation state directory before saving settings.

`--repository-settings` prints the saved settings as one JSON document on standard output:

```json
{"name":"owner/repository","reviewer":"pi","auto_launch":false,"publish_actions":["comment"],"auto_publish":""}
```

Keys use snake_case, matching every other JSON surface.
An empty `publish_actions` list prints as `[]`.
An empty `auto_publish` string means no automatic publication action.

You can also edit the active TOML file:

```toml
[[reviewers]]
id = "pi"
command = ["/absolute/path/to/herdr-pr-board", "--pi-reviewer"]

[[repositories]]
name = "owner/repository"
reviewer = "pi"
auto_launch = false
publish_actions = ["comment"]
```

Valid publication actions are `comment`, `approve`, and `request_changes`.
An empty action list keeps findings local.
The action list grants permissions.
It does not start publication automatically.
Validate the configuration with `--validate`.

## Publish findings

Open the review panel with `v`.
The panel identifies the latest completed run as the publication target.
Press `c` to publish a comment.
Press `a` to approve.
Press `x` to request changes.
The configured permissions must allow the selected action.
The panel shows publication results separately from local review outcomes.

Publish an explicit completed run without a terminal:

```sh
bin/herdr-pr-board --publish https://github.com/owner/repository/pull/7 \
  --run RUN_ID --action comment
```

Find run identifiers with `--review-history`.
Inspect publication attempts with this command:

```sh
bin/herdr-pr-board --publication-history https://github.com/owner/repository/pull/7
```

PR Board reloads permissions immediately before publication.
It checks the current head commit and target branch.
A changed revision blocks publication of older findings.
The reviewer adapter must not publish reviews.
PR Board publishes through GitHub CLI.
Publication has a 90-second timeout, including lock waits.

## Resolve publication failures

Local findings remain available after publication fails.
Publication records use `HERDR_PLUGIN_STATE_DIR/publications`.
Each request includes a unique marker in its review body.

A definite HTTP rejection records a failed attempt.
Correct the reported cause and repeat the publication command.
The history preserves the failed attempt.

A lost response records an uncertain attempt.
Repeating publication checks GitHub for the existing marker, commit, and authenticated account.
An empty response does not authorize another publication request.
An uncertain attempt stays blocked until reconciliation finds its review.
Use the original authenticated account for reconciliation.
A process restart does not clear uncertainty.
A published attempt returns its existing GitHub review identifier.
