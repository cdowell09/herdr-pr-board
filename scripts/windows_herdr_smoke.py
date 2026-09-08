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
    while time.monotonic() < deadline:
        value = probe()
        if value:
            return value
        time.sleep(0.1)
    raise AssertionError(f"Timed out: {description}")


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
            print("Native Herdr GitHub install, reinstall, link, action, pane reuse, refresh, and cleanup passed.")
        except BaseException:
            print((temporary / "server.log").read_text(encoding="utf-8", errors="replace"))
            print(host("plugin", "log", "list", "--plugin", PLUGIN, check=False).stdout)
            raise
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
