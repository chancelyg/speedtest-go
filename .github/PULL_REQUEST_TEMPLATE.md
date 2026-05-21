<!-- Thanks for contributing! Please fill in each section below.
     See CONTRIBUTING.md for the full guidelines. -->

## Summary

<!-- One-line description of the change, followed by 1-3 bullet points
     covering the motivation and notable implementation choices. -->

-
-

## Linked issue

<!-- e.g. "Closes #123" so GitHub auto-closes the issue on merge.
     If this PR doesn't have a tracking issue, briefly explain why. -->

Closes #

## Test plan

- [ ] `go test ./... -race -count=1`
- [ ] `node --test static/*.test.mjs`
- [ ] `go build .` produces a single binary
- [ ] Manually exercised the affected endpoint / UI flow
- [ ] Updated or added docs where behaviour changed

## Boundary check

speedtest-go is a single statically-linked binary with the frontend
embedded via `//go:embed`. Confirm this PR does not break that:

- [ ] No new Go module dependency (or justified above)
- [ ] No new frontend build pipeline (no npm / bundler / preprocessor)
- [ ] No new external API / CDN / sidecar runtime requirement
- [ ] Any new env var or CLI flag is documented in `README.md` and
      `docs/configuration.md`

## Additional notes

<!-- Anything reviewers should pay extra attention to: tricky concurrency,
     migration steps, follow-up tickets, screenshots, etc. -->
