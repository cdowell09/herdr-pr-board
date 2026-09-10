# Manual reviews

PR Board launches configured reviewer programs and records local findings.
The reviewer prepares its checkout and runs its agent.
PR Board validates the result before it records completion.
Neither a successful process exit nor JSON event output proves completion.
The reviewer contract prohibits GitHub publication.
This contract does not isolate arbitrary programs from GitHub credentials.

After completion, PR Board applies the repository's saved `auto_publish` choice.
This applies to manual reviews and explicit reruns, including `--review` commands.
An empty choice keeps findings local.
Each nonempty choice requires its separate publication permission.
Manual reviews do not require `auto_launch` or selected automatic views.
A publication failure does not change the completed local review.
The CLI preserves its completed run JSON and returns a nonzero status for publication failures.
Reading history or changing settings does not publish historical runs.
See [publication settings](repository-publication.md) for explicit publication and retries.

## Configure reviewers

Add these settings to the active configuration:

```toml
[review]
max_concurrency = 1
timeout = "30m"

[[reviewers]]
id = "pi"
command = ["/absolute/path/to/herdr-pr-board", "--pi-reviewer"]

[[repositories]]
name = "owner/repository"
reviewer = "pi"
```

Replace the program path and repository name.
Use an absolute executable path or an executable on `PATH`.
PR Board passes each command array item as one argument.
It does not interpret command arguments as shell code.
Add multiple repository entries to reuse one reviewer.
Reviewer IDs must start with a lowercase letter.
Remaining ID characters must be lowercase letters, digits, underscores, or hyphens.
Repository names must use `owner/repository`.
Each repository must reference a configured reviewer.

`review.max_concurrency` accepts values from one through eight.
The default is one active review across the installation.
Processes that share runtime state must use the same concurrency setting.
`review.timeout` accepts durations from one second through 24 hours.
The default is 30 minutes.
The timeout includes queue time and execution time.
PR Board checks current configuration before each launch attempt.

Built-in adapters support Pi, Codex, Claude Code, Oh My Pi, Kimi, Qoder CLI, and Qwen Code.
They also support Copilot, Mastra Code, Hermes, Cursor, Antigravity, and Grok.
See the [Pi adapter guide](pi-adapter.md) and [Codex and Claude Code guide](agent-adapters.md).
See [additional agent compatibility](agent-compatibility.md) for the other adapters and assessed limitations.
Each adapter requires its own installed native CLI or SDK.
See [Windows setup](windows.md) for native executables, npm launchers, and filesystem requirements.
Other reviewers do not require Pi.

## Select reviewers and instructions per repository

Each repository selects a named reviewer command.
Different repositories can select different commands or different arguments for the same program.

```toml
[[reviewers]]
id = "pi-security"
command = ["/absolute/path/to/herdr-pr-board", "--pi-reviewer"]
prompt_file = "reviews/security.md"
skill_file = ""

[[reviewers]]
id = "pi-product"
command = ["/absolute/path/to/herdr-pr-board", "--pi-reviewer"]
prompt_file = ""
skill_file = "reviews/product/SKILL.md"

[[repositories]]
name = "owner/security-service"
reviewer = "pi-security"

[[repositories]]
name = "owner/product-app"
reviewer = "pi-product"
```

Replace the example paths with installed programs and readable instruction files.
Relative instruction paths use the active configuration file's directory.
A custom prompt replaces embedded review criteria.
A selected skill adds compatible requirements.
The fixed execution and result contract still applies.
See [review instructions](review-instructions.md) for defaults, complete file examples, and precedence.

Use the same fields with any built-in reviewer flag.
Repository setup offers all missing built-in reviewers alongside existing reviewer commands.
Saving setup adds only the selected missing reviewer.
Saving also updates edited instruction fields on the selected existing profile.
Existing reviewer IDs and custom commands remain unchanged.
A bare agent CLI command does not implement the PR Board reviewer contract automatically.
A custom adapter must read the input and write the validated result described below.
Custom programs keep their own instruction interface.

## Detect installed agent CLIs

Repository setup looks for each built-in agent CLI on PATH when it opens.
This check reads PATH only. It sends no GitHub request.

Setup starts at the first available reviewer, in configuration order.
A custom reviewer command is always available, because it names its own program.
A built-in reviewer is available when its agent CLI is on PATH.
Configured reviewers come before the built-in reviewers that your configuration omits.
Setup keeps a saved reviewer, even when its agent CLI is absent.

The reviewer row shows `not installed` for an absent built-in agent CLI.
Each missing reviewer still cycles normally, so you can select one before you install it.
Setup shows this hint below the row when it finds no built-in agent CLI.
The hint appears only while a built-in reviewer is selected:

```text
No agent CLI found on PATH. Install one, then reopen settings.
```

Setup does not check two kinds of reviewer.
It does not check a custom reviewer command.
It also does not check the Hermes and Cursor reviewers.
Those adapters start a shared language runtime, which does not prove the agent is installed.
Setup shows no install status for an unchecked reviewer.
It also does not select an unchecked built-in reviewer as the default.

