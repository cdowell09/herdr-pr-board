# Agent review roadmap

Status: All four delivery milestones are implemented.

See the [JSON contract](json-snapshots.md), [review execution](reviews.md), and [review memory](review-memory.md).
See [repository publication](repository-publication.md) and [automatic reviews](automatic-reviews.md) for unattended workflows.

## Accepted decisions

### Architecture

Each business rule must have one owner.
The board and agent commands must use the same discovery module.
UI changes must not change review eligibility.
Reviewer changes must not change PR discovery.
See [review execution ownership](adr/0001-delegate-review-execution.md).

### First milestone

An agent must retrieve a configured view as JSON without opening a board session.
The PR snapshot must include commit identity, CI status, freshness, and retrieval errors.
Review memory and review launch use the contracts below.

### PR snapshot contract

Each command invocation must attempt a fresh scan.
The command must not silently substitute an earlier PR snapshot.
A partial result must contain available data and explicit errors.
A partial result must produce a nonzero exit code.
Unavailable fields must differ from empty results.
The result must expose configured result limits and whether completeness is known.
The shared discovery module must own freshness definitions for all callers.

### Review identity

Review completion applies to the captured head commit and target branch.
A changed head commit or target branch makes the PR eligible for another review.
Comments and title edits do not trigger another code review.
Failed review runs must not count as review completions.
Users must be able to request another review explicitly.
Review completion and publication must remain separate records.

### Reviewer contract

The external reviewer command must prepare its checkout and execute its agent.
PR Board must supply structured input that identifies the PR and revision.
The reviewer must return structured findings and a review outcome.
Agent-specific setup belongs to the reviewer.
Users must be able to configure commands for other coding agents.
The core must use one versioned reviewer input and result contract.
Pi is the first reference adapter, not a required dependency of the core.
Users may supply wrappers that translate another agent's output into the result contract.
The reviewer contract must prohibit publication by the reviewer.
This contract does not isolate arbitrary reviewer programs from GitHub credentials.

### Coordination

Review coordination must cover processes and Herdr workspaces that share one local installation.
Coordination between separate computers is out of scope.

### Permissions

Users must enable automatic review launches for each repository.
Review findings remain local by default.
Publishing GitHub reviews requires separate persistent publication permission.
Repository settings store publication permissions.
Repository visibility grants neither launch permission nor publication permission.
Users must configure allowed publication actions for each repository.
Publication starts with comment-only permission when the user enables it.
Approval and change requests each require explicit permission.
PR Board must publish through GitHub CLI and own the publication permission check.
Repository publication onboarding must require minimal user effort.
The first review action must offer a short repository setup panel.
The panel must configure the reviewer, automatic launches, and allowed publication actions.
Defaults must retain manual launches and local findings.
Save explicit selections and allow users to edit them from the board.
Provide equivalent noninteractive configuration for agents.
Recheck current permissions before launching or publishing.

### Automatic review eligibility

Users must select existing configured view IDs for automatic reviews.
Do not maintain a separate set of discovery queries for automatic reviews.
A PR must have repository launch permission, a non-draft state, and a known current revision.
The revision must have neither a completed review nor an active review run.
CI failures must not prevent code review.
The board must explain why each PR is eligible or waiting.

### Monitoring

Provide a headless monitor command with one monitor per local installation.
The monitor lifetime must remain independent of the board session.
The monitor must reuse the discovery module and configured refresh interval.
Keep operating-system startup integration separate from monitoring policy.

### Review failures and concurrency

Default to one active review across the local installation.
Users must be able to configure review concurrency and timeouts.
Mark failed review runs visibly.
Failed review runs require explicit retry initially.
Continue processing other eligible PRs after a failed review run.
Prevent duplicate review launches across local processes.
Recover abandoned work after a crash.

### Testing

Test the snapshot command through its existing command entrypoint.
Verify arguments, JSON output, error output, exit status, and operation without a terminal.
Use a fake GitHub CLI executable for deterministic command tests.
Retain discovery tests for budgeting and concurrency.
Retain UI tests for rendering and controls.
Test each shared policy through its owning module.

## Accepted delivery order

Each milestone builds on the shared discovery module.

1. Shared discovery and JSON snapshots.
2. Review memory, manual launch, and visible outcomes.
3. Repository onboarding and permission-controlled publication.
4. Headless monitoring and automatic dispatch.

The implementation issues use the agreed specification template and dependency links.
GitHub records their native blocked-by relationships.
Implement the first milestone before starting later milestones.

## Deferred extension

Authored-PR feedback tracking follows the first four milestones.
Candidate signals include new change requests, failed checks, and revisions that need another review.
Do not include automatic code fixes in the current roadmap.

## Command and integration contracts

### Snapshot command

Use `herdr-pr-board --json` for all configured views.
Add `--view <id>` to scan one existing configured view.
Preserve `--config` and existing interactive invocation.
Emit one JSON document with a schema version, observation times, PR data, limits, rates, and structured errors.
Use standard error for diagnostics.
Exit with zero for a successful scan, one for retrieval failures, and two for invalid command usage.
Represent unavailable revision and CI data explicitly.
Keep the wire representation separate from internal UI messages.

### First reviewer adapter

The local Pickr settings select the `pi-review` backend.
That backend runs Pi with the user's code-review skill in an interactive pane.
It provides a PR URL without a pinned revision or structured completion result.

Build a separate Pi adapter that uses the same skill in noninteractive mode.
The adapter must prepare an isolated checkout at the captured revision.
Supply the comparison revision and available PR specification context.
Missing required context must produce a visible blocked result.
Validate findings and the reviewed revision before recording completion.
Process exit alone must not prove review completion.
Preserve the existing Pickr configuration.

### Shared monitoring

The monitor must own scheduled scans while it runs.
The board must reuse the monitor's stored PR snapshot instead of scheduling duplicate scans.
Explicit fresh scans must coordinate locally with monitor scans.
The board may resume its existing refresh behavior when no monitor runs.
Do not dispatch reviews from retained rows after a failed observation.

### Implementation issues

1. [Expose shared PR discovery as versioned JSON snapshots](https://github.com/cdowell09/herdr-pr-board/issues/48)
2. [Track review completion and coordinate local review runs](https://github.com/cdowell09/herdr-pr-board/issues/49)
3. [Launch configurable reviewers with a Pi reference adapter](https://github.com/cdowell09/herdr-pr-board/issues/50)
4. [Add repository onboarding and configurable review publication](https://github.com/cdowell09/herdr-pr-board/issues/51)
5. [Monitor PRs headlessly and share snapshots with the board](https://github.com/cdowell09/herdr-pr-board/issues/52)
6. [Dispatch eligible PR reviews automatically](https://github.com/cdowell09/herdr-pr-board/issues/53)

Implement issue one first.
