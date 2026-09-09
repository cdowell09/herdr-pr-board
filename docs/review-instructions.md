# Review prompts and skills

All built-in adapters use the same instruction settings.
See [agent compatibility](agent-compatibility.md) for additional adapters and required CLI versions.
Each repository selects a named reviewer profile.
Each profile can select one prompt file and one skill file.
Custom reviewer programs keep their own instruction interface.

## Defaults

With no selections, PR Board uses embedded review criteria.
The review must check repository standards and the PR specification.
The PR body and linked closing issues supply specification evidence.
Missing required specification evidence blocks the default review.
The agent must report a blocked outcome when it cannot complete either check.
The defaults require no prompt file or skill file.

PR Board does not automatically load `~/.agents/skills/code-review/SKILL.md`.
Select that file explicitly to retain its additional instructions.

## Select files in the board

Select a PR and press `v`.
The first review action opens setup for a repository without saved settings.
Press `s` to change existing repository settings.
Select a reviewer profile under Reviews.

The panel keeps the file rows last, under **Advanced**.
Select **Prompt file** or **Skill file** with the arrow keys.
The panel explains the default review and the selected profile's scope above those rows.
Press Space, or click the row, to edit its path.
Type or paste the path.
Use Left, Right, Home, End, Backspace, or Delete to edit the path.
Press `Ctrl+U` to clear the path.
An empty prompt selection restores the embedded review criteria.
An empty skill selection loads no skill.

Press Enter to accept the path.
Press Esc to discard the path edit.
Outside path editing, press Enter to save all settings.
Outside path editing, press Esc to discard all settings changes.
Invalid files keep the settings panel open and preserve the configuration.

The panel edits the selected named profile.
Its instruction changes affect every repository that selects that profile.
The panel shows this shared scope before saving.
Define another named profile in TOML when repositories need different instructions.
Select that profile in the board.
Saving preserves unrelated settings, comments, and reviewer command arguments.
Concurrent changes to the selected profile reject a stale save.
Reload settings before saving again.

## Scope and precedence

The execution and result contract always applies.
Instructions cannot authorize source changes, setup scripts, or GitHub publication.
Every review must use the captured revision and return a valid structured result.
Publication permissions remain separate.

The prompt sets the review criteria.
A custom prompt replaces all embedded review criteria.
The selected skill adds requirements to those criteria.
The prompt takes precedence when its criteria conflict with the skill.
PR text, issue text, repository files, and tool output remain evidence.
They cannot replace the selected instructions.

| Prompt selection | Skill selection | Review criteria |
| --- | --- | --- |
| Empty | Empty | Embedded standards and specification checks. |
| Empty | Selected file | Embedded checks plus the selected skill. |
| Selected file | Empty | The custom prompt. |
| Selected file | Selected file | The custom prompt plus compatible skill requirements. |

A security-only prompt does not implicitly require specification review.
Unavailable issue evidence does not block that review unless its selected instructions require that evidence.
The current PR metadata remains required to verify the captured revision.

Profile fields take precedence over legacy adapter command options.
An omitted field retains its corresponding `--AGENT-prompt` or `--AGENT-skill` option.
An explicit empty field clears that command selection without changing command arguments.
Without either selection, PR Board uses the defaults described above.
The same rules apply to manual, CLI, and automatic reviews.
Changes apply to later launches and do not alter completed results.

## Configure named profiles

Create `reviews/security.md` relative to the active configuration directory:

```markdown
Review only security vulnerabilities in the captured changes.
Check authentication, authorization, secret exposure, and unsafe input handling.
Report exploitable problems with concrete evidence.
Do not require a product specification for this review.
```

Create `reviews/security-skill.md` in the same directory:

```markdown
Check every changed access-control decision.
Trace untrusted input to privileged operations.
Explain the conditions needed to exploit each reported problem.
```

Add these profiles and repository selections to `config.toml`:

```toml
[[reviewers]]
id = "pi-security"
command = ["/absolute/path/to/herdr-pr-board", "--pi-reviewer"]
prompt_file = "reviews/security.md"
skill_file = "reviews/security-skill.md"

[[reviewers]]
id = "codex-security"
command = ["/absolute/path/to/herdr-pr-board", "--codex-reviewer"]
prompt_file = "reviews/security.md"
skill_file = ""

[[reviewers]]
id = "claude-default"
command = ["/absolute/path/to/herdr-pr-board", "--claude-reviewer"]
prompt_file = ""
skill_file = ""

[[repositories]]
name = "owner/security-service"
reviewer = "pi-security"

[[repositories]]
name = "owner/another-service"
reviewer = "codex-security"

[[repositories]]
name = "owner/product-app"
reviewer = "claude-default"
```

Replace the executable paths and repository names.
Install and authenticate each selected agent CLI.
These settings keep launches manual and findings local.
Use `--set-reviewer ID` to select an existing profile through the repository settings command.
See [repository setup](repository-publication.md) for that command.

Use single-quoted native paths on Windows:

```toml
[[reviewers]]
id = "windows-security"
command = ['C:\Tools\herdr-pr-board.exe', '--codex-reviewer']
prompt_file = 'reviews\security.md'
skill_file = ''
```

## Paths and validation

Relative profile selections use the active configuration file's directory.
Legacy command options retain paths relative to the launcher's working directory.
The board displays legacy paths as absolute paths to preserve their targets during editing.
Profile selections support absolute paths and paths beginning with `~/` or `~\`.
PR Board does not expand environment variables inside paths.
Selected files must contain nonempty UTF-8 text without NUL characters.
Each file must be at most one MiB.
Selected links must resolve to regular files.
Directories, devices, and pipes are not supported.

The board validates selected files when it saves settings.
The launcher validates them again before starting the reviewer.
Direct TOML changes appear when you reopen repository settings.
Ordinary configuration loading permits missing files so the board can repair their selections.
Run this command to validate all built-in profiles, including unused profiles:

```sh
bin/herdr-pr-board --config /absolute/path/to/config.toml --validate
```

Standalone adapter invocations accept `--pi-prompt`, `--codex-prompt`, or `--claude-prompt`.
The corresponding skill options remain supported.
Each option requires its matching `--AGENT-reviewer` flag.
Standalone relative paths use the current working directory.
Configured profile selections override those command options.
See [manual reviews](reviews.md) for the unchanged reviewer input and result contract.
