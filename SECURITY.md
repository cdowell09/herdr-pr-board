# Security

PR Board is a plugin that runs `gh` and the GitHub API on your behalf. Treat the plugin like any code you run: install it from sources you trust, and review the `herdr-plugin.toml` manifest and the scripts it runs before you use it.

## Reporting a vulnerability

Please report security problems through **GitHub private vulnerability reporting** rather than a public issue or pull request:

<https://github.com/cdowell09/herdr-pr-board/security/advisories/new>

This keeps the report private until it is understood and, where needed, fixed. No personal email address is published for reporting; use the GitHub form.

## What to include

When you report, include what you found and what you expect instead:

- The plugin version (`herdr plugin list --plugin cdowell09.pr-board`) and operating system.
- Steps to reproduce, including any configuration and views involved.
- Why you believe it is a security problem (for example, where it stores data, what it runs, or what it sends to GitHub).

## What the plugin does not do

- It does not store a GitHub token. GitHub CLI (`gh`) supplies authentication.
- It does not read from or write to files outside `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`, and its own managed plugin directory.
