# Codex and Claude Code adapters

PR Board includes local review adapters for Codex and Claude Code.
Both adapters use the same reviewer contract as Pi.
See [additional agent compatibility](agent-compatibility.md) for Oh My Pi, Kimi, Qoder CLI, and Qwen Code.
The adapters share checkout preparation, specification retrieval, result validation, and process cleanup.
They do not publish directly to GitHub.
Repository publication settings control the separate publication step.

## Requirements

Install Git, GitHub CLI, and the selected agent CLI.
Authenticate GitHub CLI and the selected agent before starting a review.
The adapters use the agent's existing authentication.
They do not read or copy credential files.

| Adapter | Supported CLI | Default executable | Reviewer flag |
| --- | --- | --- | --- |
| Codex | Codex CLI 0.153.4 or later | `codex` | `--codex-reviewer` |
| Claude Code | Claude Code 2.1.259 or later | `claude` | `--claude-reviewer` |

Older versions can reject required isolation or output flags.
An unsupported flag fails the review instead of weakening the command's restrictions.
See the [Codex CLI documentation](https://learn.chatgpt.com/docs/non-interactive-mode).
See the [Claude Code CLI reference](https://code.claude.com/docs/en/cli-reference).

The default review uses embedded standards and specification criteria.
No prompt file or skill file is required.
Select `prompt_file` and `skill_file` in board settings or named TOML profiles.
Standalone adapters also accept `--codex-prompt`, `--claude-prompt`, `--codex-skill`, and `--claude-skill`.
Use `--codex-executable` or `--claude-executable` to select another executable.
Each option requires its matching reviewer flag.
Custom prompts replace the embedded review criteria.
The agent must report a blocked outcome when it cannot complete its selected requirements.
See [review instructions](review-instructions.md) for scope, precedence, and file rules.

## Configure a repository

Open the review panel with `v` and open repository settings.
Select `codex` or `claude` in the reviewer row.
Save the settings.
Setup adds only the selected missing reviewer definition.
Existing reviewer IDs and custom commands remain unchanged.

You can also configure a repository through the CLI:

```sh
bin/herdr-pr-board --repository-settings owner/repository --use-codex-reviewer
bin/herdr-pr-board --repository-settings owner/another-repository --use-claude-reviewer
```

Use `--set-reviewer ID` when the reviewer ID already exists.
Choose only one reviewer setup flag per command.
These commands do not enable automatic reviews or publication.
See [repository publication](repository-publication.md) for those settings.

For different review criteria, define named reviewer profiles:

```toml
[[reviewers]]
id = "codex-security"
command = ["/absolute/path/to/herdr-pr-board", "--codex-reviewer"]
prompt_file = "reviews/security.md"
skill_file = ""

[[reviewers]]
id = "claude-product"
command = ["/absolute/path/to/herdr-pr-board", "--claude-reviewer"]
prompt_file = ""
skill_file = "reviews/product/SKILL.md"

[[repositories]]
name = "owner/security-service"
reviewer = "codex-security"

[[repositories]]
name = "owner/product-app"
reviewer = "claude-product"
```

Replace the example paths and repository names.
Create the selected files before saving these profiles.
See [complete instruction examples](review-instructions.md#configure-named-profiles) for file contents.
PR Board passes arguments directly without shell interpretation.
Start reviews with `n` in the panel or the existing `--review` command.
See [manual reviews](reviews.md) for launch, retry, timeout, and history behavior.

## Review preparation

The adapter retrieves the PR body and linked closing issues through GitHub CLI.
Missing or inaccessible specification evidence blocks the default review.
Custom reviews require only evidence demanded by their selected instructions.
The adapter verifies the captured head, target branch, and base before preparing the checkout.
A changed revision produces a blocked result.

The adapter creates a temporary checkout inside the review run directory.
It fetches the captured commits and checks out the captured head.
It supplies the selected skill, PR evidence, comparison diff, and commit log through standard input.
The comparison uses the merge base of the captured base and head.
Each captured Git command output has a four MiB limit.
Oversized diff or log output blocks the review.
The adapter removes its temporary checkout after the run.

Repository standards, PR text, and tool output remain review evidence.
The prompt prohibits source changes, setup scripts, and GitHub publication.
A bare agent command does not satisfy the reviewer contract.

## Codex execution

Codex runs in ephemeral JSON mode with a read-only sandbox and no approval prompts.
The command disables automatic project instructions, hooks, plugins, skills, web search, and user configuration settings.
The checkout is explicitly untrusted, which disables its project configuration.
The selected skill arrives in the review prompt.
Codex can delegate review work within its inherited sandbox restrictions.
User-defined Codex subagent roles remain discoverable despite `--ignore-user-config`.
Those roles can change a child's instructions and model.
They cannot broaden its inherited sandbox or reenable the disabled integrations.
See the [Codex role override implementation](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/core/src/agent/role.rs).

Codex must emit one completed turn with a final agent message.
The final message must contain a valid reviewer result.
Errors, missing terminal events, extra turns, and invalid results fail the review.

## Claude Code execution

Claude Code runs in print mode with a structured JSON result schema.
Safe mode preserves authentication and disables automatic customizations.
Restricted mode confines file access to the checkout and ignores user, project, and local settings.
The command disables MCP servers and session persistence.

The allowed tool pool contains `Read`, `Glob`, `Grep`, and `Agent`.
The adapter supplies diff and log evidence without exposing a shell tool.
Subagents inherit that tool pool and can further restrict it.
See [Claude Code subagent tools](https://code.claude.com/docs/en/sub-agents#available-tools).

Claude Code must return a successful result envelope with `structured_output`.
A refusal, limit, error envelope, or missing structured result fails the review.
See [Claude Code structured output](https://code.claude.com/docs/en/headless).

## Results and diagnostics

Exit status zero alone does not establish completion.
Both adapters validate the full result contract and exact captured revision before writing the result atomically.
Malformed results cannot replace an existing result file.
Completed results require a summary and a findings array.
An empty findings array indicates a completed review without findings.

Each run retains agent diagnostics beside its normal review artifacts:

- `codex-events.jsonl` or `claude-events.jsonl` retains at most 32 MiB of standard output.
- `codex-stderr.log` or `claude-stderr.log` retains at most one MiB of diagnostics.

Claude's event file contains one JSON document.
Truncated standard output fails validation.
On macOS and Linux, cancellation first sends `SIGINT` to Codex for its graceful shutdown path.
On these platforms, cancellation first sends `SIGTERM` to Pi and Claude Code.
The runner then kills any remaining processes in the owned process group.
The review claim remains held until cleanup finishes.
The adapter allows one second for cleanup.
The review service allows three seconds for adapter cleanup.
The agent inherits the claim descriptor on macOS and Linux.
This preserves ownership if the adapter exits unexpectedly.
On Windows, the agent inherits a native claim handle.
Windows terminates the owned process tree on cancellation or unexpected adapter exit.
The runner waits for those processes before releasing its review slot.
See [Windows setup](windows.md) for supported native executables and npm launchers.
