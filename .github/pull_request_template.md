## What this changes

Describe the behavior or configuration this pull request changes. Explain why.

## Behavior

- [ ] Describe the change to behavior, controls, configuration, or requirements.

## Tests

- [ ] Add or update a regression test at the owning package boundary.
- [ ] `go test ./...` passes.
- [ ] `go test -race ./...` passes (after concurrency or cache changes).

## Documentation

- [ ] Update `README.md` for user-visible behavior, configuration, or controls.
- [ ] Update `config.example.toml` for new or changed configuration.
- [ ] Keyboard and mouse documentation matches the rendered UI.

## Completion gates

Report the results from the repository root:

```text
go vet ./...
gofmt -w cmd internal
bash -n bin/open bin/run
git diff --check
```

- [ ] All completion gates pass. `git status --short` contains only intended files.

## Related

Closes #___ or links the issue(s) this pull request addresses.
