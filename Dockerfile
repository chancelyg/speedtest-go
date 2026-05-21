# syntax=docker/dockerfile:1
#
# Runtime image for speedtest-go.
#
# This Dockerfile is consumed by goreleaser's `dockers:` blocks, which copy
# the pre-built static binary into the image context before invoking
# `docker buildx build`. Do not add a Go toolchain or build steps here —
# goreleaser cross-compiles outside the container, in keeping with the
# project's single-binary / zero-runtime-dependency invariant.
#
# Base: gcr.io/distroless/static:nonroot
#   - No shell, no package manager, no libc beyond what static Go needs.
#   - Runs as uid/gid 65532:65532 by default (nonroot).
#   - ~2 MB compressed, smaller and more locked-down than alpine.
FROM gcr.io/distroless/static:nonroot

COPY speedtest /usr/local/bin/speedtest

# IPv6-friendly default bind ("::" binds both v4 and v6 on dual-stack hosts).
ENV SPEEDTEST_HOST=:: \
    SPEEDTEST_PORT=8080

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/speedtest"]
