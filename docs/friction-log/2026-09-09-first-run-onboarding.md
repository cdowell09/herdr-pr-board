# Friction Log: PR Board first-run onboarding

Size: L
Audience: PR Board maintainers
Persona: Developer new to PR Board. Has Herdr 0.8.2 and `gh` installed. Uses one agent CLI (Claude Code). Comfortable in a terminal. Wants a PR dashboard first, agent reviews second.
Goal: Install PR Board, open the board, see my PRs, run one agent review on one PR, then decide about automation.
Summary: The dashboard path is fast and safe. The review path front-loads advanced options and automation state before the user has run one review. The reviewer default ignores which agent CLI is installed. No version signal exists anywhere.

Evidence base: v0.6.0 build (`e633d4f`), fresh `HERDR_PLUGIN_CONFIG_DIR` and `HERDR_PLUGIN_STATE_DIR` under a scratch directory, macOS, 2026-09-09. Items marked `[code]` come from reading source. Items marked `[run]` come from executing the binary.

## Journey

1. Read README "Install and open". Expected: install command. Got: prerequisite list first (Herdr, Git, `gh`, Go 1.24+). Go toolchain is a surprise for a dashboard.
2. Ran `gh auth login`, `herdr plugin install cdowell09/herdr-pr-board`, `herdr plugin action invoke open --plugin cdowell09.pr-board`. Expected: short open command. Got: 60-character command with plugin id repeated.
3. Board opened. `config.toml` auto-created. Three views appeared within seconds. Expected and got. [run]
4. "Opened by me" view showed `No pull requests in this view.` Expected: hint about scopes or next key. Got: bare line. [run]
5. Footer showed ` · updated now · Search 24/30 · GraphQL 5000/5000` with a leading separator. [run]
6. Pressed `E` to edit config. Fallback editor is `vi` when `$EDITOR` unset. No notice of which editor will open. [code]
7. Selected a PR, pressed `v`. Repository setup opened automatically. Good. [code]
8. Setup panel header read `Waiting: enable automatic launches`. Expected: ready state for a manual review. Got: automation wait reason. [code, test `TestSetupSummariesDoNotConfusePermissionWithScheduling`]
9. Reviewer row defaulted to `pi`. I have `claude`. Space cycles through 7 reviewers one at a time. No indicator of which are installed. [code]
10. Rows 2 and 3 were `Prompt file` and `Skill file`. Automatic launches and permissions came after. [code]
11. Panel bottom showed `Monitor: stopped`, then `Run in another terminal:` and a 4-line quoted shell command. This block is the fallback for a failed background monitor start. It renders whenever no monitor runs, including before automation is enabled. [code]
12. Pressed Enter. Message: `Repository settings saved. Press n to run a review.` Clear. [code]
13. Pressed `n`. Review launched with `pi` which is not installed. Failure appears as a failed run with a diagnostics directory path. [code; could not reproduce end to end, no open PR available]
14. Wanted to confirm installed version. No `--version` flag. `herdr plugin list` shows no version for a local link. Needed `go version -m bin/herdr-pr-board`. [run]
15. Simulated bad `GH_TOKEN`. Error included the raw 401 JSON body with escaped `\r\n` plus a good hint naming the variable. [run]

## Highlights

- Zero-edit first board. Default `user:@me` scope and three views work with no configuration. [run]
- Safe defaults. Manual launches, local findings, and separate per-action publish permissions. Roadmap intent is visible in the UI. [code]
- Setup auto-opens on first `v` and the save message names the next key. [code]
- Env token override hint names the exact variable that shadows the keyring login. [run]
- Config survives reinstall and uninstall. The lifecycle table in `docs/troubleshooting.md` is precise. [docs]
- Rate limits visible in the footer from the first refresh. [run]
- Monitor command is quoted per argument and wraps safely. Careful engineering, wrong moment. [code]

## Friction Points

