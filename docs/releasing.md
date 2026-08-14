# Release PR Board

Use a Git tag that matches the version in `herdr-plugin.toml`.

## Prepare a release

1. Change `version` in `herdr-plugin.toml`.
2. Run every completion gate from the repository root.
3. Review the change and merge it into `main`.
4. Pull the updated `main` branch.

Run the completion gates:

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
go test -race ./...
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
bash -n bin/open bin/run
git diff --check
```

Do not commit `bin/herdr-pr-board`.

## Create a release

Set the release version to the manifest version.

```sh
version="0.1.2"
go run ./cmd/herdr-release-check --tag "v$version"
git tag -a "v$version" -m "Release v$version"
git push origin "v$version"
```

The release workflow validates every pushed tag.
It accepts only the `vX.Y.Z` form.
It compares the tag with the manifest version.

The workflow creates the GitHub release only after validation succeeds.
A failed validation does not create a GitHub release.

The workflow generates release notes from merged GitHub changes.
Review the generated notes on the release page.
Edit the notes when they need more context.

## Release assets

Do not attach binary release assets.
Herdr installs the GitHub source repository.
Herdr runs the manifest `[[build]]` command during installation.
The manifest builds `bin/herdr-pr-board` on the user's machine.

This process follows the [Herdr 0.8.0 plugin documentation](https://github.com/herdrdev/herdr/blob/master/docs/versions/0.8.0/website/src/content/docs/plugins.mdx).
That documentation describes GitHub source installation and manifest build commands.
See the [Herdr 0.8.0 CLI plugin reference](https://github.com/herdrdev/herdr/blob/master/docs/versions/0.8.0/website/src/content/docs/cli-reference.mdx#plugins) for the install command.

Revisit this process if Herdr changes its plugin installer.
Update the release workflow and this guide if Herdr starts using release assets.

## Upgrade an installation

Reinstall the plugin to replace the managed source checkout.

```sh
herdr plugin install cdowell09/herdr-pr-board --ref v0.1.2
```

Use the new tag for an exact release.
Omit `--ref` to install the repository's default branch.
Herdr preserves the plugin configuration and runtime state directories.
