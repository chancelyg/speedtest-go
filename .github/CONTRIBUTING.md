# Contributing to speedtest-go

Thanks for your interest in improving speedtest-go. This guide covers
how to build and test the project locally, the style we expect, and the
hard boundaries we keep around the single-binary design.

## Project Invariants (read before opening a PR)

speedtest-go ships as **one statically-linked Go binary with the
frontend embedded via `//go:embed`**. Two non-negotiable rules follow
from that:

1. **Single binary, zero runtime dependencies.** No external API, no
   CDN, no sidecar process, no required database server.
2. **No frontend build pipeline.** `static/` is hand-written HTML/CSS/JS
   and is consumed directly by `go build`. We do not introduce npm,
   webpack, Vite, Tailwind, Babel, or any tool that turns source assets
   into different assets at build time.

See [SPEC.md](../SPEC.md) for the full set of invariants.

**Any PR that adds a new Go module dependency or any frontend tooling
must justify it in the PR description**, including:

- What problem does the new dependency solve that the standard library
  cannot?
- Have you evaluated alternatives that stay within the standard library?
- What is the dependency's maintenance and security track record?

PRs that violate the invariants without justification will be closed.

## Quick Start

Prerequisites:

- Go 1.24 (pinned by `go.mod`)
- Node.js (only for running the frontend unit tests; not used for
  building)
- `gofmt`, `go vet`, and optionally [`golangci-lint`](https://golangci-lint.run/)

```bash
# Build
go build .                       # produces ./speedtest-go

# Run in dev (auto-reloads embedded assets on each `go run`)
go run main.go

# Tests
go test ./... -race -count=1     # backend
node --test static/*.test.mjs    # frontend
go test -cover ./...             # coverage report
```

For multi-platform release dry-runs:

```bash
goreleaser build --snapshot --clean
```

## Code Style

### Go

- Format with `gofmt -s -w` (or your editor's `goimports`).
- Run `go vet ./...` and, if available, `golangci-lint run` before
  pushing.
- Prefer the standard library; the project's only non-stdlib runtime
  dependency at the time of writing is `modernc.org/sqlite` for the
  results store.
- Keep files under ~400 lines; extract a sibling file when a module
  grows past that.
- Follow the repository's Go coding standards (see
  `rules/golang/coding-style.md` if present in your local rules tree).

### Frontend

- Vanilla JS/CSS/HTML only — no transpilers, no preprocessors.
- Co-locate `.test.mjs` files next to the module under test.
- The CSS class / selector pairing rule applies: if you add a class to a
  template, add or update its CSS rule in the same change. UA defaults
  almost always violate the design contract.

### Commit Messages

Follow Conventional Commits. The prefixes we use most often:

- `feat:`, `fix:`, `refactor:`, `perf:`, `test:`, `docs:`, `chore:`,
  `ci:`, `security:`
- Optional scope in parentheses: `feat(handler): ...`,
  `fix(static/chart): ...`.
- Use the body to explain *why* the change is needed, not *what* the
  diff already shows.

## Pull Request Workflow

1. Open an issue or discussion first for non-trivial changes so we can
   agree on the approach.
2. Branch from `master`, keep the branch focused on one logical change.
3. Run the full test suite locally: backend race tests, frontend
   `node --test`, and `go build .` to confirm the single-binary output.
4. Fill in the [pull request template](./PULL_REQUEST_TEMPLATE.md),
   especially the **Boundary check** section.
5. Link the issue you are closing with `Closes #N` so GitHub auto-closes
   it on merge.
6. Wait for CI to go green before requesting review.

## Reporting Bugs / Requesting Features

Use the issue forms:

- [Bug report](./ISSUE_TEMPLATE/bug.yml)
- [Feature request](./ISSUE_TEMPLATE/feature.yml)

Security issues follow a separate, private channel — see
[SECURITY.md](./SECURITY.md).

## License

By contributing, you agree that your contributions will be released
under the same license as the rest of the project (see `LICENSE` at the
repository root).