| # | Moment | Observation | Impact | Evidence | Possible fix |
| --- | --- | --- | --- | --- | --- |
| 1 | Setup: reviewer row | Default reviewer is `pi`, first of seven builtins. No PATH check for any agent executable. Only `gh` is checked. | User saves a reviewer they do not have. First review fails. Trust drops on first try. | `internal/config/builtin_reviewers.go:12-20`; `cmd/herdr-pr-board/main.go:103` only `LookPath("gh")`; `repository_panel.go` `newRepositorySetup` picks `s.reviewers[0]` | Probe each builtin executable with `exec.LookPath` when setup opens. Default to the first installed reviewer. Label missing ones `(not installed)`. Show one-line install hint when none found. |
| 2 | Setup: header | Header reads `Waiting: enable automatic launches` for a repository that will only run manual reviews. | Reads as a blocker. User thinks manual review needs automation. | `monitor_status.go` `automaticSetupWait` first case; `repository_layout.go` `repositoryViewport` | When `AutoLaunch` is off, show `Manual reviews ready. Save, then press n.` Reserve `Waiting:` for automation-enabled repositories. |
| 3 | Setup: monitor section | The panel renders `Monitor: stopped`, then `Run in another terminal:` and the full multi-line `env HERDR_PLUGIN_STATE_DIR=... --monitor --config ...` block whenever no monitor process holds the lock. This is the fallback for a failed background start. It also renders on a fresh repository before the user enables anything. | The first setup screen ends with a shell command the user is not meant to run yet. | `repository_layout.go` `repositoryContent` monitor section; `monitor_status.go` `monitorCommandLines` returns lines when `State == Stopped`; `onboarding_test.go` asserts the command is reachable | Render the command only when automatic launches are on and background startup failed or the monitor stopped after running. Keep the Automation section in the review panel. |
| 4 | Setup: row order | `Prompt file` and `Skill file` are rows 2 and 3. Automatic launches, permissions, and after-review come later. | Advanced options before essentials. New user reads two rows of instruction text before reaching what they need. | `repository_panel.go` row constants | Reorder: Reviewer, Automatic launches, permissions, After review, then Prompt file, Skill file under an `Advanced` heading. |
| 5 | Setup: reviewer picker | Space and arrows cycle one reviewer at a time. No visible list or count. | Seven presses to see all options. User cannot tell how many exist. | `repository_panel.go` `toggle` reviewer case | Show `Reviewer: claude (3/7)` or expand the row into a list with `[x]` marker and install status. |
| 6 | Board: empty view | `No pull requests in this view.` with no next step. | New user with no open PRs sees an empty board and no path forward. | `model.go` `renderTable`; [run] frame | Contextual empty state per view: `No open PRs authored by you. Tab: next view. E: add scopes in config.toml.` |
| 7 | Board: footer | Meta line starts with ` · updated now` when no review jobs exist. | Looks like a rendering bug on the first screen. | `model.go` `renderFooter` appends ` · ` to empty `meta`; [run] frame | Join meta parts with a separator instead of prefixing each part. |
| 8 | Any: version | No `--version` flag. No version in the board title or footer. `herdr plugin list` shows no version for local links. | User cannot answer "am I on the latest release". Upgrade requires reinstall with no signal that one exists. | `options.go` flag set; `go version -m` needed today | Add `--version` printing manifest version plus `vcs.revision`. Show `PR Board v0.6.0` in the title bar. |
| 9 | Install: prerequisites | Herdr plugins build from source on the user machine, so Go 1.24+ is a prerequisite. This is the Herdr plugin model, not a PR Board choice. | Heaviest prerequisite in the flow. A user without Go sees a build failure inside `herdr plugin install`. | README line 16; `herdr-plugin.toml` `[[build]]` | Explain in README why Go is required. Keep the requirement line first. No prebuilt binary work. |
| 10 | Board: unauthenticated `gh` | Error embeds the raw 401 JSON body with escaped `\r\n`. When no env token is set, no `gh auth login` hint. | Wall of text. The fix is one command and is not named. | [run] `search "Opened by me": non-200 OK status code: 401 Unauthorized body: "{\r\n \"message\": \"Bad credentials\" ...` | On auth error, replace body with `GitHub authentication failed. Run: gh auth login`. Keep the env token hint when a variable is set. |
| 11 | Board: `E` edit | Fallback editor is `vi` with no notice. | Users without `$EDITOR` land in `vi` unexpectedly. | `model.go:894-896`; `docs/troubleshooting.md` "Edit configuration" | Footer message before launch: `Opening config in vi. Set $VISUAL or $EDITOR to change.` |
| 12 | Board and review panel: help | Footer lists 12 key pairs. Review panel help is one line of 10 items. No `?` overlay. | Dense. New users skim past `v` and `E`. | `model.go` `keyHelp`; `review_layout.go:186` | Show the five most useful keys in the footer plus `? help`. Add a `?` overlay with the full list. |
| 13 | CLI: `--repository-settings` | Output JSON uses Go field names: `{"AutoPublish":"","Name":...}`. Every other JSON surface uses snake_case. Written TOML has a double blank line and no space after commas in `command`. | Inconsistent for scripts. Cosmetic TOML diff noise. | [run] output; `publication.go:163` `json.NewEncoder(stdout).Encode(repo)` | Add JSON tags matching the snapshot contract. Normalize TOML spacing on write. |
| 14 | Install: open command | `herdr plugin action invoke open --plugin cdowell09.pr-board` is the documented way to open the board. The Herdr UI and palette do not expose plugin actions today. | Long to type once. Nit. | README line 32; maintainer confirmed Herdr behavior | Optional: document a shell alias. Low priority. May not fix. |
| 15 | Review: closed PR | `--review` on a closed PR returns `capture current PR revision: PR must be open for review`. | Acceptable. Terse but correct. | [run] | None. Recorded for completeness. |

