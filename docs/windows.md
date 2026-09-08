# Windows setup

PR Board supports native Windows on x64.
Use Windows 10 version 1809 or later, or Windows 11.
Herdr uses [ConPTY](https://learn.microsoft.com/en-us/windows/console/createpseudoconsole), which requires Windows 10 version 1809 or later.
Install Herdr 0.9.0 or later, Git, GitHub CLI, and Go 1.24 or later.
Use local NTFS volumes for configuration and runtime state.
Do not use network shares for runtime state.
The native plugin does not require Bash or WSL.

## Install and open

Authenticate GitHub CLI:

```powershell
gh auth login
```

Install and open the plugin:

```powershell
herdr plugin install cdowell09/herdr-pr-board
herdr plugin action invoke open --plugin cdowell09.pr-board
```

Herdr builds `bin/herdr-pr-board.exe` and runs the native entrypoints.
The open action reuses one tab named `PR Board`.
The board uses your default browser for PR links.
The configuration editor uses `VISUAL`, then `EDITOR`, then Notepad.

Build and link a local checkout:

```powershell
go build -o bin/herdr-pr-board.exe ./cmd/herdr-pr-board
herdr plugin link "$PWD" --enabled
```

## Reviewers

Install and authenticate the selected agent CLI separately.
The agent CLI must support native Windows.
Built-in adapters accept native executables and standard npm launchers for Node.js.
Install Node.js when the agent CLI requires it.
The adapter launches Node.js directly and preserves each argument.
Other `.cmd` and `.bat` wrappers are not supported.
Use a native executable for custom reviewer commands.

Use single-quoted Windows paths in TOML:

```toml
[[reviewers]]
id = "pi"
command = ['C:\Tools\herdr-pr-board.exe', '--pi-reviewer']
```

Replace the example path with the installed PR Board executable path.
See the [Pi guide](pi-adapter.md) and [Codex and Claude Code guide](agent-adapters.md) for CLI requirements.

## Monitor and state

The board starts the monitor when saved settings enable automatic reviews.
Closing the board leaves the monitor running.
The monitor shares review claims and concurrency limits with board and CLI reviews.

Run a foreground monitor from a source checkout:

```powershell
$env:HERDR_PLUGIN_STATE_DIR = "$env:LOCALAPPDATA\PRBoard\state"
.\bin\herdr-pr-board.exe --monitor --config "$env:LOCALAPPDATA\PRBoard\config.toml"
```

Replace both example paths with your installation paths.
Use the same state directory for the board, monitor, and review commands.
The setup panel supplies a PowerShell command with your actual paths.
Copy all command lines and keep the continuation characters.
Press `Ctrl+C` to stop a foreground monitor.

To stop one review, select its PR, press `v`, then press `t`.
Other reviews and the monitor continue.
The runner waits for the selected process tree before releasing its review slot.
Windows also stops the owned process tree if its launcher exits unexpectedly.
Configuration and runtime state use the account's Windows file permissions.

## Native verification

CI runs all Go tests with race detection on native Windows.
CI installs the tested revision through Herdr 0.9.0.
It verifies native builds, pane reuse, GitHub retrieval, configuration preservation, and cleanup.
The process tests use native GitHub and agent fixtures.
