# Additional agent compatibility

This assessment covers the additional Herdr agents requested in issues #81 through #94.
The assessment date is 2026-09-08.
Herdr detection identifies an installed program.
Detection does not establish a compatible review contract.

A built-in adapter must preserve native authentication and suppress unselected instructions, skills, hooks, plugins, and MCP servers.
It must distinguish a completed response from errors, interruption, and partial output.
All adapters must use the shared captured checkout, selected instructions, result validation, and process cleanup.
See [review instructions](review-instructions.md) for prompt and skill selection.
See [manual reviews](reviews.md) for launch, cancellation, history, and publication.

## Available adapters

| Agent | Verified CLI version | Executable | Reviewer flag |
| --- | --- | --- | --- |
| Oh My Pi | Exactly 18.1.14 | `omp` | `--omp-reviewer` |
| Kimi | 1.50.0 | `kimi` | `--kimi-reviewer` |
| Qoder CLI | 1.1.47 | `qodercli` | `--qodercli-reviewer` |
| Qwen Code | 0.23.0 | `qwen` | `--qwen-reviewer` |

Use at least the verified version, except for Oh My Pi.
The Oh My Pi adapter rejects other versions before launching a review.
Its isolation settings depend on that version's complete discovery-provider list.
Unsupported options or output fail the review.
PR Board does not retry with weaker isolation.

Authenticate the selected CLI before starting a review.
The adapters do not read or copy credential files.
Follow the selected program's installation and authentication guide:

