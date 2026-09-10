# Core user journeys

Read this document before you propose or scope a feature.
Each feature must serve one of these journeys.
See [own the review appliance](adr/0002-own-the-review-appliance.md) for the ownership boundary.

## Actors

**Human operator**:
The person who installs PR Board, configures views and reviewers, reads findings, and decides publication.

**Reviewer agent**:
The external program that PR Board launches to review one PR revision.
It receives reviewer context from PR Board and returns structured findings.

**Coding agent**:
The agent the human uses to write code.
PR Board does not launch or control this agent.
It is not an actor in these journeys.

## Primary journey: automatic reviews

1. The human installs Herdr, GitHub CLI, and the plugin, then opens the board.
2. The human configures scopes and views in `config.toml`.
3. The human selects a reviewer profile for a repository, with optional prompt and skill files.
4. The human enables automatic launches for the repository and selects automatic views.
5. The monitor scans the selected views and finds eligible PRs.
6. The monitor dispatches a reviewer for each eligible PR at its captured revision.
7. PR Board validates the reviewer result and records findings locally.
8. The human reads the REVIEW column and opens the review panel to read findings.
9. The human publishes findings, or a saved after-review action publishes them automatically.
10. A new head commit makes the PR eligible again, and the journey repeats from step 5.

## Secondary journey: manual review

1. The human selects a PR and opens the review panel.
2. The human starts a review, or repeats a review with an explicit rerun.
3. Steps 7 through 9 of the primary journey apply.

## Where value comes from

The human operator gains value when PR Board:

- Shows which PRs need attention without opening each one.
- Reports when a review finishes, blocks, or fails.
- Publishes findings under a policy the human trusts.

The reviewer agent gains value when PR Board supplies better evidence:

- The PR specification and linked issues.
- Existing review threads on the PR.
- CI failures on the revision.
- Findings from earlier revisions of the same PR.

## Not this plugin's job

- Fixing findings on a PR. The coding agent reads findings from GitHub or local history and makes the fix.
- Tracking change requests, approvals, or CI on PRs the human authored. GitHub and the coding agent own this.
- Serving as a general GitHub API for other agents. The JSON snapshot supports scripts and is a secondary surface.
