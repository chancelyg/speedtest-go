# Changelog

All notable changes to speedtest-go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- PWA support: installable manifest + service worker for offline shell.
- Prometheus `/metrics` endpoint exposing counters / histograms for the
  speed-test handlers.
- Per-IP rate limiting on the speed-test endpoints (token bucket; bounds
  configurable via env / flags).
- CLI flags as an alternative to the `SPEEDTEST_*` environment variables
  (flags win when both are set).
- JSON config file loader for operators who prefer a single declarative
  source over env/flags.
- IPv6 as the default bind address (`::`) so dual-stack hosts work
  out-of-the-box without an explicit override.
- Caddy reverse-proxy and auto-TLS deployment example in
  [docs/deployment.md](docs/deployment.md).
- `Dockerfile` based on `gcr.io/distroless/static:nonroot` for the GHCR
  multi-arch image published by goreleaser.
- Repository compliance docs: SECURITY policy, CONTRIBUTING guide, PR
  template, and structured bug / feature issue forms.
- Release engineering: GPG-signed checksums, CycloneDX SBOMs for archives
  and source, ldflag-injected `version` / `commit` / `date`, and a GHCR
  multi-arch image manifest (`amd64` + `arm64`).

### Changed

- History UI now paginates the results table and exports inline; the
  separate trends panel has been removed in favour of the inline history
  drawer (commit `cf96d0e`).

### Removed

- `DELETE /api/results` and per-row deletion endpoints — history records
  are immutable from the HTTP surface (commit `a3d1547`). Operators can
  still rotate the SQLite store at the filesystem level.

### Security

- Hardened the public API surface (commit `a3d1547`):
  - Generic `500 Internal Server Error` responses; internal failure detail
    is logged server-side only, never leaked to the client.
  - POST body sanitisation across the write endpoints to reject malformed
    or oversized payloads early.
  - CSV-injection prevention for exported result files (leading `=`, `+`,
    `-`, `@`, tab, CR characters in cell values are neutralised).

[Unreleased]: https://github.com/chancelyg/speedtest-go/compare/HEAD...HEAD