## Use the review region

The board shows the selected PR's local reviews in the review region under the URL.
A line under the URL summarizes the posted reviews.
The region header names the outcome, the reviewer, the revision comparison, and the severity counts.
The findings follow the header with their severity, title, path, and body.
One line summarizes the automation state and one line names the run and its diagnostics directory.
A `▼` marker counts the lines below the visible part of the region.
Press `j` or `k` to scroll the region.
Press `g` or `G` for the start or the end.
Press `PgUp` or `PgDn` for one page.
Selecting another PR reloads the region from local state without a GitHub request.

Press `v` to zoom the region to the full height.
Zoom shows the latest review before previous reviews.
Each review shows its findings and publication outcomes together.
The latest completed review is the manual publication target.
The region compares history with the latest successful board observation.
Older findings do not complete a newer observed revision.
A failed observation makes the current revision unknown.
Refresh the board to retrieve newer PR data.
Scroll to Details for full run IDs, revisions, publication URLs, and diagnostics paths.
Automation status appears after review results.
Eligible PRs show **Waiting for review slot** when the running monitor has no available review slot.
The region checks shared review slots each second.
A terminal with fewer than 24 rows collapses the region to one summary line. Zoom still shows everything.

The review keys work on the board and in zoom:

| Key | Action |
| --- | --- |
| `n` | Queue a review with the repository's configured reviewer. |
| `N` | Explicitly retry or repeat a review. |
| `t` | Stop the newest active review shown for this PR. |
| `s` | Edit repository settings. |
| `c` | Publish the latest completed run as a comment. |
| `a` | Publish the latest completed run as an approval. |
| `x` | Publish the latest completed run as a change request. |
| `j`, `k`, `g`, `G`, `PgUp`, `PgDn`, mouse wheel | Scroll the region. In zoom, `↑` and `↓` also scroll. |
| `o`, click the URL | Open the PR in a browser. |
| `v` | Zoom the region, or leave zoom. |
| `Esc` | Leave zoom. |
| `?` | Open or close the keyboard help. |
| `q`, `Ctrl+C` | Close the board and stop its review requests. |

