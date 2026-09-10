# Additional agent compatibility

This assessment covers the additional Herdr agents requested in issues #81 through #94.
The assessment date is 2026-09-09.
Herdr detection identifies an installed program.
Detection does not establish a compatible review contract.

A built-in adapter must preserve native authentication and suppress unselected instructions, skills, hooks, plugins, and MCP servers.
It must distinguish a completed response from errors, interruption, and partial output.
All adapters must use the shared captured checkout, selected instructions, result validation, and process cleanup.
See [review instructions](review-instructions.md) for prompt and skill selection.
See [manual reviews](reviews.md) for launch, cancellation, history, and publication.

## Available adapters

| Agent | Verified native version | Executable or SDK | Reviewer flag |
| --- | --- | --- | --- |
| Oh My Pi | Exactly 18.1.14 | `omp` | `--omp-reviewer` |
| Kimi | 1.50.0 | `kimi` | `--kimi-reviewer` |
| Qoder CLI | 1.1.47 | `qodercli` | `--qodercli-reviewer` |
| Qwen Code | 0.23.0 | `qwen` | `--qwen-reviewer` |
| GitHub Copilot CLI | Exactly 1.0.83 | `copilot` | `--copilot-reviewer` |
| Mastra Code | Exactly 0.39.0 | `mastracode` | `--mastracode-reviewer` |
| Hermes | Exactly release 2026.9.7 | Hermes environment Python | `--hermes-reviewer` |
| Cursor | Exactly SDK 1.0.31 | `@cursor/sdk` | `--cursor-reviewer` |
| Google Antigravity CLI | Exactly 1.1.28 | `agy` | `--antigravity-reviewer` |
| Grok Build | Exactly 1.0.24 | `grok` | `--grok-reviewer` |

Use exact versions where the table says `Exactly`.
Other listed versions are minimum verified versions.
Version guards protect isolation controls and terminal protocols that depend on the inspected release.
Unsupported options or output fail the review.
PR Board does not retry with weaker isolation.

Authenticate the selected native CLI or SDK before starting a review.
The adapters do not read or copy credential files.
Follow the selected program's installation and authentication guide:

