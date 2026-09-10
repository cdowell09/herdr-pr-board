# Monitor PRs without a board

The board starts a background monitor when saved settings select automatic views and enable repository automatic launches.
The board checks these settings when it opens, after successful settings saves, and after configuration reloads.
It reuses an existing monitor in the same state directory.
Closing the board leaves the monitor running.
Startup does not change review or publication permissions.

Run a foreground monitor without opening the board:

```sh
export HERDR_PLUGIN_STATE_DIR="/absolute/path/to/plugin-state"
bin/herdr-pr-board --monitor --config /absolute/path/to/config.toml
```

On Windows, use the PowerShell commands in [Windows setup](windows.md#monitor-and-state).
The state directory must use an absolute path.
The monitor runs until you send `Ctrl+C`.
On macOS and Linux, `SIGTERM` also stops the monitor.
Only one monitor can use a state directory.
A crashed monitor releases ownership automatically.
Reopen the board, save settings, or reload configuration to retry a stopped background monitor.
The board does not continuously restart crashed monitors.
The plugin does not install an operating-system startup service.

## Diagnostics and recovery

Background diagnostics use the private `monitor.log` file in `HERDR_PLUGIN_STATE_DIR`.
The log retains its first 1 MiB and discards further output.
Each new background launch resets the log.
Startup failures appear in the board.
Use the displayed foreground command if background startup fails.
Review notification problems also appear in the log.
See [review notifications](reviews.md#review-notifications) for the `review.notify` setting.

## Shared observations

The monitor scans all views immediately.
It then uses `github.refresh_interval` between completed scans.
With `"0"`, it scans once and waits until stopped.
The monitor writes each observation atomically to `monitor-snapshot.json` in the state directory.
This internal file is not the public JSON contract.

Boards with the same state directory read monitor observations each second.
They do not schedule additional GitHub scans while the monitor runs.
An unchanged observation does not renew sidebar tokens.
Failed observations preserve errors and actual observation times.
An open board retains its previous successful rows after failed searches.
Monitor ownership does not mean that its observation is current.
The board resumes independent refreshes when the monitor stops.

Manual refreshes and `--json` still attempt fresh scans.
They wait for other scans in the same state directory.
Waiting uses the command's existing timeout.
A one-shot scan does not replace the monitor observation.
Commands without `HERDR_PLUGIN_STATE_DIR` keep independent refresh behavior.

Use the same discovery configuration for the monitor and board.
After changing views or GitHub settings, restart the monitor with the updated configuration.
A configuration mismatch shows an error instead of starting duplicate scheduled scans.
Automatic reviews remain disabled until you select views and enable repository launches.
See [automatic reviews](automatic-reviews.md) for dispatch, eligibility, and optional publication.