## Stream

Install. README lists four prerequisites before the first command. Go toolchain for a TUI plugin. Herdr constraint. Ours to explain, not to remove. Three commands to open. The third one is long.

First board. Fast. Config appeared in the config dir with sane defaults. `user:@me` scope means "All open" works with zero edits. Empty "Opened by me" view has one dim line and nothing else. Footer meta starts with a separator. Rate limits visible, nice.

Config edit. `E` works. `vi` fallback. Fine for me, not for everyone.

Review setup. `v` on an unconfigured repo opens setup automatically. Good. Then the header says "Waiting: enable automatic launches". I do not want automatic launches. I want one review. Reviewer says `pi`. I have `claude`. Space, Space, Space. No count. Prompt file and Skill file rows come before the things I care about. Bottom of panel: monitor stopped, run this four-line command in another terminal. I did not ask for a monitor.

Save. "Repository settings saved. Press n to run a review." Clear. Best line in the flow.

First review. Would have launched `pi`. Not installed. Would fail with a diagnostics path. Could not reproduce end to end today, no open PR in any repo I author.

Version. Wanted to check I was on 0.6.0. No flag. Used `go version -m`. Nobody will do that.

Auth failure. Set a bad `GH_TOKEN`. Hint names the variable, good. Raw JSON body with `\r\n` escapes, bad. No `gh auth login` in the message.

CLI settings. Output has PascalCase keys. TOML has double blank line.

## Evidence

- Build identity: `go version -m bin/herdr-pr-board` shows `vcs.revision=e633d4f01b00e4b6fa0cf1a826e32d92dfecdf16`, `vcs.modified=false`.
- Fresh run frame (100x30 pty): `Pull Requests` / `No pull requests in this view.` / ` · updated now · Search 24/30 · GraphQL 5000/5000`.
- Bad token run: `herdr-pr-board: gh: Bad credentials (HTTP 401); GH_TOKEN is set and overrides the gh keyring login; unset it or replace it with a valid token` followed by the search error with the embedded JSON body.
- Missing `gh` run: `herdr-pr-board: GitHub CLI (gh) is required and must be on PATH`.
- `--repository-settings` output: `{"AutoPublish":"","Name":"cdowell09/herdr-pr-board","Reviewer":"pi","AutoLaunch":false,"PublishActions":null}`.
- Closed PR review: `herdr-pr-board: capture current PR revision: PR must be open for review`.
- Source: `internal/config/builtin_reviewers.go`, `internal/board/repository_panel.go`, `internal/board/repository_layout.go`, `internal/board/monitor_status.go`, `internal/board/model.go`, `cmd/herdr-pr-board/options.go`, `cmd/herdr-pr-board/publication.go`.
- Docs: `README.md` lines 14-36 and 84-101, `docs/troubleshooting.md` lifecycle section, `docs/automatic-reviews.md`.
- `[screenshot needed]` Setup panel on a fresh repository at 100x30.
- `[log needed]` Failed first review with a missing agent CLI: run status and diagnostics text as shown in the review panel.

## Follow-Up

- Detect installed agent CLIs in repository setup and default to one that exists. Priority: high. Friction 1. Issue: #123.
- Show a manual-review-ready header and hide monitor command until automation is enabled. Priority: high. Friction 2, 3. Issue: #110, #111.
- Reorder setup rows and group prompt and skill files under Advanced. Priority: medium. Friction 4. Issue: #112.
- Show reviewer position or a list in the reviewer row. Priority: medium. Friction 5. Issue: #113.
- Add contextual empty states per view. Priority: medium. Friction 6. Issue: #114.
- Fix footer leading separator. Priority: low. Friction 7. Issue: #115.
- Add `--version` and show the version in the title bar. Priority: medium. Friction 8. Issue: #116.
- Replace raw 401 body with a `gh auth login` hint. Priority: medium. Friction 10. Issue: #118.
- Announce the editor before `E` launches it. Priority: low. Friction 11. Issue: #119.
- Add `?` help overlay and trim the footer to the top keys. Priority: medium. Friction 12. Issue: #120.
- Add JSON tags to `--repository-settings` output and normalize TOML spacing. Priority: low. Friction 13. Issue: #121, #122.
- Explain the Go prerequisite in README. Priority: low. Friction 9. Herdr constraint. Issue: #117.
- Optional shell alias for the open command. Priority: low. May not fix. Friction 14. Herdr does not expose actions in its UI. No issue filed.
- Open question: capture a real failed first review with a missing CLI on an open PR and attach the panel text. Friction 1 evidence.
