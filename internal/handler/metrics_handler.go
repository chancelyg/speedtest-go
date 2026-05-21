package handler

// Phase 4 Track A — Prometheus /metrics endpoint.
//
// Contract for agent A:
//
// Public surface:
//   - func (h *Handler) MetricsHandler(w http.ResponseWriter, r *http.Request)
//     — wired by main.go [P4-A] to GET /metrics.
//
// Internal collectors (kept on Handler as a *metricsRegistry field, defined
// in this file). Track A is responsible for both the prom registry struct
// AND the wiring: on Handler creation (handler.go New) instantiate the
// registry and add the Prometheus collectors, then increment them from the
// existing acquire() / 503 paths and from middleware-level request counts.
//
// Required series (snake_case, with the `speedtest_` prefix):
//   speedtest_active_tests             gauge      currently-running tests (len(h.sem))
//   speedtest_total_tests_total{status="accepted"|"rejected"}   counter
//   speedtest_bytes_transferred_total{direction="up"|"down"}    counter
//   speedtest_test_duration_seconds    histogram  per-test wall-clock
//   speedtest_http_requests_total{method,path,status}           counter (middleware)
//
// Dependency: github.com/prometheus/client_golang (pure-Go, single-binary
// constraint preserved). go.mod / go.sum will gain transitive deps —
// prometheus/client_model, prometheus/common, golang/protobuf, etc. Run
// `go mod tidy` after adding the import.
//
// Skeleton placeholder — the real implementation lands via agent A.
