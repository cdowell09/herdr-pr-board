# PR Board Context

The PR Board context presents pull requests to users and agents through configurable views.

## Language

### PR discovery

**View**:
A named list of PRs from one GitHub search.
_Avoid_: Tab

**Scope**:
A GitHub user, organization, or repository that limits a configured search.
_Avoid_: Workspace

**PR snapshot**:
A record of observed PR data, with its observation time and any known retrieval failures.
_Avoid_: Live state

### Review work

**Reviewer**:
An external program that performs a PR review on the user's behalf.
_Avoid_: Monitor

**Review run**:
One attempt to review a PR at a captured head commit and target branch.
_Avoid_: Published review

**Review completion**:
A successful review run with recorded findings, whether or not those findings are published.
_Avoid_: Publication

**Review eligibility**:
Whether a PR needs review under the user's configured rules and recorded review history.
_Avoid_: Review permission

**Launch permission**:
The user's persistent permission to start automatic reviews for a repository.
_Avoid_: Repository visibility

**Publication permission**:
The user's separate persistent permission to publish review findings to GitHub for a repository.
_Avoid_: Launch permission

**Publication action**:
A GitHub review action: comment, approve, or request changes.
_Avoid_: Review outcome

### Configuration

**Active configuration**:
The user-owned configuration that controls the current board session.
_Avoid_: Example configuration, default configuration

**Configuration edit**:
A change to the active configuration that changes the board's views or behavior.
_Avoid_: Configuration navigation

### Board interaction

**Board session**:
The running interactive PR Board experience that shows views and pull requests.
_Avoid_: Board tab, plugin window
