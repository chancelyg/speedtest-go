# Changelog

All notable changes to speedtest-go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-07-14

### Added

- Source IP column in the History table (commit `35d21d0`). The peer
  address was already stored server-side and returned by `/api/results`;
  it's now rendered between the timestamp and the metrics so a shared
  single-machine deployment can see which client produced each row
  without exporting the table. Cell is muted / tabular-nums / ellipsis
  at 180 px (110 px on narrow screens), with a `title` tooltip showing
  the full address when truncated.
- `/api/config` now reports `maxConcurrent` (commit `c8c1bc9`), taken
  from `cap(h.sem)` so the `Handler.New`-coerced fallback value is
  authoritative rather than the raw `cfg.MaxConcurrent` field which can
  be `0` when a caller builds `Config` directly (tests, minimal
  embeddings). The frontend uses it to disable streams-selector options
  above cap (labelled `(>N)`) and to clamp `activeCfg.streams`.

### Fixed

- Sibling streams no longer leak past a failure (commit `c8c1bc9`). When
  one stream in `Promise.all` failed — typically a 503 from the
  concurrency semaphore when the user picked more streams than
  `MaxConcurrent` — the surviving fetches kept reading their response
  bodies and feeding the gauge past the user's chosen duration, and the
  toast's Retry button spawned a new run that competed with the leaked
  streams from the previous one. `measureDownload` and `measureUpload`
  now share an `AbortController` across siblings so the outer test
  signal, the time-mode deadline, and any sibling failure all funnel
  through one cancellation point. `postBlobUntil` gained an entry check
  for a pre-aborted signal so the worker's `!signal.aborted` guard
  can't race a doomed XHR into flight. `runTest`'s `finally` now calls
  `abortCtrl.abort()` as a belt-and-suspenders catch-all.
- `activeCfg.streams` used to desync from the visible streams dropdown
  when the merged value snapped to a smaller discrete option (commit
  `c8c1bc9`). Options are 1/2/4/8/16, so a cap of 10 makes
  `mergeConfig` yield 10 but the largest usable option is 8;
  programmatic `sel.value` assignment does not fire the `change`
  handler, so `measure*` would spawn 10 streams while the user saw 8
  and history would persist `streams: 10`. `applyConfigToUI` now reads
  the snapped value back into `activeCfg`.

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
