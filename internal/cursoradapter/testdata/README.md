# Cursor SDK terminal fixtures

These fixtures use the official `@cursor/sdk` package, version 1.0.31.
The verification date is 2026-09-09.
The host uses macOS arm64 and Node.js 26.7.0.
The adapter bridge runs against a local mock Cursor backend.
The backend uses the package's Connect protocol and generated Protobuf field definitions.
All credentials and repository files are generated test values.
No request uses production credentials or a paid model.

The `tools` fixture completes a native file read before the final answer.
The `pending` fixture omits the tool completion event.
The SDK reports `finished`, but the adapter rejects the pending tool.
The `missing` fixture omits the terminal turn event.
The `error` fixture returns a token-limit protocol error.
The `error_after` fixture returns that error after the terminal turn event.
The `cancel` fixture receives SIGTERM during an active stream.
The `stored` fixture uses the SDK's native authentication store with a generated fake key.

Separate native probes plant personal and project hooks, MCP servers, rules, and repository instructions.
An empty `settingSources` list excludes those sources, including during a nested file read.
The positive control uses `settingSources: ["all"]`.
That control executes all four generated hooks and MCP commands.
It also includes the planted project rules and repository instructions in the request context.

See the [official SDK reference](https://cursor.com/docs/sdk/typescript)
and [pinned package](https://www.npmjs.com/package/@cursor/sdk/v/1.0.31).
