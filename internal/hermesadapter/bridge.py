import contextlib
import json
import os
from pathlib import Path
import signal
import sys
from types import SimpleNamespace


def main():
    control, checkout = map(Path, sys.argv[1:3])
    os.chdir(control)
    os.environ.update(
        HERMES_SAFE_MODE="1", HERMES_IGNORE_USER_CONFIG="1", HERMES_IGNORE_RULES="1",
        TERMINAL_CWD=str(control), TERMINAL_ENV="local",
    )
    # Native imports and diagnostics must not enter the machine result stream.
    with contextlib.redirect_stdout(sys.stderr):
        import hermes_cli
        if hermes_cli.__release_date__ != "2026.9.7":
            raise RuntimeError("Hermes adapter requires release 2026.9.7")
        from run_agent import AIAgent
        from hermes_cli.config import load_config_readonly, resolve_turn_limit
        from hermes_cli.oneshot import _resolve_model_and_provider
        from hermes_cli.runtime_provider import resolve_runtime_provider
        from toolsets import create_custom_toolset

        def check_transport(value):
            # External agents can bypass this process's customization and tool controls.
            if (value.api_mode not in {"chat_completions", "anthropic_messages", "codex_responses", "bedrock_converse"}
                    or value.provider in {"moa", "copilot-acp"}
                    or str(value.base_url).lower().startswith(("acp://", "acp+tcp://"))):
                raise RuntimeError("Hermes review requires an in-process provider transport")

        cfg = load_config_readonly()
        if cfg.get("context", {}).get("engine", "compressor") not in (None, "", "compressor"):
            raise RuntimeError("Hermes review requires the built-in compressor context engine")
        # Hermes resolves its own selected model and credentials in this process.
        choice = _resolve_model_and_provider(cfg, None, None)
        runtime = resolve_runtime_provider(
            requested=choice.provider, target_model=choice.model or None,
            explicit_base_url=choice.base_url, explicit_api_key=choice.api_key,
        )
        check_transport(SimpleNamespace(**{key: runtime.get(key) for key in ("api_mode", "provider", "base_url")}))
        create_custom_toolset("pr-board-review", "Read-only captured review", ["read_file", "search_files"])
        agent = AIAgent(
            api_key=runtime.get("api_key"), base_url=runtime.get("base_url"),
            provider=runtime.get("provider"), requested_provider=runtime.get("requested_provider"),
            api_mode=runtime.get("api_mode"), model=choice.model,
            credential_pool=runtime.get("credential_pool"), enabled_toolsets=["pr-board-review"],
            skip_context_files=True, skip_memory=True, skip_background_review=True,
            quiet_mode=True, platform=None,
            max_iterations=resolve_turn_limit(cfg.get("agent", {}).get("max_turns")),
        )
        cancelled = False

        def cancel(signum, frame):
            nonlocal cancelled
            cancelled = True
            agent.interrupt()

        signal.signal(signal.SIGTERM, cancel)
        signal.signal(signal.SIGINT, cancel)
        try:
            check_transport(agent)
            if agent.valid_tool_names != {"read_file", "search_files"}:
                raise RuntimeError("Hermes review tools differ from the read-only toolset")
            prompt = sys.stdin.read()
            if cancelled:
                raise RuntimeError("Hermes review cancelled before the run started")
            result = agent.run_conversation(f"Captured checkout: {checkout}\n\n{prompt}")
        finally:
            agent.close()
        messages = result.get("messages", [])
        last = messages[-1] if messages else {}
        text = result.get("final_response", "")
        output = {
            "type": "result",
            "completed": not cancelled and result.get("completed") is True and all(
                result.get(flag) is False for flag in (
                    "failed", "partial", "interrupted", "response_transformed", "response_previewed",
                )
            ),
            "finalAssistant": last.get("role") == "assistant" and not last.get("tool_calls") and last.get("content") == text,
            "text": text,
        }
    print(json.dumps(output), flush=True)


if __name__ == "__main__":
    main()
