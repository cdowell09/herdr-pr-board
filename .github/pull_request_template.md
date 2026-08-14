## What this changes

Describe the user-visible behavior or configuration this pull request changes, and why.

## Behavior

- [ ] Describe the change to behavior, controls, configuration, or requirements.

## Tests

- [ ] Regression tests added or updated at the owning package boundary.
- [ ] `go test ./...` passes.
- [ ] `go test -race ./...` passes (after concurrency or cache changes).

## Documentation

- [ ] `README.md` updated for user-visible behavior, configuration, or controls.
- [ ] `config.example.toml` updated for new or changed configuration.
- [ ] Keyboard and mouse documentation matches the rendered UI.

## Completion gates

Report the results from the repository root:

```text
go vet ./...
gofmt -w cmd internal
bash -n bin/open bin/run
git diff --check
```

- [ ] All completion gates pass and `git status --short` contains only intended files.

## Related

Closes #___ or links the issue(s) this pull request addresses.
