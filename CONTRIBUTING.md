# Contributing to docsli

## Trunk-based workflow

`main` is always releasable. **Never push directly to `main`** — every change lands through a short-lived branch and a pull request. Branch protection enforces this: PRs require green `test`, `lint`, and `vulncheck` checks before merge.

```
main ──●─────────────●──────────●──  (always green, always releasable)
        \           /          /
         feature/x         fix/y      (short-lived, squash-merged)
```

## Branch naming

Name branches `prefix/kebab-summary`, where `prefix` is one of:

| Prefix      | Use for                                  |
|-------------|------------------------------------------|
| `feature/`  | new functionality                        |
| `fix/`      | bug fixes                                |
| `hotfix/`   | urgent production fixes                  |
| `chore/`    | maintenance, deps, tooling               |
| `ci/`       | CI / build pipeline changes              |
| `docs/`     | documentation only                       |
| `refactor/` | internal restructure, no behavior change |
| `test/`     | tests only                               |

Examples: `fix/history-follow-rename`, `feature/doc-list-pagination`, `docs/mirror-setup-troubleshooting`.

## Before opening a PR

Run the local gate — it must pass:

```sh
make test     # go test -race ./...
make lint     # golangci-lint run
make vuln     # govulncheck ./...
```

CI re-runs the same three as required checks. The test suite runs against real temp-dir git repos — no mocks — so a working `git` binary is the only test dependency.

A few project conventions worth knowing before you write code:

- **Git via CLI only.** All git access goes through `internal/gitstore`'s runner (`git -C dir ...` argv slices, never shell strings). No go-git.
- **Errors talk to agents.** Error messages returned from tools are read by AI agents — make them instructive (what went wrong, which tool call fixes it). See `internal/gitstore/errors.go`.
- **Never log tokens or document content.** Log who wrote what path, when — nothing else.

## Merging

Squash-merge only, keeping `main` history linear. CI must be green; the maintainer merges.

## Releasing

Releases are tag-driven. Pushing a `vX.Y.Z` tag triggers the `publish` workflow, which builds the multi-arch container image (amd64 + arm64) and pushes it to [ghcr.io/chalvinwz/docsli](https://github.com/chalvinwz/docsli/pkgs/container/docsli) with semver tags; `latest` tracks `main`.

Only maintainers with write access can push release tags. Contributors working from a fork propose changes as PRs; a maintainer cuts the release once they land on `main`.

## Good first issues

New to the project? Look for issues labelled **`good first issue`** (small, well-scoped) and **`help wanted`**. Comment on an issue to have it assigned to you before starting, so two people don't duplicate work.

## Reporting security issues

Do **not** open a public issue for vulnerabilities. Follow the process in [SECURITY.md](SECURITY.md) (private reporting via GitHub Security advisories).
