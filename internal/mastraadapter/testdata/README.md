# Native terminal fixtures

The fixture date is 2026-09-09.
The native packages are `mastracode` 0.39.0 and `@mastra/code-sdk` 1.7.0.
The runtime uses `@mastra/core` 1.65.0 and `@mastra/memory` 1.28.3.
The verification uses Node.js 26.7.0.

A local HTTP server returns synthetic OpenAI Responses events.
The adapter uses synthetic authentication and isolated native directories.
No model service receives a request.
The fixtures contain the adapter's native SDK output.
The error fixture excludes the machine-specific stack trace.

`native-stop.json` contains a complete response.
`native-length.json` contains a response with `max_output_tokens` termination.
`native-missing.json` contains a stream without its provider completion event.
Mastra reports completion for the missing event.
The adapter rejects its `other` provider finish reason.

The verification also tests one read-tool call before the final response.
The model requests contain only the four configured read tools.
The requests exclude the planted global and project instruction text.

See the [published SDK](https://www.npmjs.com/package/@mastra/code-sdk/v/1.7.0).
