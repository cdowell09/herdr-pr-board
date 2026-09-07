# Automatic reviews

Automatic reviews run through the headless monitor.
The board can start the monitor.
JSON snapshots do not start the monitor or automatic reviews.
The monitor uses existing discovery views.

## Enable automatic reviews

Press `v`, then press `s` to open repository settings.
Enable automatic launches for the repository.
Select existing IDs under Global views.
These selections apply to all repositories that allow automatic launches.
The panel does not select views automatically.
Press Enter to save repository settings and global view selections together.
Use PgUp, PgDn, or the mouse wheel to scroll through long settings.

You can also select existing view IDs in `config.toml`:

```toml
[review]
auto_views = ["review"]
max_concurrency = 1
timeout = "30m"
```

An empty `auto_views` list disables automatic reviews.
Each selected ID must identify one configured view.
Duplicate IDs are invalid.

Configure a reviewer and enable each repository:

```toml
[[reviewers]]
id = "pi"
command = ["/absolute/path/to/herdr-pr-board", "--pi-reviewer"]

[[repositories]]
name = "owner/repository"
reviewer = "pi"
auto_launch = true
publish_actions = []
auto_publish = ""
```

Repository settings also support equivalent board and command controls.
See [repository settings](repository-publication.md).

The board starts the monitor when saved automatic views and repository automatic launches are enabled.
It checks on board open, successful settings saves, and configuration reloads.
It reuses an existing monitor in the same state directory.
Closing the board leaves the monitor running.
Startup does not grant review or publication permissions.

Start a foreground monitor without opening the board:

```sh
export HERDR_PLUGIN_STATE_DIR="/absolute/path/to/plugin-state"
bin/herdr-pr-board --monitor --config /absolute/path/to/config.toml
```

The setup panel shows the monitor as running, stopped, or unknown.
A running monitor does not prove that its latest observation succeeded.
The panel reports failed observations and configuration differences separately.
When the monitor stops, the panel shows its exact command.
If background startup fails, copy the complete command and run it in another terminal.
Keep the command's continuation characters when copying multiple lines.
The command uses the current executable, configuration path, and state directory.
Startup failures remain visible until a startup retry succeeds.
Background diagnostics use the private `monitor.log` file in `HERDR_PLUGIN_STATE_DIR`.
The log retains its first 1 MiB and discards further output.
Each new background launch resets the log.
Reopen the board, save settings, or reload configuration to retry a stopped monitor.
The plugin does not continuously restart crashed monitors or install an operating-system startup service.
The board refreshes this local status while the review panel remains open.
New selections can take effect on the next monitor scan.
A ready setup still requires an eligible PR.

The monitor requires one installation state directory.
The monitor shares review claims and concurrency limits with manual reviews.
Foreground monitors report eligibility and outcomes on standard error.
Background monitors write these diagnostics to `monitor.log`.
Stopping the monitor cancels its reviews and waits for subprocess cleanup.

## Eligibility

A PR must meet every condition:

- A selected view contains the PR.
- The latest full observation succeeds.
- The observation includes current revision metadata.
- The PR is open and is not a draft.
- The repository allows automatic launches.
- The revision has no active process or completed review.
- The revision has no failed, blocked, or abandoned attempt that requires retry.

Failed CI does not prevent review.
Duplicate PRs across views produce one candidate.
Conflicting observations prevent dispatch.
Cached metadata from an earlier scan does not establish current revision evidence.
Retained board rows do not enter automatic dispatch.

Before launch, the service rechecks repository permission, selected views, and the current revision.
A changed head or target prevents launch from the earlier observation.
A later full observation can make that revision eligible.
Comments and title edits do not change review identity.

Failed attempts require explicit retry through `N` or `--review --rerun`.
Other eligible PRs continue after a failed review.
Automatic requests do not wait indefinitely for capacity.
New monitor observations replace pending work.

Change launch permissions or selected automatic views without restarting the monitor.
Restart the monitor after changing discovery queries, scopes, or GitHub settings.
The monitor holds dispatch when its discovery configuration differs from the current configuration.

## Inspect eligibility

Retrieve a fresh eligibility report:

```sh
bin/herdr-pr-board --review-eligibility --config /absolute/path/to/config.toml
```

The command emits version-one JSON with observation time and per-PR decisions.
Each decision includes identity, URL, view IDs, eligibility, and its reason.
Retrieval failures preserve available decisions and return exit status `1`.
The command does not launch a reviewer.

Press `v` on the board to inspect the selected PR.
The review panel shows the same eligibility reason from the latest full observation.
A newer active-view revision invalidates that earlier eligibility evidence.
Run history records completed, failed, blocked, and abandoned outcomes separately.

## Optional automatic publication

Automatic publication requires an explicit selector and the corresponding permission:

```toml
[[repositories]]
name = "owner/repository"
reviewer = "pi"
auto_launch = true
publish_actions = ["comment"]
auto_publish = "comment"
```

Valid selectors are `comment`, `approve`, `request_changes`, and an empty string.
An empty selector keeps findings local.
Comment permission alone does not enable automatic posting.
The selector must also appear in `publish_actions`.
Findings do not select an action automatically.

Select an After review action under Automatic posting in repository settings.
Keep local sends no automatic posts.
Post comment posts a completed automatic review as a comment.
Selecting comment publication also enables comment permission.
Other automatic actions require their separate permission first.
Removing the selected permission clears the automatic selector.

Equivalent command:

```sh
bin/herdr-pr-board --repository-settings owner/repository \
  --publish-actions comment --auto-publish comment
```

Only a completed automatic review can enter automatic publication.
The publication service rechecks revision, action permission, and the automatic selector before sending.
Manual publication uses its separate action permission.
Review adapters must keep findings local.
