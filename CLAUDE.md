# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Self-hosted network speed-test site (download / upload / latency / jitter / loss) shipped as a single Go binary with the frontend embedded via `//go:embed`. No external API or CDN dependencies — design constraint, not just current state. See [SPEC.md](SPEC.md) for invariants.

## Commands

```bash
# Run in development
go run main.go

# Build for current platform
go build -o speedtest .

# Tests (the CI workflow runs the first one with -count=1)
go test ./... -count=1
go test -race ./...
go test -cover ./...

# Run a single test
go test ./internal/handler -run TestClientIP -v

# Multi-platform snapshot build (no publish)
goreleaser build --snapshot --clean
```

Releases are tag-driven: pushing `v*` triggers [.github/workflows/release.yml](.github/workflows/release.yml), which runs tests then `goreleaser release --clean`. The Go toolchain version is pinned by `go.mod` (Go 1.24).

## Architecture

Three-layer Go program. Each layer has a narrow job; cross-layer changes are usually the sign of a missing concept.

- **`main.go`** — wires up `http.Server`, the `loggingMiddleware`, the embedded static FS, and graceful shutdown (SIGINT/SIGTERM → 30 s drain so in-flight speed tests can complete). `WriteTimeout` is intentionally `0` because download/upload streams are designed to run longer than any fixed timeout — do not "fix" this by adding one.
- **`internal/config`** — loads everything from env vars (`SPEEDTEST_*`); see the table in [README.md](README.md). All values clamp to safe ranges in `envInt` rather than erroring, so the binary is never unstartable due to a bad env var.
- **`internal/handler`** — all `/api/*` endpoints. One `Handler` struct shares a buffered-channel semaphore (`sem`) that caps concurrent download+upload tests at `cfg.MaxConcurrent`. Exceeding capacity returns 503 immediately rather than queueing.
- **`static/`** — vanilla HTML/CSS/JS, embedded with `//go:embed static`. No build step. Because assets are compiled in, **changing a file in `static/` requires rebuilding the Go binary** for a running server to see it (`go run` re-embeds on each start).

### Speed-test modes (important invariant)

Two measurement strategies that the same handler can produce:

- **time mode** (`ModeTime`): `downloadByTime` writes random bytes with chunked transfer-encoding until a deadline. Requires `http.Flusher` to actually stream — that's why `statusWriter` in `main.go` explicitly implements `Flush()` (without it, the wrapper hides the underlying `Flusher` and downloads stall).
- **size mode** (`ModeSize`): `downloadBySize` sets `Content-Length` and writes exactly N bytes.

The query parameters `?bytes=` / `?size=` force size mode for that single request regardless of `cfg.Mode`; `?duration=` overrides the time-mode deadline. The frontend uses these to split a size test across `cfg.Streams` parallel connections.

### Security guardrails worth knowing before changing

- `maxBytesPerStream` (1 GB) caps `?bytes=`; `maxDurationSecs` (300 s) caps `?duration=`; `maxUploadBytes` (10 GB) wraps the upload body in `http.MaxBytesReader`.
- `ClientIP` only trusts `X-Forwarded-For` / `X-Real-Ip` when the direct peer is loopback or RFC-1918/4193 private. Don't relax this without an explicit reverse-proxy story.
- The favicon handler prefers `./favicon.ico` from the working directory and falls back to the embedded one — this is the documented runtime-override mechanism, not a bug.

## Boundaries (from SPEC.md)

- **Always**: single-binary, zero external runtime deps.
- **Ask first**: adding any Go module or frontend library.
- **Never**: depend on an external API/CDN, or introduce a frontend build pipeline.
