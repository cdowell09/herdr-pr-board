# Hermes native probes

Use the Python environment from Hermes release `2026.9.7`.
The release source commit is `2237be355906fbe6065ce1815711eee52b2d646e`.

Run the native probe from the repository root:

```sh
PR_BOARD_HERMES_NATIVE=/path/to/hermes/.venv/bin/python go test -v -run TestNativeIsolation -timeout 4m ./internal/hermesadapter
```

The probe uses generated credentials and a local HTTP server.
The probe does not contact a model provider.
Temporary settings contain hostile hooks, MCP servers, memory, and instruction files.
The success case reads a captured file through the native tool.
The probe checks that the requests contain no hostile instructions.
The probe checks that hooks and MCP servers create no marker files.

Chat Completions, Anthropic Messages, and Codex Responses each have success and truncated-response cases.
The Codex truncated response ends before its terminal event.
A Chat Completions case sends a cancellation signal during the request.
The probe keeps its generated inputs, requests, output, and diagnostics in the temporary directory.

`bridge_test.py` checks the installed interface boundary without installing Hermes.
The test checks isolation options, completion flags, cancellation, version checks, and large output.
