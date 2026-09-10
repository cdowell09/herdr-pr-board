# Changelog

This file lists notable changes to PR Board.

## 0.7.0 - 2026-09-10

PR Board makes the first run faster to set up and adds six more review adapters.

### Highlights

- Open the board on Herdr 0.8 macOS and Linux. The pane now starts through the bash wrapper, so a fresh install no longer fails with `No viable candidates found in PATH`.
- Set up reviews faster. Repository setup finds installed agent CLIs and starts at one you have, puts essentials before the prompt and skill files, and shows that manual reviews are ready without monitor noise.
- Choose Copilot, Mastra Code, Hermes, Cursor, Antigravity, or Grok alongside the seven existing adapters.
- Find your way around. Press `?` for a help overlay, read tailored text and next keys on an empty view, and see the editor name before `E` opens it.
- Diagnose faster. `--version` prints the version and build revision, the title bar shows the version, and an authentication failure names `gh auth login` instead of the raw 401 body.

See [agent compatibility](https://github.com/cdowell09/herdr-pr-board/blob/v0.7.0/docs/agent-compatibility.md) for the new adapters' CLI versions, authentication, and assessed limitations.
See [reviewer detection](https://github.com/cdowell09/herdr-pr-board/blob/v0.7.0/docs/reviews.md#detect-installed-agent-clis) for the PATH rules.

### Upgrade

```sh
herdr plugin install cdowell09/herdr-pr-board --ref v0.7.0
```

Reopen the board to load the new binary.
Restart an older monitor after its active reviews finish.

<details>
<summary>Full changelog</summary>

### Bug Fixes

- Replace raw 401 body with a gh auth login hint (#136)
- Normalize repository-settings JSON keys and TOML spacing (#137)
- Start the Unix pane through the bash wrapper (#144)

### Documentation

- First-run onboarding walkthrough (#124)
- Add ADR 0002 and core user journeys (#130)
- Explain the Go prerequisite in README (#135)
- Trim README polish and refresh board screenshots (#143)

### Features

- Add Copilot and Mastra review adapters (#108)
- Add Hermes, Cursor, Antigravity, and Grok reviewers (#109)
- Invite manual reviews before automation (#134)
- Add a ? help overlay and fix the footer meta line (#138)
- Put setup essentials before advanced files (#139)
- Explain empty views and announce the editor (#141)
- Detect installed agent CLIs and show reviewer position (#140)
- Add --version and show the version in the title bar (#142)

### Testing

- Fix Windows fixture readiness races (#107)

</details>

## 0.6.0 - 2026-09-08

PR Board adds native Windows support and more ways to configure agent reviews.

### Highlights

- Run PR Board on Windows with native pane reuse, process cleanup, and shared review state.
- Select prompt and skill files in repository setup or TOML. Custom prompts replace the default review criteria.
- Choose Oh My Pi, Kimi, Qoder CLI, or Qwen Code alongside Pi, Codex, and Claude Code.
- Start with a concise README. Find detailed controls, configuration, and operations in focused guides.

Windows requires Herdr 0.9.0 or later and local NTFS configuration and state directories.
See [Windows setup](https://github.com/cdowell09/herdr-pr-board/blob/v0.6.0/docs/windows.md) for platform requirements.
See [agent compatibility](https://github.com/cdowell09/herdr-pr-board/blob/v0.6.0/docs/agent-compatibility.md) for CLI versions, authentication, and assessed limitations.

### Upgrade

```sh
herdr plugin install cdowell09/herdr-pr-board --ref v0.6.0
```

Reopen the board to load the new binary.
Restart an older monitor after its active reviews finish.

<details>
<summary>Full changelog</summary>

### Documentation

- Make README a concise landing page (#105)

### Features

- Support native Windows installation and runtime (#102)
- Configure review prompts and skills (#103)
- Add four native review adapters (#104)

</details>

## 0.5.0 - 2026-09-08

PR Board adds Codex and Claude Code reviews, plus a control to stop active reviews.

### Highlights

- **Choose Codex or Claude Code.** Both now have built-in adapters alongside Pi. Select the agent in repository settings, or define reviewer profiles with different review skills.
- **Stop one review without stopping your monitor.** Press `v`, then `t`. The panel identifies the run and shows cleanup progress. Stopped reviews keep their history, do not publish, and require an explicit retry.
- **Read the feature summary first.** Release notes now lead with curated highlights. The full git-cliff changelog remains available below.

The new adapters require Codex CLI 0.153.4+ or Claude Code 2.1.259+.
Install and authenticate the selected CLI and install your review skill before starting reviews.
Existing reviewer commands and repository permissions remain unchanged.
See [agent setup](https://github.com/cdowell09/herdr-pr-board/blob/v0.5.0/docs/agent-adapters.md).

### Upgrade

```sh
herdr plugin install cdowell09/herdr-pr-board --ref v0.5.0
```

Reopen the board to load the new binary.
Restart an older monitor after its active reviews finish to enable the stop control.

<details>
<summary>Full changelog</summary>

### Documentation

- Lead release notes with feature highlights (#96)

### Features

- Stop active reviews from the board (#97)
- Add built-in Codex and Claude Code adapters (#98)

</details>

## 0.4.0 - 2026-09-08

### Bug Fixes

- Address review findings across board, github, and sidebar (#46)
- Name the environment token that overrides gh login
- Complete automatic review onboarding (#62)
- Improve repository settings readability (#65)
- Clarify reviews and honor configured posting (#70)
- Show when reviews wait for a slot (#74)
- Preserve review origin and refresh failed page rates (#77)

### Features

- Edit config from board (#44)
- Expose JSON PR snapshots (#54)
- Track local review history and claims (#55)
- Share headless monitor observations (#56)
- Launch configurable PR reviewers (#57)
- Configure repository review publication (#58)
- Dispatch eligible PR reviews automatically (#59)
- Start opted-in review monitors automatically (#67)
- Show review progress and publication sources (#75)

### Refactoring

- Simplify release and runner plumbing
- Deepen rendering and refresh
- Trim review overhead

## 0.3.1 - 2026-08-15

### Bug Fixes

- Scope counts to current workspace

### Documentation

- Document plugin versioning policy

### Maintenance

- Bump plugin version to 0.3.1

## 0.3.0 - 2026-08-14

### Documentation

- Align README with STE writing standards (#37)

### Features

- Report PR counts into Herdr sidebar tokens (#39)

## 0.2.2 - 2026-08-14

### Bug Fixes

- Simplify shortcut footer

## 0.2.1 - 2026-08-14

### Bug Fixes

- Clarify shortcut footer

## 0.2.0 - 2026-08-14

### Bug Fixes

- Preserve completed CI enrichment batches (#17)
- Show accurate refresh freshness
- Keep stale layout within the screen
- Report browser-open failures; add contributor, security, and troubleshooting docs (#18)
- Diff whitespace against merge-base and whole push
- Center CI icons beneath the column heading
- Checkout before publishing release (#34)

### Documentation

- Add multi-org board screenshot
- Rewrite config reference in Simplified Technical English
- Add screenshot of CI completion gates
- Broaden the README screenshot to a full-screen board
- Fix README screenshot so every column renders
- Consolidate hook instructions
- Clarify source plugin setup

### Features

- Reject configuration mistakes and add validation mode
- Make the board readable and controls discoverable (#28)

### Maintenance

- Enforce completion gates in CI
- Automate dependency maintenance and update go-toml
- Bump the actions group with 2 updates
- Add pre-commit hooks
- Cross-build manifest platforms

### Performance

- Run configured-scope searches concurrently

### Testing

- Measure ANSI rows and derive CI glyphs from renderCI
- Verify pane reuse and state ownership end to end
- Pin freshest-row dedup; cover views+scopes concurrency cap

## 0.1.1 - 2026-08-09

### Bug Fixes

- Decouple PR Board from Pickr

### Documentation

- Add agent guidance

## 0.1.0 - 2026-08-07

### Features

- Add cross-repository PR board

### Maintenance

- Initialize repository
