"""Exercise the native plugin in an isolated Herdr 0.9.0 session on Windows."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import tomllib
import urllib.request
import uuid
import zipfile


HERDR_URL = "https://github.com/herdrdev/herdr/releases/download/v0.9.0/herdr-windows-x86_64.zip"
HERDR_SHA256 = "b4508c445de1c1a68c760a01735da2aba2fa214b2aafd4b07f732e49b2a64b11"
PLUGIN = "cdowell09.pr-board"


def wait_for(description, probe, seconds=30):
    deadline = time.monotonic() + seconds
    last_error = None
    while time.monotonic() < deadline:
        try:
            value = probe()
            if value:
                return value
        except (FileNotFoundError, PermissionError) as error:
            # Python opens cannot share DELETE with a native atomic replacement.
            # Retry only transient file access during this bounded observation.
            last_error = error
        time.sleep(0.1)
    raise AssertionError(f"Timed out: {description}; last file error: {last_error}")


MONITOR_GUARD = r"""
$ErrorActionPreference = 'Stop'
$monitor = $null
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $matches = @(Get-CimInstance Win32_Process -Filter "Name = 'herdr-pr-board.exe'" |
            Where-Object {
                $_.ExecutablePath -eq $env:PR_BOARD_SMOKE_EXE -and
                $_.CommandLine -and $_.CommandLine.Contains('--monitor') -and
                $_.CommandLine.Contains($env:PR_BOARD_SMOKE_CONFIG)
            })
        if ($matches.Count -gt 1) { throw 'Multiple matching test monitors' }
        if ($matches.Count -eq 1) {
            $monitor = [Diagnostics.Process]::GetProcessById($matches[0].ProcessId)
            $null = $monitor.Handle # Retain this exact process before reporting readiness.
            break
        }
        Start-Sleep -Milliseconds 100
    }
    if ($null -eq $monitor) { throw 'The test monitor did not start' }
    [IO.File]::WriteAllText($env:PR_BOARD_SMOKE_READY, [string]$monitor.Id)
    while (-not [IO.File]::Exists($env:PR_BOARD_SMOKE_STOP)) {
        if ($monitor.HasExited) { throw 'The monitor exited before test cleanup' }
        Start-Sleep -Milliseconds 100
    }
    if ($monitor.HasExited) { throw 'The monitor exited with its board pane' }
} finally {
    if ($null -ne $monitor) {
        if (-not $monitor.HasExited) { $monitor.Kill() }
        if (-not $monitor.WaitForExit(5000)) { throw 'The test monitor did not stop' }
        $monitor.Dispose()
    }
}
"""


def smoke(root, temporary):
    manifest = tomllib.loads((root / "herdr-plugin.toml").read_text(encoding="utf-8"))
    assert "windows" in manifest["platforms"], "The manifest must declare Windows support"
    env = os.environ.copy()
    for name in list(env):
        if name.startswith("HERDR_"):
            env.pop(name)
    env.update(
        XDG_CONFIG_HOME=str(temporary / "configuration"),
        XDG_STATE_HOME=str(temporary / "state"),
        XDG_CACHE_HOME=str(temporary / "cache"),
        HERDR_SESSION="pr-board-ci-" + uuid.uuid4().hex,
        GH_TEST_LOG=str(temporary / "gh-calls"),
        GH_TEST_MODE="",
        GH_TEST_ELIGIBLE="",
        GH_REVIEW_METADATA="",
        GH_TOKEN="",
        GITHUB_TOKEN="",
        GORACE="atexit_sleep_ms=0",
    )
    config_root = Path(env["XDG_CONFIG_HOME"]) / "herdr"
    config_root.mkdir(parents=True)
    (config_root / "config.toml").write_text(
        'onboarding = false\n[update]\nversion_check = false\nmanifest_check = false\n',
        encoding="utf-8",
    )
    archive = temporary / "herdr.zip"
    urllib.request.urlretrieve(HERDR_URL, archive)
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == HERDR_SHA256, "Herdr checksum mismatch"
    with zipfile.ZipFile(archive) as bundle:
        bundle.extractall(temporary / "herdr")
    binaries = list((temporary / "herdr").rglob("herdr.exe"))
    assert len(binaries) == 1, binaries
    herdr = binaries[0]
    tools = temporary / "tools"
    tools.mkdir()
    subprocess.run(
        ["go", "test", "-c", "-o", str(tools / "gh.exe"), "./cmd/herdr-pr-board"],
        cwd=root, env=env, check=True, timeout=120,
    )
    env["PATH"] = str(tools) + os.pathsep + env["PATH"]

    def host(*args, json_result=True, check=True, timeout=20):
        result = subprocess.run(
            [str(herdr), *args], cwd=root, env=env, capture_output=True,
            text=True, encoding="utf-8", errors="replace", timeout=timeout,
        )
        if not check:
            return result
        if result.returncode:
            raise AssertionError(f"Herdr {args}: {result.stdout}\n{result.stderr}")
        if not json_result:
            return result.stdout.strip()
        response = json.loads(result.stdout)
        assert "error" not in response, response
        return response["result"]

    assert "0.9.0" in host("--version", json_result=False)
    source = os.environ["GITHUB_REPOSITORY"]
    revision = os.environ["GITHUB_SHA"]
    host("plugin", "install", source, "--ref", revision, "--yes", json_result=False, timeout=180)
    linked = host("plugin", "list", "--json", "--plugin", PLUGIN)["plugins"]
    assert len(linked) == 1, linked
    config_dir = Path(host("plugin", "config-dir", PLUGIN, json_result=False))
    assert config_dir.is_relative_to(config_root), config_dir
    state_dir = Path(env["XDG_STATE_HOME"]) / "herdr" / "plugins" / PLUGIN
    record = state_dir / "pane-id"
    config = config_dir / "config.toml"
    monitor_guard = None
    monitor_stop = temporary / "stop-monitor"
    with (temporary / "server.log").open("w", encoding="utf-8") as log:
        server = subprocess.Popen([str(herdr), "server"], cwd=root, env=env, stdout=log, stderr=log)
        try:
            wait_for("Herdr server", lambda: host("workspace", "list", check=False).returncode == 0)
            workspace = host("workspace", "create", "--cwd", str(root), "--label", "PR Board CI", "--focus")
            workspace_id = workspace["workspace"]["workspace_id"]
            actions = host("plugin", "action", "list", "--plugin", PLUGIN)["actions"]
            assert actions, "The plugin has no registered action"

            def open_board():
                host("plugin", "action", "invoke", "open", "--plugin", PLUGIN)
                return wait_for("board pane record", lambda: record.read_text().strip() if record.exists() else None)

            pane = open_board()
            wait_for("board configuration", config.exists)
            original = config.read_bytes()
            wait_for("native GitHub refresh", lambda: "api graphql" in Path(env["GH_TEST_LOG"]).read_text() if Path(env["GH_TEST_LOG"]).exists() else False)
            wait_for("board UI", lambda: "acme/api" in host("pane", "read", pane, "--source", "visible", json_result=False))
            tabs = host("tab", "list", "--workspace", workspace_id)["tabs"]
            assert sum(tab.get("label") == "PR Board" for tab in tabs) == 1, tabs
            before = {tab["tab_id"] for tab in tabs}
            assert open_board() == pane, "The action created a second board pane"
            assert {tab["tab_id"] for tab in host("tab", "list", "--workspace", workspace_id)["tabs"]} == before
            assert config.read_bytes() == original, "Pane reuse changed configuration"
            host("pane", "send-keys", pane, "ctrl+c")
            wait_for("ownership-safe pane cleanup", lambda: not record.exists())
            assert config.read_bytes() == original, "Pane cleanup changed configuration"
            reopened = open_board()
            assert reopened != pane, "The action reused a closed pane"
            wait_for("reopened board UI", lambda: "acme/api" in host("pane", "read", reopened, "--source", "visible", json_result=False))
            assert config.read_bytes() == original, "Reopening changed configuration"
            host("pane", "send-keys", reopened, "ctrl+c")
            wait_for("reopened pane cleanup", lambda: not record.exists())
            wait_for("closed board process", lambda: reopened not in {
                item["pane_id"] for item in host("pane", "list", "--workspace", workspace_id)["panes"]
            })
            host("plugin", "install", source, "--ref", revision, "--yes", json_result=False, timeout=180)
            assert config.read_bytes() == original, "Reinstalling changed configuration"
            host("plugin", "unlink", PLUGIN)
            host("plugin", "link", str(root))
            assert config.read_bytes() == original, "Local linking changed configuration"
            # The CI build supplies this local linked executable.
            linked_pane = open_board()
            wait_for("linked board UI", lambda: "acme/api" in host("pane", "read", linked_pane, "--source", "visible", json_result=False))
            host("pane", "send-keys", linked_pane, "ctrl+c")
            wait_for("linked pane cleanup", lambda: not record.exists())
            automatic = original.decode("utf-8").replace('auto_views = []', 'auto_views = ["review"]', 1)
            assert automatic != original.decode("utf-8"), "The automatic-view fixture did not apply"
            automatic += '\n[[reviewers]]\nid = "native-smoke"\ncommand = ' + json.dumps([str(tools / "gh.exe"), "unused", "reviewer"]) + '\n'
            # This repository never appears in the GitHub fixture. No review can launch.
            automatic += '[[repositories]]\nname = "acme/monitor-smoke-no-candidate"\nreviewer = "native-smoke"\nauto_launch = true\n'
            config.write_text(automatic, encoding="utf-8")
            monitor_ready = temporary / "monitor-ready"
            guard_env = dict(env, PR_BOARD_SMOKE_EXE=str(root / "bin" / "herdr-pr-board.exe"),
                             PR_BOARD_SMOKE_CONFIG=str(config), PR_BOARD_SMOKE_READY=str(monitor_ready),
                             PR_BOARD_SMOKE_STOP=str(monitor_stop))
            monitor_guard = subprocess.Popen(
                ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", MONITOR_GUARD],
                env=guard_env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            )
            monitoring_pane = open_board()

            def monitor_is_ready():
                if monitor_guard.poll() is not None:
                    raise AssertionError(monitor_guard.communicate()[0])
                return monitor_ready.read_text()

            wait_for("native background monitor", monitor_is_ready)
            wait_for("monitoring board UI", lambda: "acme/api" in host("pane", "read", monitoring_pane, "--source", "visible", json_result=False))
            host("pane", "send-keys", monitoring_pane, "v")
            wait_for("monitor ownership in board", lambda: "Monitor: running" in host("pane", "read", monitoring_pane, "--source", "visible", json_result=False))
            snapshot_path = state_dir / "monitor-snapshot.json"
            observed = wait_for("monitor observation", lambda: json.loads(snapshot_path.read_text()))
            assert observed["Version"] == 1 and observed["Views"] and not observed["Errors"], observed
            host("pane", "send-keys", monitoring_pane, "ctrl+c")
            wait_for("monitoring board cleanup", lambda: not record.exists())
            retained = wait_for("retained monitor observation", lambda: json.loads(snapshot_path.read_text()))
            assert retained["Config"] == observed["Config"], retained
            assert monitor_guard.poll() is None, "The background monitor stopped with its board"
            monitoring_pane = open_board()
            wait_for("monitor reuse board UI", lambda: "acme/api" in host("pane", "read", monitoring_pane, "--source", "visible", json_result=False))
            host("pane", "send-keys", monitoring_pane, "ctrl+c")
            wait_for("monitor reuse board cleanup", lambda: not record.exists())
            print("Native Herdr install, reinstall, link, pane reuse, refresh, and monitor survival passed.")
        except BaseException:
            print((temporary / "server.log").read_text(encoding="utf-8", errors="replace"))
            print(host("plugin", "log", "list", "--plugin", PLUGIN, check=False).stdout)
            raise
        finally:
            try:
                if monitor_guard is not None:
                    monitor_stop.touch()
                    output, _ = monitor_guard.communicate(timeout=40)
                    assert monitor_guard.returncode == 0, output
            finally:
                host("server", "stop", json_result=False, check=False)
                try:
                    server.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait(timeout=5)


if __name__ == "__main__":
    if os.name != "nt":
        raise SystemExit("Run this smoke test on native Windows.")
    with tempfile.TemporaryDirectory(prefix="PR Board native ") as directory:
        smoke(Path.cwd(), Path(directory))
