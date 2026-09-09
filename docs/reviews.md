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

Built-in adapters support Pi, Codex, Claude Code, Oh My Pi, Kimi, Qoder CLI, Qwen Code, Copilot, and Mastra Code.
See the [Pi adapter guide](pi-adapter.md) and [Codex and Claude Code guide](agent-adapters.md).
See [additional agent compatibility](agent-compatibility.md) for the other adapters and assessed limitations.
Each adapter requires its own installed agent CLI.
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

## Use the review panel

Press `v` to open the selected PR's review history.
The panel opens repository setup when the repository has no saved settings.
Choose a reviewer and save the settings.
Press `n` in the review panel to start the review.

The panel shows the latest review before previous reviews.
Each review shows its findings and publication outcomes together.
The latest completed review is the manual publication target.
The panel compares history with the latest successful board observation.
Older findings do not complete a newer observed revision.
A failed observation makes the current revision unknown.
Refresh the board to retrieve newer PR data.
Scroll to Details for full run IDs, revisions, publication URLs, and diagnostics paths.
Automation status appears after review results.
Eligible PRs show **Waiting for review slot** when the running monitor has no available review slot.
The panel checks shared review slots each second.

| Key | Action in the review panel |
| --- | --- |
| `n` | Queue a review with the repository's configured reviewer. |
| `N` | Explicitly retry or repeat a review. |
| `t` | Stop the newest active review shown for this PR. |
| `s` | Edit repository settings. |
| `c` | Publish the latest completed run as a comment. |
| `a` | Publish the latest completed run as an approval. |
| `x` | Publish the latest completed run as a change request. |
| `j`, `k`, `↑`, `↓`, mouse wheel | Scroll through findings and diagnostics. |
| `g`, `Home`, `G`, `End` | Move to the first or last history line. |
| `o`, click the URL | Open the PR in a browser. |
| `Esc`, `v` | Return to the board. |
| `q`, `Ctrl+C` | Close the board and stop its review requests. |

Reviews continue when you close the panel.
Closing the board cancels its queued and active reviews.
The background monitor and its reviews continue.

Press `t` to stop the newest active review shown for this PR.
The panel names the target run and reports cleanup progress and the final outcome.
The stop request also works for a review owned by another board or monitor in the same state directory.
The owner cancels only that reviewer and cleans up its child processes.
The stopped run records a failure with a cancellation reason and does not publish findings.
Use `N` to retry the stopped revision explicitly.
If the run finishes before the stop request, the panel reports that it is no longer running.
An older or unavailable owner cannot accept the request.

Press `s` in the panel to edit repository settings.
Use the arrow keys and Space to change settings.
Select **Prompt file** and **Skill file** to choose custom instructions.
See [instruction setup](review-instructions.md#select-files-in-the-board) for path editing controls.

The settings panel separates reviews, GitHub permissions, automatic posting, and global views.
Select global automatic view IDs explicitly.
These view selections apply to all repositories that allow automatic launches.
Permission alone does not enable automatic posting.
Select **After review: Keep local** to publish findings only with manual controls.
See [repository setup and publication](repository-publication.md) for permissions, automatic posting, and failure recovery.

The panel shows the monitor state and missing setup requirements.
Use PgUp and PgDn to scroll through settings and the monitor command.
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

## Inspect local results

Read all revisions for one PR:

```sh
bin/herdr-pr-board --review-history https://github.com/owner/repository/pull/7
```

This command requires neither GitHub CLI nor a terminal.
It writes a versioned document with a `runs` array.
The command preserves older revision history.
See [review panel controls](#use-the-review-panel) for findings, cancellation, and publication.

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
