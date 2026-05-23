# Changelog

All notable changes to speedtest-go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-05-23

### Added

- Touch-friendly hint tooltips on every metric tile (latency, jitter,
  packet loss, Bufferbloat, IP, connection type). A shared overlay is
  positioned in viewport coordinates and clamped to the screen so long
  Chinese / English strings never overflow on narrow phones. Triggers on
  hover, focus, and tap-to-toggle (Esc / outside-click to dismiss).

### Fixed

- Time-mode tests now end at the user-selected duration even on slow
  links. `downloadByTime` previously wrote in 1 MB chunks and only
  checked the deadline at the top of the loop, so a Write blocked on a
  full TCP send buffer could keep the response open for tens of seconds
  past the configured duration. The handler now writes in 64 KB chunks
  and installs a `SetWriteDeadline` via `http.NewResponseController`
  (with the logging middleware now exposing `Unwrap()` so the deadline
  reaches the underlying conn). The frontend mirrors the cap with an
  `AbortController` for defence in depth.
- Latency and jitter shown at the end of each phase are now averaged
  across every ping taken during that phase instead of only the trailing
  ~5 s rolling window, so the numbers reflect the whole phase rather
  than the few seconds right before the test ended. Packet loss keeps
  its rolling window since recency matters more there.

## [0.1.0] - 2026-05-22

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

### Fixed

- `IdleTimeout` no longer truncates long tests; it now scales with the
  configured test duration plus a 60 s safety margin so a `?duration=300`
  run completes instead of being closed at 120 s.
- Upload responses now reply `200` with a `truncated` flag and the partial
  byte count when the body exceeds the cap, instead of returning `413`.
  Gigabit-class samples that previously looked like failures now produce
  usable throughput numbers.
- Download and upload responses send `Content-Encoding: identity` so
  transparent gzip-aware proxies cannot distort the throughput sample.
- `uploadResponse` exposes `serverElapsedMs`; the front-end uses the
  server-observed wall-clock for the final upload number, sidestepping
  client-side timing weirdness like tab throttling.
- The packet-loss metric now ships with an i18n tooltip clarifying that
  the figure is the HTTP request failure rate, not UDP packet loss.

[Unreleased]: https://github.com/chancelyg/speedtest-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/chancelyg/speedtest-go/compare/v0.0.2...v0.1.0
