"""Run the installed Hermes bridge with generated credentials and loopback responses."""
import http.server
import json
import os
from pathlib import Path
import subprocess
import signal
import sys
import tempfile
import threading


def parent():
    root = Path(tempfile.mkdtemp(prefix="pr-board-hermes-native-"))
    home, control, checkout = (root / name for name in ("home", "control", "checkout"))
    for path in (home / ".hermes", control, checkout / "nested"):
        path.mkdir(parents=True)
    hhome = home / ".hermes"
    for path in [hhome / "SOUL.md", hhome / "AGENTS.md", hhome / "memories/MEMORY.md", hhome / "memories/USER.md", checkout / "AGENTS.md", checkout / "nested/AGENTS.md"]:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("UNSELECTED_POISON_INSTRUCTIONS")
    (checkout / "nested/target.txt").write_text("captured target content")
    requests = []
    mode = sys.argv[3]
    reason = sys.argv[4]
    if reason == "missing":
        reason = None
    read_tool = mode == "chat_completions" and reason == "stop"
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass
        def do_GET(self):
            data = json.dumps({"data": [{"id": "pr-board-probe", "object": "model"}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data) + (100 if reason == "truncated" else 0)))
            self.end_headers()
            self.wfile.write(data)
        def do_POST(self):
            req = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))))
            requests.append(req)
            if reason == "cancel" and req.get("messages"):
                proc.send_signal(signal.SIGTERM)
            if mode == "codex_responses" and self.path.endswith("/responses"):
                events = [
                    {"type": "response.created", "response": {"id": "resp-probe", "object": "response", "status": "in_progress", "model": "pr-board-probe", "output": []}},
                    {"type": "response.output_item.added", "output_index": 0, "item": {"id": "msg-probe", "type": "message", "role": "assistant", "status": "in_progress", "content": []}},
                    {"type": "response.output_text.delta", "item_id": "msg-probe", "output_index": 0, "content_index": 0, "delta": "{}"},
                ]
                if reason not in (None, "truncated"):
                    status = "completed" if reason == "stop" else "incomplete"
                    events.append({"type": "response." + status, "response": {"id": "resp-probe", "object": "response", "status": status, "model": "pr-board-probe", "output": [{"id": "msg-probe", "type": "message", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "{}", "annotations": []}]}], "usage": {"input_tokens": 20, "output_tokens": 2, "total_tokens": 22}}})
                data = "".join("event: " + e["type"] + "\ndata: " + json.dumps(e) + "\n\n" for e in events).encode()
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Content-Length", str(len(data) + (100 if reason == "truncated" else 0)))
                self.end_headers()
                self.wfile.write(data)
                return
            if mode == "anthropic_messages" and "/messages" in self.path:
                data = json.dumps({"id": "msg-probe", "type": "message", "role": "assistant", "content": [{"type": "text", "text": "{}"}], "model": "pr-board-probe", "stop_reason": "end_turn" if reason == "stop" else reason, "stop_sequence": None, "usage": {"input_tokens": 20, "output_tokens": 2}}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data) + (100 if reason == "truncated" else 0)))
                self.end_headers()
                self.wfile.write(data)
                return
            message = {"role": "assistant", "content": "{}"}
            stop = reason
            if read_tool and len([r for r in requests if r.get("messages")]) == 1:
                stop = "tool_calls"
                message = {"role": "assistant", "content": None, "tool_calls": [{"id": "read-1", "type": "function", "function": {"name": "read_file", "arguments": json.dumps({"path": str(checkout / "nested/target.txt")})}}]}
            data = json.dumps({"id": "chatcmpl-probe", "object": "chat.completion", "model": "pr-board-probe", "choices": [{"index": 0, "message": message, "finish_reason": stop}], "usage": {"prompt_tokens": 40, "completion_tokens": 8, "total_tokens": 48}}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data) + (100 if reason == "truncated" else 0)))
            self.end_headers()
            self.wfile.write(data)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    config = {
        "model": {"default": "pr-board-probe", "provider": "custom:probe", "api_mode": mode, "base_url": f"http://127.0.0.1:{server.server_port}/v1", "streaming": False},
        "custom_providers": [{"name": "probe", "base_url": f"http://127.0.0.1:{server.server_port}/v1", "api_mode": mode, "api_key": "FAKE-HERMES-KEY"}],
        "agent": {"max_turns": 3, "environment_probe": False},
        "hooks": {"pre_llm_call": [{"command": f"touch {root}/hook-marker"}]},
        "hooks_auto_accept": True,
        "mcp_servers": {"poison": {"command": "touch", "args": [str(root / "mcp-marker")]}},
        "platform_hints": {"cli": "UNSELECTED_POISON_PLATFORM", "": "UNSELECTED_POISON_EMPTY_PLATFORM"},
        "personalities": {"default": "UNSELECTED_POISON_PERSONALITY"},
    }
    (hhome / "config.yaml").write_text(json.dumps(config))
    env = {key: value for key, value in os.environ.items() if key in {"PATH", "TMPDIR", "SYSTEMROOT"}}
    env.update(HOME=str(home), HERMES_HOME=str(hhome), HERMES_MANAGED_DIR=str(root / "managed"), HERMES_SAFE_MODE="1", PYTHONDONTWRITEBYTECODE="1", HERMES_MAX_ITERATIONS="3")
    try:
        proc = subprocess.Popen([sys.argv[1], "-I", sys.argv[2], str(control), str(checkout)], env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            stdout, stderr = proc.communicate("Read the captured checkout nested/target.txt. Return only {}.", timeout=90)
        except BaseException:
            proc.kill()
            proc.communicate()
            raise
        (root / "stdout.json").write_text(stdout)
        (root / "stderr.log").write_text(stderr)
        (root / "requests.json").write_text(json.dumps(requests, indent=2))
        print(json.dumps({"root": str(root), "exit": proc.returncode, "requests": len(requests), "poison": "UNSELECTED_POISON" in json.dumps(requests), "markers": [p.name for p in root.glob("*-marker")]}))
        print(stdout[:2500])
        print(stderr[-4000:])
        if proc.returncode != 0 or "UNSELECTED_POISON" in json.dumps(requests) or list(root.glob("*-marker")):
            raise RuntimeError("Native Hermes failed or loaded unselected customization")
        if read_tool and "captured target content" not in json.dumps(requests):
            raise RuntimeError("Native Hermes did not read the captured file")
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    parent()
