# Contributing to cerne

Thanks for taking the time. cerne is young, so the most useful contributions right now are the cheap ones: telling us where it broke, where the docs lost you, and what you expected to happen.

## Where things go

| You have | Go to |
|---|---|
| A question, an idea, or a "how do I…" | [Discussions](https://github.com/henriquesakata/cerne/discussions) |
| A bug, or behaviour that surprised you | [A bug report](https://github.com/henriquesakata/cerne/issues/new?template=bug_report.yml) |
| A concrete feature request | [A feature request](https://github.com/henriquesakata/cerne/issues/new?template=feature_request.yml) |
| A security problem | A [private vulnerability report](https://github.com/henriquesakata/cerne/security/advisories/new), not a public issue |

For anything larger than a small fix, open an issue or a discussion before writing code. It saves you from building something that will not be merged.

## Building and testing

You need Go at the version in `go.mod` or newer. Node 24 is only needed if you touch the web UI.

```bash
go build -ldflags="-s -w" -o cerne ./cmd/cerne   # the UI is embedded, no separate step
go test ./internal/<pkg>/ -run TestName -v       # focused tests while you work
go vet ./... && go test ./... -count=1 -timeout 30m   # the full suite
```

The full suite spawns real git processes and takes several minutes; the `-timeout 30m` is there because a loaded machine can pass Go's default 10 minutes per package. CI runs the same command on every pull request, so running focused tests locally and letting CI run the rest is fine.

## What a pull request needs

- **A test that fails without your change.** The project is developed test-first; a fix without a test is likely to be asked for one.
- **One concern per commit**, with a [Conventional Commits](https://www.conventionalcommits.org/) message (`fix(cli): …`, `feat(lint): …`, `docs: …`).
- **English** for code, comments, identifiers and CLI output. Web UI strings exist in both English and Portuguese; add both.
- **No speculative API.** Export only what is used.
- If an AI assistant wrote part of the change, say so with a `Co-Authored-By:` trailer naming the model.

Two things in this repository will surprise you if nobody warns you:

- **Do not rebuild or commit `ui/dist`.** It is embedded in the binary and must be built on the CI platform to be reproducible; a build from macOS or Windows fails the drift check. If your change touches `ui/src`, say so in the pull request and a maintainer will rebuild it.
- **Do not touch `docs.lock`.** A CI check refuses any pull request that changes it.

## You do not need access to the docs backend

cerne documents itself with cerne, and that documentation lives in a separate backend repository that outside contributors cannot read. You will see its traces in the tree: `.cernerc`, `docs.lock`, and feature ids such as `F-0357` in commit messages.

None of this blocks you. The code builds and every test runs without the backend; commands like `cerne pull` in this checkout will simply fail for you, and you can ignore them. If your contribution becomes a tracked feature, a maintainer registers it and adds the id.

## Reporting a good bug

The fastest fixes come from reports with:

- `cerne --version` and your operating system;
- the exact command you ran and its full output;
- what you expected instead;
- `--verbose` output if the command was slow or hung.

## License

By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
