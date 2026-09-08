# Release PR Board

Use a Git tag that matches the version in `herdr-plugin.toml`.

## Versioning

Use semantic versioning for the plugin version.
The version has three parts: major, minor, and patch.

- Bump the patch version for a bug fix.
- Bump the minor version for a new feature.
- Bump the major version for a breaking change.
- Do not bump the version for a documentation-only change.

A breaking change changes the user configuration or the user behavior.

## Prepare a release

Install [git-cliff](https://git-cliff.org/docs/installation/) before you prepare release notes.
On macOS, use Homebrew:

```sh
brew install git-cliff
git-cliff --version
```

CI uses git-cliff 2.13.1.
The repository's `cliff.toml` controls changelog groups and formatting.
Use Conventional Commit subjects for merged changes.
The changelog omits `chore(release):` commits.
Keep feature and bug-fix changes separate from release preparation.

1. Change `version` in `herdr-plugin.toml`.
2. Generate `CHANGELOG.md` with the new version.
3. Review the generated entries against the commits since the previous release.
4. Run every completion gate from the repository root.
5. Commit the release preparation with a `chore(release):` subject.
6. Review the change and merge it into `main`.
7. Pull the updated `main` branch.

Use the manifest version for the changelog tag:

```sh
version="$(python3 -c 'import tomllib; print(tomllib.load(open("herdr-plugin.toml", "rb"))["version"])')"
git-cliff --tag "v$version" --offline --output CHANGELOG.md
git-cliff --unreleased --tag "v$version" --strip header --offline
```

The first git-cliff command generates the full changelog.
The second git-cliff command previews only the next release notes.
These commands do not create a Git tag.
Do not edit generated entries directly.
Change `cliff.toml` when the generated format needs changes.

Run the completion gates:

```sh
gofmt -w cmd internal
go test ./...
python3 -B -m unittest discover -s scripts -p '*_test.py'
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...
go test -race ./...
go build -o bin/herdr-pr-board ./cmd/herdr-pr-board
bash -n bin/open bin/run
git diff --check
```

Do not commit `bin/herdr-pr-board`.

## Create a release

Set the release version to the manifest version.

```sh
version="$(python3 -c 'import tomllib; print(tomllib.load(open("herdr-plugin.toml", "rb"))["version"])')"
python3 scripts/validate_release.py herdr-plugin.toml "v$version"
git tag -a "v$version" -m "Release v$version"
git push origin "v$version"
```

The release workflow validates every pushed tag.
It accepts only the `vX.Y.Z` form.
It compares the tag with the manifest version.

The workflow creates the GitHub release only after validation succeeds.
A failed validation does not create a GitHub release.

The workflow uses git-cliff and `cliff.toml` to generate notes for the pushed tag.
It passes those notes to `gh release create` with `--notes-file`.
It does not use GitHub-generated notes.
Review the generated notes on the release page.
The release notes contain only the tagged release.
`CHANGELOG.md` contains all releases.

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
herdr plugin install cdowell09/herdr-pr-board --ref v0.4.0
```

Use the new tag for an exact release.
Omit `--ref` to install the repository's default branch.
Herdr preserves the plugin configuration and runtime state directories.
Close and reopen the board to load the new binary.
Restart an older background monitor after its active reviews finish.
The board reuses a running monitor, even after an upgrade.