- Oh My Pi: [installation](https://github.com/can1357/oh-my-pi/tree/v18.1.14#install) and [provider authentication](https://github.com/can1357/oh-my-pi/blob/v18.1.14/docs/providers.md).
- Kimi Code CLI: [installation and login](https://moonshotai.github.io/kimi-cli/en/guides/getting-started.html).
- Qoder CLI: [installation](https://docs.qoder.com/cli/installation) and [authentication](https://docs.qoder.com/cli/authentication).
- Qwen Code: [installation and authentication](https://qwenlm.github.io/qwen-code-docs/en/users/quickstart/).
- GitHub Copilot CLI: [installation and login](https://github.com/github/copilot-cli).
- Mastra Code: [official package](https://www.npmjs.com/package/mastracode/v/0.39.0).
- Hermes: [installation](https://hermes-agent.nousresearch.com/docs/getting-started/installation).
- Cursor: [SDK installation and authentication](https://cursor.com/docs/sdk/typescript).
- Antigravity: [installation and authentication](https://antigravity.google/docs/cli/install/).
- Grok Build: [getting started](https://docs.x.ai/build/overview).

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
Install the agent CLI before you select it.
Setup starts at the first available reviewer and shows `not installed` for an absent agent CLI.
See [detect installed agent CLIs](reviews.md#detect-installed-agent-clis) for the PATH rules and the exceptions.
Alternatively, use a setup command:

```sh
bin/herdr-pr-board --repository-settings owner/repository --use-qwen-reviewer
```

Replace `qwen` with the selected reviewer flag prefix, such as `antigravity`.
Use `--set-reviewer ID` for an existing named profile.
Each adapter accepts matching `--AGENT-executable`, `--AGENT-prompt`, and `--AGENT-skill` options.
Replace `AGENT` with the selected reviewer flag prefix.
The Cursor executable option selects an SDK entrypoint.
The Hermes executable option selects a Python interpreter.
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

### GitHub Copilot CLI

The adapter uses the native SDK protocol through `copilot --headless --stdio`.
The native host retains its normal login.
The SDK session uses an isolated configuration directory and explicit discovery exclusions.
Only view, grep, and glob tools remain available.
The adapter excludes instructions, skills, plugins, memory, MCP servers, and host Git operations.

Machine policy hooks have no session exclusion.
The adapter refuses to run when machine policy files or registry entries exist.
It also refuses to run when it cannot inspect those locations.
Use another reviewer on a machine with required policy.

An assistant message and an idle event do not prove completion.
The adapter requires a matching provider `stop`, assistant response, turn end, and successful idle event.
Truncation, interruption, unexpected interactions, and incomplete protocol frames fail the review.

The contract uses [Copilot CLI 1.0.83](https://github.com/github/copilot-cli/releases/tag/v1.0.83) and [SDK protocol 3](https://github.com/github/copilot-sdk/blob/d8bbc9dd7a6167d4806780f405d8ce74add1cc7c/go/types.go).
Native offline probes use generated customization files and a local mock model endpoint.
They verify customization exclusion and distinguish normal completion from token truncation.

### Mastra Code

The adapter uses the installed public SDK because the CLI does not forward its isolation controls.
It requires Mastra Code 0.39.0, `@mastra/code-sdk` 1.7.0, `@mastra/core` 1.65.0, and `@mastra/memory` 1.28.3.
The adapter rejects other versions.
Use Node.js 22.19.0 or later.
Install the CLI with npm and select a model in the native CLI before starting a review.
The SDK retains native authentication storage.

Install the pinned packages in a directory for this CLI:

```sh
npm install --prefix "$HOME/.local/share/pr-board-mastracode" --save-exact --prefer-dedupe \
  mastracode@0.39.0 @mastra/code-sdk@1.7.0 @mastra/core@1.65.0 @mastra/memory@1.28.3
```

Add that directory's `node_modules/.bin` directory to `PATH` before starting Herdr.
Alternatively, add `--mastracode-executable` and the installed launcher path to the reviewer command.
On Windows, use the npm `.cmd` launcher.
PR Board resolves that launcher through Node.js.

The adapter combines isolated settings with explicit SDK exclusions for hooks, MCP servers, plugins, and background integrations.
The SDK receives an untrusted checkout and an explicit read-only workspace.
The workspace excludes skills, language servers, command execution, and file changes.
The SDK uses a fresh local thread for the review.
It disables observational memory, semantic recall, and working memory.
The adapter requires SDK completion and a normal provider stop reason.
Token limits, filtering, errors, cancellation, and unknown stop reasons fail the review.

The contract uses the official [CLI package](https://www.npmjs.com/package/mastracode/v/0.39.0)
and [SDK package](https://www.npmjs.com/package/@mastra/code-sdk/v/1.7.0).
See the pinned [SDK factory](https://github.com/mastra-ai/mastra/blob/75a962527507424f373975955c32c2756749a936/mastracode/sdk/src/index.ts)
and [headless contract](https://github.com/mastra-ai/mastra/blob/75a962527507424f373975955c32c2756749a936/mastracode/sdk/src/headless/run-mc.ts).
Native offline probes verify direct completion, read-tool completion, truncation, and unknown or missing provider stops.
Regression tests also verify large output delivery and dependency guards.

### Hermes

Install Hermes release 2026.9.7 and configure its native authentication and model.
Use Python 3.11, 3.12, or 3.13, as required by that release.
The adapter uses the installed Python interface.
The selected Python interpreter must import the installed Hermes package.
The default interpreter is `python3`.
Set `--hermes-executable` to the Hermes environment's Python path when necessary.
On Windows, select that environment's `Scripts/python.exe`.
Do not select the `hermes` CLI launcher for this option.

For example, configure a profile with the interpreter path:

```toml
[[reviewers]]
id = "hermes"
command = ["/absolute/path/to/herdr-pr-board", "--hermes-reviewer", "--hermes-executable", "/path/to/hermes/.venv/bin/python"]
```

Replace both paths with the installed paths.
Hermes resolves the configured model and authentication inside its own runtime.
The adapter does not read or copy credential files.
The adapter requires an in-process provider transport and the built-in compressor context engine.
External agent transports and MoA are not supported.

Native safe mode excludes hooks, plugins, MCP servers, and external integrations.
Explicit constructor settings exclude context files, memory, and background review.
Only file reads and searches remain available.
A separate control directory prevents automatic nested instruction discovery during captured-file reads.
Python isolated mode excludes ambient startup options and checkout imports.

A successful review requires native completion and matching final assistant text.
Failures, partial results, interruption, transformed responses, and preview responses fail the review.
The shared runner still validates the exact captured revision and review result.

The contract uses the [Python integration interface](https://hermes-agent.nousresearch.com/docs/developer-guide/programmatic-integration)
and [2026.9.7 source](https://github.com/NousResearch/hermes-agent/tree/v2026.9.7).
Native local probes verify captured-file reads, customization exclusion, and transport truncation.
Regression tests verify completion flags, cancellation, version guards, and selected instructions.

### Cursor

The adapter uses the public `@cursor/sdk` package, version 1.0.31.
Use Node.js 22.13 or later.
Install the pinned SDK:

```sh
npm install --global @cursor/sdk@1.0.31
```

Authenticate through the SDK's native browser login:

```sh
cd "$(npm root -g)/@cursor/sdk"
node --input-type=module -e 'import { Cursor } from "./dist/esm/index.js"; await Cursor.auth.login();'
```

These commands also work in PowerShell.
The SDK stores this login in `~/.cursor/sdk/auth.json`.
The SDK login is separate from the Cursor CLI and application logins.
An existing `CURSOR_API_KEY` also works through native SDK authentication.
The adapter does not read or copy either credential.

The default adapter locates the global package with `npm root -g`.
For another installation location, set `--cursor-executable` to the package's `dist/esm/index.js` file.
The adapter uses `composer-2.5` by default.
Set `CURSOR_MODEL` before starting Herdr to select another available model.

Empty SDK setting sources exclude user, project, team, machine policy, and plugin customizations.
The session permits only read, directory listing, grep, and glob tools.
It uses a fresh local store and disables agent retries.
A successful review requires SDK completion, one turn ending, and no pending tools.
Errors, cancellation, and incomplete turns fail the review.

The contract uses the [official SDK reference](https://cursor.com/docs/sdk/typescript)
and [1.0.31 package](https://www.npmjs.com/package/@cursor/sdk/v/1.0.31).
Native local probes verify nested file reads, customization exclusion, native login, cancellation, and completion failures.
A control run confirms that enabled sources discover instructions and execute generated hooks and MCP commands.

### Google Antigravity CLI

Install and authenticate Antigravity CLI 1.1.28.
The default executable is `agy`.
The adapter retains the native CLI data directory and model preference.
It uses separate customization and data directory controls.
These undocumented controls require the exact verified version.
On Windows, runtime state and the native user directory must use the same volume.

A generated agent disables customization and MCP inheritance.
The agent permits file reads and grep searches.
The native task metadata tool also remains available.
Personal, project, and legacy customization sources remain excluded.

Native `statusLine.command` and `title.command` settings execute during headless operation.
The adapter refuses to run when either command is configured.
It does not change native settings.

The adapter requires the selected agent, matching conversation, completed steps, and a successful terminal result.
It returns only the final completed assistant response.
Native timeouts can report success with incomplete text.
The adapter rejects those results and keeps the shared review timeout authoritative.

The contract uses the [headless protocol](https://antigravity.google/docs/cli/headless/)
and [1.1.28 release](https://github.com/google-antigravity/antigravity-cli/blob/1.1.28/CHANGELOG.md).
Native local probes verify generated customization exclusion and a complete source-file read.
Regression fixtures include native timeouts with empty and valid-looking partial responses.

### Grok Build

Install and authenticate Grok Build 1.0.24.
The adapter uses an isolated native configuration and an empty repository for discovery.
Read tools access the captured checkout through its absolute path.
Only file reads, grep searches, and directory listings remain available.
These tools retain Grok's native read scope.
The native runtime retains ownership of login storage through `GROK_AUTH_PATH`.
The adapter does not read or copy that storage.

A native `inspect --json` preflight checks the effective configuration before each review.
It rejects inherited configuration, managed settings, instructions, hooks, plugins, skills, MCP servers, and language servers.
It also checks compatibility-source exclusions and the exact CLI version.
The adapter refuses to run when the inspector cannot prove isolation.

A successful review requires matching initialization, final assistant text, and a successful result from one session.
Both the assistant response and the result must report `end_turn`.
Errors, partial output, unexpected events, and unfinished tools fail the review.
Grok determines completion for the upstream model stream.
PR Board validates Grok's terminal result and the captured review contract.

The contract uses the native 1.0.24 inspector and event protocol.
See [headless scripting](https://docs.x.ai/build/cli/headless-scripting)
and [CLI reference](https://docs.x.ai/build/cli/reference).
Native local probes verify stored authentication, isolated discovery, and a complete captured-file read.

## Assessed agents without a built-in adapter

These findings apply to the listed versions or source revisions.
They do not claim that the programs lack headless operation.
No paid model request is part of this assessment.

### Devin 3000.6.14

Devin provides a local CLI with print mode, prompt files, and ACP.
`--config` replaces the default user configuration file.
Import controls include `agents_standard: false` and third-party sources.
However, native project hooks remain separate automatic discovery sources.
These include `.devin/hooks.v1.json` and project configuration files.
The inspected help and configuration reference expose no complete native hook exclusion for one invocation.
An offline `rules list` probe disables all import sources and plugin discovery.
Native personal and project rules remain listed as always-on.
An empty working directory excludes the project rule but retains the personal rule.
Listing proves discovery, not active-turn filtering.

Support requires a native per-run exclusion for project hooks and other integrations while preserving native login.

Evidence: [commands](https://docs.devin.ai/cli/reference/commands),
[configuration file](https://docs.devin.ai/cli/reference/configuration/config-file),
[import controls](https://docs.devin.ai/cli/reference/configuration/read-config-from),
and [hook locations](https://docs.devin.ai/cli/extensibility/hooks/overview#where-hooks-live).
The [official release manifest](https://static.devin.ai/cli/current/manifest.json) supplies the inspected binary and checksum.
Native help verification uses temporary configuration and requires no login.

### Factory Droid 0.215.1

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
Verification inspects the [official 0.215.1 package](https://registry.npmjs.org/@factory/cli-darwin-arm64/-/cli-darwin-arm64-0.215.1.tgz).
Package integrity verification precedes isolated native help checks.
The published code confirms exact-name exclusions and separate instruction discovery.

### OpenCode 1.18.30

OpenCode supports unattended JSON events and native stored authentication.
`OPENCODE_PURE` excludes external plugins.
However, stored `wellknown` authentication entries also load remote configuration.
This occurs before project configuration controls and explicit overlays.
Instruction arrays concatenate, so an empty overlay cannot clear inherited instructions.
Inherited MCP entries also remain and initialize unless each entry is disabled by name.
Changing the configuration directory does not exclude this authenticated remote source.
An offline native probe supplies remote configuration through a local mock endpoint.
The configuration retains inherited instructions with PURE and project discovery exclusions enabled.
The native `mcp list` command also executes the inherited marker command.

Support requires a native control that excludes remote instructions and MCP initialization while retaining authentication.

Evidence: [CLI reference](https://opencode.ai/docs/cli/),
[configuration loading](https://github.com/anomalyco/opencode/blob/v1.18.30/packages/opencode/src/config/config.ts#L373),
[plugin exclusion](https://github.com/anomalyco/opencode/blob/v1.18.30/packages/opencode/src/plugin/index.ts#L181),
and [MCP initialization](https://github.com/anomalyco/opencode/blob/v1.18.30/packages/opencode/src/mcp/index.ts#L496).

### Kilo 7.5.16

`kilo run --format json` provides unattended execution.
`KILO_PURE` excludes external plugins.
Stored `wellknown` authentication entries still load remote configuration before local configuration controls.
Remote instruction arrays concatenate with later arrays.
Remote MCP maps also survive empty overlays.
MCP initialization has no PURE exclusion.
An offline native probe reproduces instruction retention under an empty configuration overlay.
The native `mcp list` command executes the inherited marker command with PURE enabled.

Support requires a native control that excludes authenticated remote instructions and MCP initialization while retaining authentication.

Evidence: [CLI documentation](https://kilo.ai/docs/code-with-ai/platforms/cli),
[remote configuration](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/config/config.ts#L600),
[array merging](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/config/config.ts#L72),
[MCP initialization](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/mcp/index.ts#L530),
and [plugin exclusion](https://github.com/Kilo-Org/kilocode/blob/v7.5.16/packages/opencode/src/plugin/index.ts#L189).
