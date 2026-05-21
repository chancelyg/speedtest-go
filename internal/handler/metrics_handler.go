package handler

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Phase 4 Track A — Prometheus /metrics endpoint.
//
// Exposed series (snake_case, with the `speedtest_` prefix):
//
//	speedtest_active_tests
//	    gauge — number of speed-test slots currently held in the
//	    concurrency semaphore. Implemented as a GaugeFunc reading
//	    len(h.sem) so the value is always exact and lock-free.
//
//	speedtest_total_tests_total{status="accepted"|"rejected"}
//	    counter — every call to Handler.acquire() increments exactly
//	    one of the two label values.
//
//	speedtest_bytes_transferred_total{direction="up"|"down"}
//	    counter — bytes streamed by Download/UploadHandler. Recorded
//	    via ObserveTest at the end of each test, even on early exit.
//
//	speedtest_test_duration_seconds
//	    histogram — per-test wall-clock duration. Buckets tuned to the
//	    5/10/15/30/60 s frontend duration knobs.
//
//	speedtest_http_requests_total{method,path,status}
//	    counter — populated by main.go's loggingMiddleware after every
//	    request. Exposed via (h *Handler).ObserveRequest.
//
// All collectors live on a private *prometheus.Registry so we never
// touch the default global registry. This keeps test isolation simple
// and ensures the binary does not export unrelated process metrics
// unless explicitly wired in.

// durationBuckets matches the supported frontend duration knobs plus a
// little headroom on either side so outliers still bucket cleanly.
var durationBuckets = []float64{0.5, 1, 2.5, 5, 10, 15, 30, 60, 120}

// metricsRegistry owns the Prometheus collectors and the registry they
// are registered on. The struct itself is intentionally unexported —
// callers go through Handler methods (ObserveTest, ObserveRequest,
// MetricsHandler).
type metricsRegistry struct {
	registry *prometheus.Registry

	activeTests       prometheus.GaugeFunc
	totalTests        *prometheus.CounterVec // labels: status
	bytesTransferred  *prometheus.CounterVec // labels: direction
	testDuration      prometheus.Histogram
	httpRequestsTotal *prometheus.CounterVec // labels: method, path, status
}

// newMetricsRegistry builds a metricsRegistry whose active_tests gauge
// reflects the live length of the supplied semaphore. The semaphore is
// captured by reference, so the returned registry must not outlive the
// Handler that owns it.
func newMetricsRegistry(sem chan struct{}) *metricsRegistry {
	reg := prometheus.NewRegistry()

	activeTests := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "speedtest_active_tests",
			Help: "Number of speed-test slots currently held in the concurrency semaphore.",
		},
		func() float64 { return float64(len(sem)) },
	)

	totalTests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "speedtest_total_tests_total",
			Help: "Speed-test attempts partitioned by admission outcome (accepted/rejected).",
		},
		[]string{"status"},
	)
	// Initialise both label values so the series appear at zero before the
	// first request — easier for dashboards and alert rules.
	totalTests.WithLabelValues("accepted").Add(0)
	totalTests.WithLabelValues("rejected").Add(0)

	bytesTransferred := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "speedtest_bytes_transferred_total",
			Help: "Total bytes streamed during speed tests, partitioned by direction (up/down).",
		},
		[]string{"direction"},
	)
	bytesTransferred.WithLabelValues("up").Add(0)
	bytesTransferred.WithLabelValues("down").Add(0)

	testDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "speedtest_test_duration_seconds",
		Help:    "Wall-clock duration of a single download or upload test.",
		Buckets: durationBuckets,
	})

	httpRequestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "speedtest_http_requests_total",
			Help: "Total HTTP requests processed, partitioned by method, path and status.",
		},
		[]string{"method", "path", "status"},
	)

	reg.MustRegister(activeTests, totalTests, bytesTransferred, testDuration, httpRequestsTotal)

	return &metricsRegistry{
		registry:          reg,
		activeTests:       activeTests,
		totalTests:        totalTests,
		bytesTransferred:  bytesTransferred,
		testDuration:      testDuration,
		httpRequestsTotal: httpRequestsTotal,
	}
}

// recordAdmission increments the accepted/rejected counter exactly once
// per acquire() call. Kept as a tiny method so the Handler hot path
// doesn't need to know about labels.
func (m *metricsRegistry) recordAdmission(accepted bool) {
	if accepted {
		m.totalTests.WithLabelValues("accepted").Inc()
	} else {
		m.totalTests.WithLabelValues("rejected").Inc()
	}
}

// ObserveTest records the result of one completed speed test. direction
// must be "up" or "down". bytes may be zero (e.g. a rejected test or a
// client that disconnected before any payload was streamed). elapsed is
// always recorded — even tiny values are useful for SLO histograms.
func (m *metricsRegistry) ObserveTest(direction string, bytes int64, elapsed time.Duration) {
	if bytes > 0 {
		m.bytesTransferred.WithLabelValues(direction).Add(float64(bytes))
	}
	m.testDuration.Observe(elapsed.Seconds())
}

// observeRequest records one HTTP request from the logging middleware.
func (m *metricsRegistry) observeRequest(method, path, status string) {
	m.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
}

// ── Handler-facing surface ──────────────────────────────────────────────

// MetricsHandler serves the Prometheus exposition format on /metrics.
// When the registry has not been wired (h.metrics == nil) it returns a
// 503 rather than a misleading empty 200 — surfacing the configuration
// error to the scrape job.
func (h *Handler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reg, ok := h.metrics.(*metricsRegistry)
	if !ok || reg == nil {
		http.Error(w, "metrics not initialised", http.StatusServiceUnavailable)
		return
	}
	promhttp.HandlerFor(reg.registry, promhttp.HandlerOpts{
		// Continue serving stale-but-valid data when individual collectors
		// fail; failing the whole scrape is rarely the right answer for a
		// self-hosted tool that should keep working.
		ErrorHandling: promhttp.ContinueOnError,
	}).ServeHTTP(w, r)
}

// ObserveTest is the public Handler-method wrapper that download/upload
// handlers call when a test completes. It is a no-op when metrics are
// disabled, keeping the call-site noise-free.
func (h *Handler) ObserveTest(direction string, bytes int64, elapsed time.Duration) {
	if reg, ok := h.metrics.(*metricsRegistry); ok && reg != nil {
		reg.ObserveTest(direction, bytes, elapsed)
	}
}

// ObserveRequest is invoked by main.go's loggingMiddleware once per
// request. method/path/status are kept as strings (rather than ints)
// because Prometheus labels are always strings and the conversion is
// cheaper to do once at the call site.
func (h *Handler) ObserveRequest(method, path, status string) {
	if reg, ok := h.metrics.(*metricsRegistry); ok && reg != nil {
		reg.observeRequest(method, path, status)
	}
}
