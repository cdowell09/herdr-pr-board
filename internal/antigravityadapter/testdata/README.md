# Antigravity native fixtures

These fixtures come from Antigravity CLI 1.1.28 on macOS arm64.
The capture date is 2026-09-09.
The CLI uses a synthetic home and a local Gemini API mock.
The capture process does not use account credentials or paid model requests.
Conversation identifiers and checkout paths are replaced.

- `success.jsonl` contains a completed response and a successful terminal result.
- `timeout.jsonl` contains a native zero-duration timeout.
- `partial-timeout.jsonl` contains valid JSON text before a native one-second timeout.

Both timeout fixtures contain a successful terminal status.
The partial response step remains active.
The adapter must reject both timeout fixtures.

The `max-tokens` fixture contains the native error after token-limit retries.
The transport fixtures contain incomplete HTTP responses or incomplete SSE records.
The HTTP probes omit advertised content bytes or the final chunk marker.
The SSE probes contain an incomplete record or an incomplete record after valid text.
All four probes produce a native error.
The adapter must reject these fixtures, including errors with a zero process exit code.

The `missing-reason` and `unknown-reason` fixtures contain complete HTTP responses and complete SSE records.
These model responses omit the completion reason or contain an unknown completion reason.
The native CLI converts both cases to completed steps and successful terminal results.
The adapter accepts this native terminal contract.
PR Board still validates the review result and its captured revisions.

Run the optional native test with `ANTIGRAVITY_NATIVE_TEST_BINARY` set to the installed CLI path.
The test uses the actual adapter command and a local mock server.
The test checks selected instructions, source-file access, and excluded customizations.

Read the official [headless protocol](https://antigravity.google/docs/cli/headless/).
Read the official [authentication guide](https://antigravity.google/docs/cli/install/).
Read the official [agent configuration guide](https://antigravity.google/docs/subagents/).

The adapter requires the tested version because its independent directory controls are undocumented.
`--gemini_dir` isolates global customization discovery.
`--app_data_dir` preserves the native CLI data directory.
The named reviewer also disables customization and MCP inheritance.
The adapter rejects executable status-line and title settings.
The CLI executes these settings in headless mode.
