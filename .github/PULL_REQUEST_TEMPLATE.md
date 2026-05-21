<!-- Phase 4 Track E — agent E to replace this scaffold with the real template.
     Sections expected:
     - ## Summary (one-line + bullets)
     - ## Test plan (checkbox list)
     - ## Boundary check: am I adding deps, breaking single-binary, touching docs?
     - Linked issue / Closes # -->

## Summary

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] `node --test static/*.test.mjs`
- [ ] `go build .` produces a single binary

## Boundary check
- [ ] No new Go module deps (or justified above)
- [ ] No new frontend build pipeline
- [ ] No external API / CDN dependency
