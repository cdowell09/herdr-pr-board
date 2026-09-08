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

See the [Pi adapter guide](pi-adapter.md) for Pi requirements and its validated event contract.
Other reviewers do not require Pi.
Existing Pickr settings remain unchanged.

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
Cancellation terminates the owned reviewer process group.
Wrappers must stop separate child process groups when they receive termination.

## Inspect local results

Read all revisions for one PR:

```sh
bin/herdr-pr-board --review-history https://github.com/owner/repository/pull/7
```

This command requires neither GitHub CLI nor a terminal.
It writes a versioned document with a `runs` array.
The command preserves older revision history.
Open the board's review panel with `v` to inspect findings and diagnostics.
Use `n` to queue a review.
Use `N` to request an explicit rerun.
Press `Esc` to return to the board while reviews continue.
Closing the board cancels its queued and active reviews.

The panel compares history with the latest successful board observation.
Older findings do not complete a newer observed revision.
A failed observation makes the current revision unknown.
Refresh the board to retrieve newer PR data.

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

The program inherits its claim descriptor as file descriptor three.
`HERDR_REVIEW_CLAIM_FD=3` identifies that descriptor.
The program must retain that descriptor until its review work stops.
It must forward ownership to child processes that can outlive it.
The Pi reference adapter forwards ownership to Pi.
The operating system releases ownership after every inherited descriptor closes.
