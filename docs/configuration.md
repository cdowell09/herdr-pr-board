# Configure PR Board

A view is a named GitHub PR search.
A scope is a GitHub user, organization, or repository.

The first run creates this file:

```text
$(herdr plugin config-dir cdowell09.pr-board)/config.toml
```

Store user configuration only in `HERDR_PLUGIN_CONFIG_DIR`.
Store runtime state only in `HERDR_PLUGIN_STATE_DIR`.
`HERDR_PLUGIN_ROOT` contains installed program files.
Do not store user data there.

See [`config.example.toml`](../config.example.toml) for a complete example.

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

[sidebar]
enabled = true
ttl = "15m"
review_view = "review"

[review]
auto_views = []
max_concurrency = 1
timeout = "30m"
```

Each view uses a GitHub PR search query.

`scope = "global"` runs the query one time. `scope = "configured"` runs the query one time for each configured scope.

The plugin runs the scope searches at the same time. `github.max_concurrency` limits the number of Search API requests that run at the same time.

The plugin combines the scoped results. The plugin removes duplicate PR URLs.

## Settings reference

| Setting | Default | Valid values | Effect |
| --- | --- | --- | --- |
| `ui.title` | `"Pull Requests"` | A string that is not empty. | The header of the board. |
| `github.refresh_interval` | `"5m"` | `"0"` or a Go duration of `1m` or more, for example `"5m"` or `"1h"` | The time between automatic refreshes. `"0"` stops automatic refresh. |
| `github.limit_per_scope` | `100` | An integer from 1 through 1000 | The maximum number of PRs that each search query returns. |
| `github.max_concurrency` | `4` | An integer from 1 through 8 | The maximum simultaneous Search requests across all views and scopes. |
| `github.ci_batch_size` | `25` | An integer from 1 through 50 | The number of PRs in one GraphQL metadata query. |
| `github.scopes` | `["user:@me"]` | Each entry must be `user:name`, `org:name`, or `repo:owner/name`. Each entry must be unique. Configured views require at least one scope. | The scopes that views with `scope = "configured"` use. `@me` refers to your GitHub account. |
| `[[views]].id` | None | Lowercase letters, digits, `-`, and `_`. The ID must start with a letter. Each ID must be unique. | The ID of the view. |
| `[[views]].title` | None | A string that is not empty. | The name of the view in the board. |
| `[[views]].query` | None | A GitHub PR search that is not empty. | The PRs that the view shows. |
| `[[views]].scope` | None | `"global"` or `"configured"` | `"global"` runs the query one time. `"configured"` runs the query one time for each entry in `github.scopes`. |
| `sidebar.enabled` | `true` | `true` or `false` | Report PR counts into Herdr sidebar tokens after each full refresh. |
| `sidebar.ttl` | `"15m"` | `"0"` or a Go duration of `1m` or more, for example `"15m"` or `"1h"` | How long the reported tokens stay visible after the last report. `"0"` keeps the tokens until the next report. |
| `sidebar.review_view` | `"review"` | Lowercase letters, digits, `-`, and `_`. The value must start with a letter. | The view whose PR count reports as the `$prs_review` token. When no view has this ID, the plugin omits the token. |
| `review.auto_views` | `[]` | Unique configured view IDs | The views that supply automatic review candidates. An empty list disables automatic reviews. |
| `review.max_concurrency` | `1` | An integer from 1 through 8 | The shared limit for simultaneous manual and automatic reviews in one state directory. |
| `review.timeout` | `"30m"` | A Go duration from `"1s"` through `"24h"` | The review timeout, including queue time and execution time. |
| `review.notify` | `"all"` | `"all"`, `"problems"`, or `"off"` | The run outcomes that show a Herdr notification. `"problems"` limits notifications to blocked and failed runs. See [review notifications](reviews.md#review-notifications). |

Reviewer commands and repository permissions require separate configuration.
See [manual reviews](reviews.md) and [repository setup and publication](repository-publication.md) for those settings.

The board validates configuration at startup.
Invalid settings prevent startup.
The error message identifies the invalid setting.

## Validate your configuration

Validate a configuration from a source checkout:

```sh
bin/herdr-pr-board --config path/to/config.toml --validate
```

On Windows, use `bin/herdr-pr-board.exe`.
The command checks selected files for every built-in reviewer profile, including unused profiles.
Valid configuration produces `configuration is valid` and exit status `0`.
Invalid configuration produces an error and a nonzero exit status.
The command does not start the board, create a missing file, or require GitHub CLI.

## Add a custom view

Add another `[[views]]` section:

```toml
[[views]]
id = "security"
title = "Security queue"
query = 'is:open label:"security" -is:draft'
scope = "configured"
```

Use a unique lowercase `id`. Use a GitHub PR search for `query`.

## Stop automatic refresh

Set the refresh interval to zero:

```toml
[github]
refresh_interval = "0"
```

## Herdr sidebar counts

The board reports PR counts into Herdr sidebar tokens after each full refresh.
It reports the tokens only to the Herdr workspace that runs the board.
The reporting reuses the refresh snapshot. It makes no extra GitHub requests.

The board reports after initial, automatic, and manual full refreshes (`R`).
It does not report after an active-view refresh (`r`).

The sidebar shows the tokens only when you add them to the Herdr configuration.
Add a row to the space entries in `~/.config/herdr/config.toml`:

```toml
[ui.sidebar.spaces]
rows = [
  ["state_icon", "workspace"],
  ["branch", "git_status"],
  ["$prs_open", "$prs_review", "$prs_ci"],
]
```

Reload the Herdr configuration:

```sh
herdr server reload-config
```

The board reports these tokens:

| Token | Example value | Meaning |
| --- | --- | --- |
| `$prs_open` | `12 open` | The number of distinct pull requests on the board. |
| `$prs_review` | `3 review` | The number of pull requests in the `sidebar.review_view` view. |
| `$prs_ci` | `2 fail` | The number of distinct pull requests with a failed check. |

The board omits `$prs_ci` when no check failed. It omits `$prs_review` when no view has the configured ID. The board does not report after a refresh with a failed view. It keeps the previous tokens until they expire.

The tokens appear under the workspace that runs the board.
With a nonzero `sidebar.ttl`, tokens expire after the last report.
Closing the board stops reports.
With `sidebar.ttl = "0"`, closing the board does not expire tokens.
The headless monitor does not report workspace tokens.
The board reports tokens only when Herdr starts it. When you run the binary directly, the board does not report.

You can style each token with an inline style table, for example:

```toml
rows = [["state_icon", "workspace"], ["$prs_open", "$prs_review", { token = "$prs_ci", fg = "#f38ba8" }]]
```

The reporting needs the `herdr` command on `PATH`. The board must run inside Herdr. When the command fails, the board shows one warning in the footer. The board stays usable.

## API capacity and refresh failures

The plugin refreshes the board when it starts. The default automatic refresh interval is five minutes.

GitHub permits 30 authenticated Search requests each minute and returns at most 1,000 results per search.
See [GitHub Search limits](https://docs.github.com/en/rest/search/search#rate-limit).

The plugin runs view and scope searches at the same time. `github.max_concurrency` limits the number of Search API requests that run at the same time. The plugin removes duplicate PRs before it requests CI data.

The plugin gets CI data and your submitted reviews with batched GraphQL queries.
It uses your cached GitHub login to filter review authors.
Restart the monitor after switching GitHub accounts.
Review histories with more than 100 entries require additional pages.
Each page checks the available GraphQL capacity before it runs.
The usual user limit is 5,000 GraphQL points each hour.
Some authentication methods have different limits.
See [GitHub GraphQL limits](https://docs.github.com/en/graphql/overview/rate-limits-and-query-limits-for-the-graphql-api).
The plugin keeps complete metadata results for two minutes.
A refresh inside that time uses the kept result without another GraphQL query for that PR.
Failed or incomplete review observations retry on the next refresh.

The board shows the remaining Search API and GraphQL capacity. The board keeps old data when a refresh fails.

The footer reports when the active view last refreshed successfully. A failed refresh does not advance that time. When a refresh fails, the footer marks the view as `stale`. The board keeps old data. A `stale` notice above the table counts the retained rows until a refresh succeeds.
