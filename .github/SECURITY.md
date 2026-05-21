# Security Policy

speedtest-go is a self-hosted single-binary network speed-test server.
This document describes how to report a vulnerability and what we commit
to in return.

## Supported Versions

This project is pre-1.0 and ships from a single active line of
development. Only the **latest tagged minor release** receives security
fixes; older tags will not be patched.

| Version              | Supported          |
| -------------------- | ------------------ |
| latest minor release | :white_check_mark: |
| anything older       | :x:                |

If you are running an unreleased commit from `master`, please reproduce
the issue against the most recent tag before reporting.

## Reporting a Vulnerability

**Please do not file public GitHub issues for security bugs.**

Use one of the following private channels:

1. **GitHub Security Advisory** (preferred) — open a draft advisory at
   <https://github.com/chancelyg/speedtest-go/security/advisories/new>.
   This keeps disclosure private until a fix is ready.
2. **Email** — `security@speedtest-go.invalid` (replace with the
   maintainer-controlled address before the first public release).
   PGP key fingerprint and ASCII-armored public key will be published
   alongside the first signed release.

Please include:

- A description of the issue and its impact (information disclosure,
  RCE, DoS amplification, etc.).
- Steps to reproduce, ideally against a stock binary.
- Affected version (`./speedtest --version` output if you have it).
- Any proof-of-concept payloads, request captures, or logs.

## Response SLA

| Stage                        | Target               |
| ---------------------------- | -------------------- |
| Initial acknowledgement      | within **72 hours**  |
| Triage + severity assessment | within **7 days**    |
| Coordinated public disclosure| within **90 days**   |

We will keep the reporter updated at each stage and credit them in the
release notes unless they request anonymity. CVE assignment is
coordinated through GitHub Security Advisories.

## Scope

In scope:

- The Go HTTP handlers in `internal/handler` and the configuration
  loader in `internal/config`.
- The embedded frontend assets in `static/`.
- The release pipeline (`.goreleaser.yaml`, GitHub Actions workflows,
  signed checksums, SBOMs, container images).

Out of scope:

- **Denial of service against a binary exposed directly to the public
  internet without a reverse proxy.** Speed-test traffic is, by design,
  bandwidth- and connection-intensive; mitigations such as TLS
  termination, connection limits, and rate limiting are the operator's
  responsibility (see [docs/deployment.md](../docs/deployment.md)).
- **Brute-force or scraping attacks when the `SPEEDTEST_RATE_LIMIT_*`
  environment variables are unset** — rate limiting is an opt-in
  guardrail, not a default authentication boundary.
- **Physical access** to the host running the binary (someone with shell
  on the box can already read the SQLite results store and the
  configuration).
- Bugs in **third-party reverse proxies, CDNs, or container runtimes**
  used in front of speedtest-go.
- **Self-XSS** that requires the victim to paste hostile content into
  their own browser console.

## Hardening Reminders for Operators

- Run behind a reverse proxy that terminates TLS (Caddy, nginx, Cloudflare
  Tunnel — see [docs/deployment.md](../docs/deployment.md)).
- Enable per-IP rate limiting via the `SPEEDTEST_RATE_LIMIT_*` env vars
  if the instance is exposed to untrusted networks.
- Restrict `/healthz` and `/metrics` to internal CIDRs at the proxy layer
  (the Caddy example in the deployment docs shows the pattern).
- Verify release artifacts: `gpg --verify checksums.txt.sig checksums.txt`
  before extracting any archive, and `sha256sum -c checksums.txt`
  before running the binary.