The repository must have saved settings before `n` starts a review.
Without saved settings, `n` opens repository setup.
Choose a reviewer and save the settings.
The reviewer row shows the selection, its position, and the total.
`Reviewer: claude (3/13)` selects the third reviewer of 13.
Press Right or Space to select the next reviewer.
Press Left to select the previous reviewer.
See [detect installed agent CLIs](#detect-installed-agent-clis) for the default selection and the install status.
The setup header shows that manual reviews are ready when automatic launches are off.
It names Enter to save the settings and `n` to run the review.

Reviews continue when you leave zoom or select another PR.
Closing the board cancels its queued and active reviews.
The background monitor and its reviews continue.

Press `t` to stop the newest active review shown for this PR.
The footer names the target run while the run is active.
The region reports cleanup progress and the final outcome.
The stop request also works for a review owned by another board or monitor in the same state directory.
The owner cancels only that reviewer and cleans up its child processes.
The stopped run records a failure with a cancellation reason and does not publish findings.
Use `N` to retry the stopped revision explicitly.
If the run finishes before the stop request, the region reports that it is no longer running.
An older or unavailable owner cannot accept the request.

Press `s` to edit repository settings.
The settings form replaces the board until you save or cancel.
Use the arrow keys and Space to change settings.
On the reviewer row, Left selects the previous reviewer.
Right and Space select the next reviewer.

The settings form separates reviews, GitHub permissions, automatic posting, global views, and advanced files.
The rows keep that order.
Select global automatic view IDs explicitly.
These view selections apply to all repositories that allow automatic launches.
Permission alone does not enable automatic posting.
Select **After review: Keep local** to publish findings only with manual controls.
Select **Prompt file** and **Skill file** under **Advanced** to choose custom instructions.
See [instruction setup](review-instructions.md#select-files-in-the-board) for path editing controls.
See [repository setup and publication](repository-publication.md) for permissions, automatic posting, and failure recovery.

The form shows the monitor state and missing setup requirements when automatic launches or global views are on.
Use `PgUp` and `PgDn` to scroll through settings and the monitor command.
Press Enter to save, or Esc to discard changes.
Successful saves start a stopped monitor when automatic views and repository launches are enabled.
Run the displayed command in another terminal if background startup fails.
New selections can take effect on the next monitor scan.
See [automatic reviews](automatic-reviews.md) for scan and dispatch rules.

## Start a review

Herdr supplies `HERDR_PLUGIN_STATE_DIR` for plugin actions.
Set that variable to the installation's absolute state directory when you run commands directly.
All review processes must share that directory.

Run the repository's configured reviewer:

```sh
bin/herdr-pr-board --review https://github.com/owner/repository/pull/7
```

Select a different configured reviewer:

```sh
bin/herdr-pr-board --review https://github.com/owner/repository/pull/7 --reviewer another-agent
```

Add `--config path/to/config.toml` to select a configuration.
PR Board retrieves the current head commit and target branch before it claims the revision.
The PR must be open.
Manual reviews can include drafts and failed CI.
Automatic dispatch is a separate feature.

An active claim prevents duplicate launches.
A capacity limit queues the request.
Queued requests wait locally without repeated GitHub requests.
PR Board captures the revision again when capacity becomes available.
Failed, blocked, and abandoned attempts require an explicit retry.
Completed revisions also require an explicit rerun.

Request an explicit rerun:

```sh
bin/herdr-pr-board --review https://github.com/owner/repository/pull/7 --rerun
```

The command writes one versioned run record to standard output after it records an outcome.
Queue and execution diagnostics use standard error.
Failures before a claim do not produce a run record.
Completed runs exit with status zero.
Blocked runs, failed runs, and launch failures exit with status one.
Invalid option combinations exit with status two.
Cancellation terminates the owned reviewer processes.
On macOS and Linux, wrappers must stop separate child process groups when they receive termination.
On Windows, the runner terminates the owned Job Object and waits for its processes.

## Review notifications

A review run that finishes in the background is silent until you open the board.
PR Board shows a Herdr notification when a run completes, blocks, or fails.
The board sends it for manual reviews.
The monitor sends it for automatic reviews.
The `--review` command does not send notifications.

The title names the outcome, the repository, and the PR number.
A completed run lists the finding count for each severity, for example `P0:0 P1:2 P2:0 P3:1`.
A blocked or failed run shows the first clause of the run message.
A stopped run does not notify.
A notification never makes a GitHub request.

Set `review.notify` to select the outcomes:

- `"all"` notifies on completed, blocked, and failed runs. This is the default.
- `"problems"` notifies on blocked and failed runs only.
- `"off"` sends no notifications.

The board and the monitor read the setting when a run finishes.
A saved change applies to queued and running reviews without a restart.
Notifications need the `herdr` CLI on `PATH` or in `HERDR_BIN_PATH`.
When the CLI is missing or a notification fails to show, the board warns once per session.
The monitor writes one `monitor.log` line for each failed notification.
The review run records its outcome in both cases.

## Inspect local results

Read all revisions for one PR:

```sh
bin/herdr-pr-board --review-history https://github.com/owner/repository/pull/7
```

This command requires neither GitHub CLI nor a terminal.
It writes a versioned document with a `runs` array.
The command preserves older revision history.
See [review region controls](#use-the-review-region) for findings, cancellation, and publication.

Each attempt stores artifacts under `HERDR_PLUGIN_STATE_DIR/reviews/<run-id>/`:

- `input.json` contains the captured reviewer input.
- `result.json` contains the reviewer's returned result, when available.
- `stdout.log` and `stderr.log` contain bounded process diagnostics.

Each process log retains at most one MiB.
The board includes failure diagnostics and the artifact directory in the run details.
The result must contain at most four MiB.
See [review memory](review-memory.md) for history and ownership rules.

## Reviewer contract

The program reads one JSON input object from standard input.
Version one supplies these fields:

| Field | Meaning |
| --- | --- |
| `version` | The integer `1`. |
| `identity.repository` | The lowercase repository name. |
| `identity.number` | The positive PR number. |
| `identity.head_oid` | The captured head commit. |
| `identity.base_ref_name` | The target branch. |
| `base_oid` | The captured comparison commit. |
| `pr_url` | The PR URL. |
| `title` | The PR title as data. |
| `result_path` | The absolute location for the result file. |

The reviewer must preserve the captured identity and comparison commit.
It must write one result object to `result_path` before it exits successfully.
The result object must contain `version`, `identity`, `base_oid`, and `outcome`.
Unknown fields and trailing content are invalid.
The `outcome` object must contain `status`, `message`, and completed review findings.
Use `completed`, `blocked`, or `failed` for `status`.
Completed outcomes require a summary and an explicit `findings` array.
Use an empty array when the review finds no issues.
Blocked and failed outcomes require a diagnostic message.
Missing specification context must produce a blocked outcome.

Each finding requires `severity`, `title`, and `body`.
Severity must be `P0`, `P1`, `P2`, or `P3`.
Optional `path` identifies the affected file.
Optional `line` supplies a positive line number and requires a path.
The core validates these fields and the exact captured revision.
Malformed results become failed runs.

On macOS and Linux, the program inherits file descriptor three.
`HERDR_REVIEW_CLAIM_FD=3` identifies that descriptor.
On Windows, `HERDR_REVIEW_CLAIM_FD` contains the inherited native handle value.
Windows reviewers must use that value instead of assuming descriptor three.
The program must retain that descriptor until its review work stops.
It must forward ownership to child processes that can outlive it.
The built-in adapters forward ownership to their agent processes.
The operating system releases ownership after every inherited descriptor closes.