- Oh My Pi: [installation](https://github.com/can1357/oh-my-pi/tree/v18.1.14#install) and [provider authentication](https://github.com/can1357/oh-my-pi/blob/v18.1.14/docs/providers.md).
- Kimi Code CLI: [installation and login](https://moonshotai.github.io/kimi-cli/en/guides/getting-started.html).
- Qoder CLI: [installation](https://docs.qoder.com/cli/installation) and [authentication](https://docs.qoder.com/cli/authentication).
- Qwen Code: [installation and authentication](https://qwenlm.github.io/qwen-code-docs/en/users/quickstart/).

Install Oh My Pi version 18.1.14.
For Kimi, install the `kimi-cli` distribution.
The separate successor `kimi-code` distribution is not part of this assessment.

Use these pinned package commands:

```sh
bun install -g @oh-my-pi/pi-coding-agent@18.1.14
uv tool install --python 3.13 kimi-cli==1.50.0
```

On Windows, use `omp-windows-x64.exe` from the [18.1.14 release](https://github.com/can1357/oh-my-pi/releases/tag/v18.1.14).
Select that executable with `--omp-executable` if its installed name is not `omp.exe`.
PR Board does not translate Bun command launchers on Windows.

Kimi uses its native Kimi Code OAuth login and `kimi-for-coding` model.
The Kimi adapter does not import custom providers from the user's configuration.
Qoder uses its native Qoder account authentication.
The Qoder adapter does not import custom provider settings.
Qwen and Oh My Pi retain their native configured authentication.

Select these agents in repository settings.
Alternatively, use a setup command:

```sh
bin/herdr-pr-board --repository-settings owner/repository --use-qwen-reviewer
```

Replace `qwen` with `omp`, `kimi`, or `qodercli` for another adapter.
Use `--set-reviewer ID` for an existing named profile.
Each adapter accepts matching `--AGENT-executable`, `--AGENT-prompt`, and `--AGENT-skill` options.
Replace `AGENT` with the executable name from the table.
Each option requires its matching reviewer flag.
Use profile fields for normal board configuration:

```toml
[[reviewers]]
id = "qwen-security"
command = ["/absolute/path/to/herdr-pr-board", "--qwen-reviewer"]
prompt_file = "reviews/security.md"
skill_file = ""

[[repositories]]
name = "owner/repository"
reviewer = "qwen-security"
```

Create the selected prompt file before saving this profile.
An empty prompt selection uses the embedded standards and specification criteria.
An empty skill selection adds no skill.
Custom prompts replace the embedded criteria.
These choices do not change revision checks or publication permissions.

### Oh My Pi

The adapter uses `--print --mode json` and an isolated settings overlay.
It disables every discovery provider in 18.1.14.
It also disables extensions, skills, rules, memory, personality files, and auxiliary integrations.
Only read, grep, and glob tools remain available.
The overlay preserves native authentication.
The adapter accepts only a terminal `agent_end` event with a final assistant `stop` response.
Intermediate agent events do not complete a review.

The contract comes from the [18.1.14 source](https://github.com/can1357/oh-my-pi/tree/v18.1.14).
Regression tests cover the exact version guard, terminal events, malformed output, and isolation settings.

### Kimi

The adapter uses `--wire --afk` with generated configuration and a generated agent definition.
The agent has read, glob, and grep tools.
The generated files exclude ambient hooks, skills, MCP servers, and inherited agent instructions.
Kimi resolves its normal `oauth/kimi-code` login reference.
PR Board keeps Wire input open until the prompt response arrives.
Only a matching `finished` response completes the review.
The final assistant step must contain text and no pending tool activity.

The contract comes from the [1.50.0 source](https://github.com/MoonshotAI/kimi-cli/tree/1.50.0).
An offline run uses Kimi's native echo provider to verify the Wire envelope.
The sanitized transcript is a regression fixture.
Tests also cover chunked output, cancellation, early exit, and blocked input cleanup.

### Qoder CLI

The adapter uses print mode with one JSON result.
Empty setting sources exclude user and repository instruction discovery.
Explicit settings disable hooks, skills, memory, and auxiliary actions.
Strict empty MCP configuration excludes inherited servers.
Only Read, Grep, and Glob tools remain available.
The adapter accepts a successful result with an `end_turn` or `stop_sequence` stop reason.
Errors, truncation, and tool-use stops fail the review.

The contract comes from the [official 1.1.47 package](https://www.npmjs.com/package/@qoder-ai/qodercli/v/1.1.47).
See the [settings reference](https://docs.qoder.com/cli/settings-reference) and [SDK skills documentation](https://docs.qoder.com/cli/sdk/skills).
Verification uses the published bundle, isolated help output, and parser regression tests.

### Qwen Code

The adapter uses `--safe-mode` and JSON schema output.
Safe mode excludes ambient instructions, extensions, hooks, skills, subagents, memory, and local MCP servers.
Safe mode retains native authentication.
Default noninteractive permissions restrict command execution and file changes.
The adapter accepts only the final successful root result and its structured result object.
Nested results and intermediate messages cannot complete a review.

See [headless mode](https://qwenlm.github.io/qwen-code-docs/en/users/features/headless/).
The contract comes from the [0.23.0 source](https://github.com/QwenLM/qwen-code/tree/v0.23.0).
Regression tests cover isolation options, nested results, failures, and malformed structured output.

## Assessed agents without a built-in adapter

These findings apply to the listed versions or source revisions.
They do not claim that the programs lack headless operation.
No paid model request is part of this assessment.

### GitHub Copilot CLI 1.0.83

Copilot supports unattended prompts and JSONL output.
Instruction suppression and named MCP exclusions are available.
Skill and plugin exclusions use names.
An offline discovery test confirms that `disabledSkills: ["*"]` does not exclude an installed personal skill.
An exact skill name does exclude it.
An empty `COPILOT_HOME` also replaces stored authentication configuration.
Machine policy hooks remain active despite `disableAllHooks`.

Support requires a verified invocation that excludes all ambient customization while preserving native login.
No reliable discovery-and-name override procedure is established here.

Evidence: [release 1.0.83](https://github.com/github/copilot-cli/releases/tag/v1.0.83), [CLI reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference),
[configuration reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference), and [hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference).
The official release checksum validates the binary used for offline discovery.

### Devin 3000.6.14

Devin provides a local CLI with print mode, prompt files, and ACP.
`--config` replaces the default user configuration file.
Import controls include `agents_standard: false` and third-party sources.
However, native project hooks remain separate automatic discovery sources.
These include `.devin/hooks.v1.json` and project configuration files.
The inspected help and configuration reference expose no complete native hook exclusion for one invocation.

Support requires a native per-run exclusion for project hooks and other integrations while preserving native login.

Evidence: [commands](https://docs.devin.ai/cli/reference/commands),
[configuration file](https://docs.devin.ai/cli/reference/configuration/config-file),
[import controls](https://docs.devin.ai/cli/reference/configuration/read-config-from),
and [hook locations](https://docs.devin.ai/cli/extensibility/hooks/overview#where-hooks-live).
The [official release manifest](https://static.devin.ai/cli/current/manifest.json) supplies the inspected binary and checksum.
Native help verification uses temporary configuration and requires no login.

### Factory Droid 0.213.0

`droid exec -o json` provides unattended execution.
The global `--settings PATH` option applies a runtime settings overlay.
However, `--disable-builtin-skills` retains user, project, plugin, and dynamic skills.
The `disabledSkills` setting uses exact skill names.
Startup and file-read discovery also inject project and personal instruction files.
Tool restrictions and an empty settings overlay do not exclude that context.

Support requires a native per-run source exclusion that preserves native login.

Evidence: [skill controls](https://docs.factory.ai/droid-exec/overview#skill-controls),
[disabled skills](https://docs.factory.ai/droid-cli/settings#disabled-skills),
[CLI reference](https://docs.factory.ai/droid-cli/cli-reference),
and [automatic instructions](https://docs.factory.ai/harness/agents-md).
Verification inspects the [official 0.213.0 package](https://registry.npmjs.org/@factory/cli-darwin-arm64/-/cli-darwin-arm64-0.213.0.tgz).
Package integrity verification precedes isolated native help checks.
The published code confirms exact-name exclusions and separate instruction discovery.

### OpenCode 1.18.29

OpenCode supports unattended JSON events and native stored authentication.
`OPENCODE_PURE` excludes external plugins.
However, stored `wellknown` authentication entries also load remote configuration.
This occurs before project configuration controls and explicit overlays.
Instruction arrays concatenate, so an empty overlay cannot clear inherited instructions.
Inherited MCP entries also remain and initialize unless each entry is disabled by name.
Changing the configuration directory does not exclude this authenticated remote source.

Support requires a native control that excludes remote instructions and MCP initialization while retaining authentication.

Evidence: [CLI reference](https://opencode.ai/docs/cli/),
[configuration loading](https://github.com/anomalyco/opencode/blob/v1.18.29/packages/opencode/src/config/config.ts#L373),
[plugin exclusion](https://github.com/anomalyco/opencode/blob/v1.18.29/packages/opencode/src/plugin/index.ts#L181),
and [MCP initialization](https://github.com/anomalyco/opencode/blob/v1.18.29/packages/opencode/src/mcp/index.ts#L496).

### Kilo 7.5.16

`kilo run --format json` provides unattended execution.
`KILO_PURE` excludes external plugins.
Stored `wellknown` authentication entries still load remote configuration before local configuration controls.
Remote instruction arrays concatenate with later arrays.
Remote MCP maps also survive empty overlays.
MCP initialization has no PURE exclusion.

Support requires a native control that excludes authenticated remote instructions and MCP initialization while retaining authentication.

Evidence: [CLI documentation](https://kilo.ai/docs/code-with-ai/platforms/cli),
[remote configuration](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/config/config.ts#L600),
[array merging](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/config/config.ts#L72),
[MCP initialization](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/mcp/index.ts#L530),
and [plugin exclusion](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/plugin/index.ts#L189).

### Hermes 2026.9.7

Hermes provides two unattended paths with different contracts.
Top-level `-z --usage-file` exports explicit `completed` and `failed` fields.
However, this path constructs an agent without the context and memory exclusion arguments.
Those arguments default to false.
The safe-mode environment does not change those constructor defaults.

The alternative `chat --safe-mode --cli -Q --query-file -` passes the exclusion arguments correctly.
However, its exit status checks `failed` without requiring `completed` or excluding `partial`.
The usage report applies only to top-level `-z`.
Thus neither inspected path provides both required isolation and explicit completion evidence.

Support requires the usage-report path to honor context and memory exclusions.
Alternatively, the isolated chat path must expose a terminal completion result.

Evidence: [one-shot construction and usage report](https://github.com/NousResearch/hermes-agent/blob/v2026.9.7/hermes_cli/oneshot.py),
[constructor defaults](https://github.com/NousResearch/hermes-agent/blob/v2026.9.7/agent/agent_init.py#L2204),
[chat exclusion arguments](https://github.com/NousResearch/hermes-agent/blob/v2026.9.7/hermes_cli/cli_agent_setup_mixin.py#L543),
and [quiet chat exit handling](https://github.com/NousResearch/hermes-agent/blob/v2026.9.7/cli.py#L4056).

### Cursor Agent 2026.09.02-c22c1a3

The executable is `agent`, with `cursor-agent` retained as an alias.
Print mode supports JSON and streaming JSON.
The inspected native help exposes no complete customization exclusion.
`CURSOR_CONFIG_DIR` changes CLI configuration discovery.
However, the published hook loader still reads user hooks from the native home directory.
The authentication store also uses the native home directory on macOS.
Changing that home replaces the normal stored login location.

Support requires a native isolation control that excludes hooks and other ambient customization while preserving login.

Evidence: [CLI parameters](https://cursor.com/docs/cli/reference/parameters),
[configuration paths](https://cursor.com/docs/cli/reference/configuration), and [hooks](https://cursor.com/docs/hooks).
Verification also inspects the [official pinned release archive](https://downloads.cursor.com/lab/2026.09.02-c22c1a3/darwin/arm64/agent-cli-package.tar.gz).
The archive's `190.index.js` defines hook paths independently of `CURSOR_CONFIG_DIR`.
Its `index.js` defines the separate authentication path.
Help verification uses temporary configuration, home, and cache directories.

### Mastra Code 0.38.1-alpha.11

This assessment uses source revision `a968c22817f88fc7f9b0d956c5309b034a7abad1`.
The native CLI supports headless execution and a settings file.
The SDK exposes `disableHooks`, `disableMcp`, `disablePlugins`, and `configDir`.
However, the native headless command passes only `settingsPath` to `createMastraCode`.
The default plugin manager then reloads plugins.
The TUI's environment-based exclusions do not establish equivalent headless exclusions.

Support requires the native headless command to expose the SDK's isolation controls while preserving native authentication.

Evidence: [headless construction](https://github.com/mastra-ai/mastra/blob/a968c22817f88fc7f9b0d956c5309b034a7abad1/mastracode/sdk/src/headless/cli.ts#L202),
[plugin construction](https://github.com/mastra-ai/mastra/blob/a968c22817f88fc7f9b0d956c5309b034a7abad1/mastracode/sdk/src/index.ts#L665),
and [TUI controls](https://github.com/mastra-ai/mastra/blob/a968c22817f88fc7f9b0d956c5309b034a7abad1/mastracode/tui/src/main.ts).

### Google Antigravity CLI 1.1.27

This assessment covers Google's native Antigravity CLI.
Print mode supports JSON, streaming JSON, and JSON schema output.
`--disable-slash-commands` disables slash-command and skill expansion in print mode.
It does not establish complete customization isolation.
The 1.1.25 changelog enables ambient skill, rule, and subagent inheritance for Markdown agents.
The documented custom-agent fields do not establish a complete inheritance and hook exclusion.

Support requires a verified native invocation that excludes inherited customization while preserving normal authentication.

Evidence: [headless mode](https://antigravity.google/docs/cli/headless),
[custom agents](https://antigravity.google/docs/subagents/),
and the [CLI changelog](https://github.com/google-antigravity/antigravity-cli/blob/1.1.27/CHANGELOG.md).
Verification includes help output from the official 1.1.27 binary with temporary home and cache directories.

### Grok Build

The public changelog lists 1.0.13 during this assessment.
Source inspection uses revision `72a61251fcffb464bcc687aeb5a998e5a98ec0c9`.
The CLI supports unattended prompts, JSON output, and native login.
`GROK_CONFIG` and `GROK_CONFIG_PATH` provide configuration overlays.
However, hook configuration layers combine additively outside ordinary configuration merging.
Root-owned policy hooks cannot be disabled by user settings.
The inspected CLI does not expose a complete isolation mode.

Support requires a native invocation that excludes ambient customization while retaining native authentication.

Evidence: [changelog](https://x.ai/build/changelog), [headless scripting](https://docs.x.ai/build/cli/headless-scripting),
[configuration overlay](https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-config/src/env_overlay.rs),
and [hook layer policy](https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-config/src/loader.rs#L210).
