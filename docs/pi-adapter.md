# Pi reference adapter

The Pi adapter uses the version-one reviewer contract.
The adapter runs separately from PR discovery.
Other reviewer commands do not require Pi.
The adapter does not change Pickr settings.

Select `herdr-pr-board --pi-reviewer` as the reviewer command.
Send one reviewer input object through standard input.
The adapter writes its result to `result_path`.
Use an absolute result path inside the review run directory.
The core creates this directory before launch.
The core applies the configured review timeout.

Install Pi and authenticate its model provider before launch.
The default executable is `pi` on `PATH`.
The default review uses embedded standards and specification criteria.
No prompt file or skill file is required.
Select `prompt_file` and `skill_file` in board settings or a named TOML profile.
See [review instructions](review-instructions.md) for scope, precedence, and examples.
Standalone adapters also accept `--pi-prompt` and `--pi-skill`.
Use `--pi-executable` to select another Pi executable.
These options apply only to `--pi-reviewer`.

The adapter gets the PR body and linked closing issues through GitHub CLI.
The default review blocks when specification retrieval fails or required context is empty.
Custom prompts replace the default criteria and their specification requirement.
Pi must block when available context cannot support its selected instructions.
The adapter checks the current PR revision against the captured revision.
A revision mismatch blocks the review.
Capture the current revision before retrying.

The adapter clones the repository into the review run directory.
It fetches the captured head and base commits.
It checks out the head commit in detached mode.
Git compares the captured base and head through their merge base.
The adapter removes its temporary checkout after the run.

Pi receives these arguments:

```text
--print --mode json --no-session --no-extensions --no-skills
--no-context-files --no-approve
```

Pi also receives `--skill <skill-file>` when a skill is selected.
The adapter supplies review instructions, the selected skill, and JSON evidence through standard input.
It also supplies the captured comparison diff and commit log.
Each captured Git command output has a four MiB limit.
Oversized diff or log output blocks the review.
Repository instructions remain review evidence.
Pi must not publish findings or change source files.
This instruction does not sandbox Pi or its tools.

The adapter retains `pi-events.jsonl` and `pi-stderr.log` in the run directory.
The event log retains at most 32 MiB.
The diagnostic log retains at most 1 MiB.
A truncated event log fails review validation.
On macOS and Linux, cancellation sends SIGTERM to the process group before SIGKILL.
The adapter allows one second for child cleanup.
The core allows three seconds for adapter cleanup.
Custom wrappers must forward termination and preserve the inherited claim descriptor.
On macOS and Linux, the core supplies descriptor 3 and sets `HERDR_REVIEW_CLAIM_FD=3`.
The adapter forwards this descriptor to Pi as descriptor 3.
Pi retains the claim if the adapter exits unexpectedly on these platforms.
On Windows, the environment variable contains a native handle value.
The adapter forwards that handle to Pi.
Windows stops the owned process tree if the adapter exits.
The adapter rejects invalid supplied descriptors.
Standalone invocations can omit this environment variable.
On macOS and Linux, Pi uses SIGTERM to stop its tracked detached children.
On Windows, cancellation terminates the owned Job Object and waits for its processes.
See [Windows setup](windows.md) for native installation requirements.

## Result validation

Exit status zero does not prove completion.
Pi must emit a final `agent_end` event.
Its last message must have role `assistant` and stop reason `stop`.
The final assistant text must contain one reviewer result JSON object.
Markdown fences and trailing text fail validation.
The result must match the captured identity and base commit.
The result must satisfy the shared outcome validation rules.
A completed result requires a summary and findings array.
Use an empty findings array for a completed review without findings.

## Event fixture provenance

`completed.jsonl` contains synthetic messages with the installed Pi 0.84.2 event structure.
`pi-0.84.2-recorded.jsonl` retains terminal events from an actual Pi review.
The review targets public PR `cdowell09/herdr-pr-board#56`.
The fixture removes intermediate messages and unused assistant fields.
The fixture preserves final assistant text and the following `agent_settled` event.

The relevant installed source files are:

- `@earendil-works/pi-coding-agent/dist/modes/print-mode.js`
- `@earendil-works/pi-coding-agent/dist/modes/json-event.js`
- `@earendil-works/pi-coding-agent/dist/core/agent-session.js`
- `@earendil-works/pi-agent-core/dist/types.d.ts`

Print mode emits the session header before agent events.
JSON conversion changes only `message_update` events.
`agent_end.messages` can contain user messages with string content.
The parser reads only the final assistant message.
It permits one terminal `agent_settled` event after `agent_end`.
New turns, messages, and errors after `agent_end` fail validation.
An `agent_end` event with `willRetry: true` also fails validation.
JSON mode can return zero after an assistant error.
The fixture tests reject error, aborted, length-limited, and incomplete turns.
