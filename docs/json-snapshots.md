# JSON snapshots

Run a fresh scan without a terminal or Herdr:

```sh
bin/herdr-pr-board --json
bin/herdr-pr-board --json --view review --config path/to/config.toml
```

The command uses the same configuration and discovery module as the board.
Without `--view`, the command scans all configured views.
With `--view`, the command scans only that configured view ID.
The command preserves the configured view order.
The command does not report sidebar tokens.

Each invocation creates a new GitHub client.
The command does not reuse an earlier snapshot or metadata cache.
The scan has a 90-second timeout.
An interrupt or termination signal cancels the scan.
The command returns available data after retrieval failures.
The interactive board can retain an earlier successful observation after a failed search.
The JSON command does not retain that observation.

## Output and exit status

The command emits one JSON document on standard output after the scan.
Diagnostics use standard error.
Configuration failures and missing tools can prevent the scan and produce no JSON document.

| Exit status | Meaning |
| --- | --- |
| `0` | All attempted retrieval operations succeed. |
| `1` | An operational failure or retrieval failure occurs. |
| `2` | Command usage is invalid. |

Do not combine `--json` with `--validate`.
Use `--view` only with `--json`.
The view ID must exist in the configuration.
Do not supply positional arguments.

## Version-one contract

Consumers must check `schema_version` before they use the document.
Consumers must permit additional fields within the same schema version.
Consumers must treat `errors` as structured retrieval failures.
The `message` text is diagnostic text, not a stable identifier.
All timestamps use RFC 3339 format.
A timestamp can include fractional seconds.

| Document field | Type | Meaning |
| --- | --- | --- |
| `schema_version` | integer | The schema version, currently `1`. |
| `started_at` | timestamp | The local time immediately before discovery starts. |
| `finished_at` | timestamp | The local time after discovery finishes. |
| `limit_per_search` | integer | The configured maximum results for each Search query. |
| `views` | array | The requested configured views, including failed views. |
| `rates` | object | The last available Search and GraphQL rate resources from this invocation. |
| `errors` | array | Retrieval failures from this invocation. An empty array means no known retrieval failure. |

`started_at` and `finished_at` describe the scan interval.
They do not establish an atomic GitHub snapshot.
GitHub can change PR data between requests.

### Views

| View field | Type | Meaning |
| --- | --- | --- |
| `id` | string | The configured view ID. |
| `title` | string | The configured view title. |
| `query` | string | The configured Search query. |
| `scope` | string | `global` or `configured`. |
| `scopes` | array of strings | Configured scope expressions. Global views use an empty array. |
| `observed_at` | timestamp or null | The local time when Search returns available results. |
| `search_succeeded` | boolean | Whether all Search operations for this view succeed. |
| `completeness` | string | `unknown` in version one. |
| `prs` | array | Available PRs, with duplicate URLs removed within this view. |

Scope expressions preserve configuration values, including `@me`.
A successful empty search has an observation time and an empty `prs` array.
A failed search without available rows has a null observation time and an empty `prs` array.
A partial scoped search can return rows and an observation time with `search_succeeded` set to `false`.
Search completion does not prove complete result coverage.
GitHub CLI supplies a result array without evidence that all matching results are present.
Do not infer completeness from the row count or the configured limit.

### Pull requests

| PR field | Type | Meaning |
| --- | --- | --- |
| `repository` | string | The repository name with its owner. |
| `number` | integer | The PR number within the repository. |
| `url` | string | The PR URL. |
| `title` | string | The PR title from Search. |
| `author` | string | The author's GitHub login from Search. |
| `draft` | boolean | The draft state from Search. |
| `updated_at` | timestamp or null | GitHub's PR update time from Search. This is not an observation time. |
| `head_oid` | string or null | The observed head commit identity. |
| `base_ref_name` | string or null | The observed target branch name. |
| `base_oid` | string or null | The observed target branch commit identity. |
| `ci` | string or null | The observed aggregate CI state. |
| `metadata_observed_at` | timestamp or null | The local GraphQL response time for revision metadata and CI. |

A null metadata field means the value is unavailable.
CI values are `SUCCESS`, `PENDING`, `FAILURE`, `ERROR`, and `NONE`.
`NONE` means the response reports no checks.
A null `ci` means CI is unavailable.
An incomplete response can supply some metadata fields and omit others.
Its observation time describes the available fields only.
Metadata failures appear in `errors` and produce exit status `1`.
Successful Search rows remain available after metadata failures.
The shared discovery module preserves cached metadata observation times for interactive refreshes.

### Rate resources

`rates` contains `search` and `graphql`.
Each value is a rate resource object or null when unavailable.

| Rate resource field | Type | Meaning |
| --- | --- | --- |
| `limit` | integer | The reported capacity. |
| `remaining` | integer | The reported remaining capacity. |
| `reset_at` | timestamp or null | The reported reset time, when available. |
| `cost` | integer or null | The last reported GraphQL query cost, when available. Search cost is null. |

A failed rate refresh preserves the last available rate resource from this invocation.
The failure appears in `errors`.
Do not treat the rate resource as a guarantee for later requests.

### Errors

| Error field | Type | Meaning |
| --- | --- | --- |
| `stage` | string | `rates`, `search_budget`, `search`, or `enrichment`. |
| `view_id` | string or null | The affected view ID, when the failure identifies one view. |
| `message` | string | The diagnostic explanation. |

A null `view_id` can describe a failure that affects several views.
Enrichment batches combine duplicate PRs across views.
Enrichment errors therefore do not identify one view.
Rate retrieval failures do not prevent Search when capacity is unknown.
Known insufficient Search capacity prevents the scan's Search requests.
