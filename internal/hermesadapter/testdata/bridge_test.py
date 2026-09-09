"""Exercise the embedded bridge against the pinned Hermes method boundary."""
import contextlib
import io
import json
import os
from pathlib import Path
import runpy
import signal
import sys
from types import ModuleType, SimpleNamespace as NS
import unittest

bridge, control, checkout = sys.argv[1:]
sys.argv = [bridge, control, checkout]
scenario = "stop"
closed = False


def module(name, **values):
    result = ModuleType(name)
    result.__dict__.update(values)
    sys.modules[name] = result


def config():
    return {"context": {"engine": "poison" if scenario == "engine" else "compressor"}}


def runtime(**kwargs):
    assert kwargs == {"requested": "native", "target_model": "native-model", "explicit_base_url": None, "explicit_api_key": None}
    mode = {"codex": "codex_responses", "anthropic": "anthropic_messages", "bedrock": "bedrock_converse", "external": "codex_app_server"}.get(scenario, "chat_completions")
    return {"api_key": "native-key", "provider": "moa" if scenario == "provider" else "native", "api_mode": mode, "base_url": "acp://external" if scenario == "acp" else "native-url"}


class Agent:
    def __init__(self, **kwargs):
        assert kwargs["api_key"] == "native-key"
        assert kwargs["model"] == "native-model"
        assert kwargs["enabled_toolsets"] == ["pr-board-review"]
        assert kwargs["skip_context_files"] is True
        assert kwargs["skip_memory"] is True
        assert kwargs["skip_background_review"] is True
        assert kwargs["platform"] is None
        assert "fallback_model" not in kwargs
        assert Path.cwd().resolve() == Path(control).resolve()
        assert not Path(checkout).is_relative_to(control)
        for name in ("HERMES_SAFE_MODE", "HERMES_IGNORE_RULES", "HERMES_IGNORE_USER_CONFIG"):
            assert os.environ[name] == "1"
        assert os.environ["TERMINAL_CWD"] == control
        assert os.environ["TERMINAL_ENV"] == "local"
        self.__dict__.update(kwargs)
        self.valid_tool_names = {"read_file", "search_files"}
        if scenario == "tools":
            self.valid_tool_names.add("terminal")
        if scenario == "resolved-external":
            self.api_mode = "codex_app_server"

    def run_conversation(self, prompt):
        assert "selected prompt and skill" in prompt
        assert checkout in prompt
        text = "x" * (2 * 1024 * 1024) if scenario == "large" else "{}"
        last = {"role": "assistant", "content": text}
        if scenario == "pending":
            last["tool_calls"] = [{"id": "pending"}]
        if scenario == "cancel":
            signal.raise_signal(signal.SIGTERM)
        result = {"completed": True, "failed": False, "partial": False, "interrupted": False,
                  "response_transformed": False, "response_previewed": False,
                  "messages": [last], "final_response": text}
        if scenario in {"failed", "partial", "interrupted", "response_transformed", "response_previewed"}:
            result[scenario] = True
        if scenario == "missing-flag":
            del result["partial"]
        if scenario == "mismatched-text":
            last["content"] = "intermediate response"
        return result

    def interrupt(self):
        pass

    def close(self):
        global closed
        closed = True


module("hermes_cli", __release_date__="2026.9.7")
module("run_agent", AIAgent=Agent)
module("hermes_cli.config", load_config_readonly=config, resolve_turn_limit=lambda value: 3)
module("hermes_cli.oneshot", _resolve_model_and_provider=lambda *args: NS(provider="native", model="native-model", base_url=None, api_key=None))
module("hermes_cli.runtime_provider", resolve_runtime_provider=runtime)
module("toolsets", create_custom_toolset=lambda name, description, tools: tools == ["read_file", "search_files"] or sys.exit(1))


class BridgeTests(unittest.TestCase):
    def run_bridge(self, name):
        global scenario, closed
        scenario, closed = name, False
        output = io.StringIO()
        sys.stdin = io.StringIO("selected prompt and skill")
        if name == "pre-cancel":
            class CancelledInput(io.StringIO):
                def read(self):
                    signal.raise_signal(signal.SIGTERM)
                    return super().read()
            sys.stdin = CancelledInput("selected prompt and skill")
        with contextlib.redirect_stdout(output):
            runpy.run_path(bridge, run_name="__main__")
        self.assertTrue(closed)
        return json.loads(output.getvalue())

    def test_terminals_and_isolation(self):
        for name in ("stop", "anthropic", "codex", "bedrock"):
            with self.subTest(name=name):
                value = self.run_bridge(name)
                self.assertTrue(value["completed"])
                self.assertTrue(value["finalAssistant"])
                self.assertEqual(value["text"], "{}")

    def test_failed_conversation(self):
        for name in ("failed", "partial", "interrupted", "response_transformed", "response_previewed", "cancel", "missing-flag"):
            with self.subTest(name=name):
                self.assertFalse(self.run_bridge(name)["completed"])
        self.assertFalse(self.run_bridge("pending")["finalAssistant"])
        self.assertFalse(self.run_bridge("mismatched-text")["finalAssistant"])

    def test_large_result(self):
        self.assertEqual(len(self.run_bridge("large")["text"]), 2 * 1024 * 1024)

    def test_unsupported_runtime(self):
        for name in ("engine", "provider", "external", "acp", "resolved-external", "tools", "pre-cancel"):
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                self.run_bridge(name)
        sys.modules["hermes_cli"].__release_date__ = "2026.9.8"
        with self.assertRaisesRegex(RuntimeError, "requires release"):
            self.run_bridge("version")


unittest.main(argv=[bridge])
