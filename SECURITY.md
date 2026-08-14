# Security

PR Board is a Herdr plugin. It runs `gh` and the GitHub API on your behalf. Treat the plugin like any code you run.

Install the plugin from sources you trust. Review the `herdr-plugin.toml` manifest and the scripts it runs before you use it.

## Report a vulnerability

Report security problems through GitHub private vulnerability reporting. Do not open a public issue or pull request.

Use this form:

<https://github.com/cdowell09/herdr-pr-board/security/advisories/new>

This keeps the report private until it is understood and fixed. No personal email address is published. Use the GitHub form.

## Include this information

When you report, include what you found and what you expect instead:

- The plugin version (`herdr plugin list --plugin cdowell09.pr-board`) and operating system.
- Steps to reproduce, including any configuration and views involved.
- Why you believe it is a security problem. For example, where it stores data, what it runs, or what it sends to GitHub.

## What the plugin does not do

- It does not store a GitHub token. GitHub CLI (`gh`) supplies authentication.
- It does not read or write files outside `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`, and its managed plugin directory.
