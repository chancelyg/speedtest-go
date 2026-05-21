package handler

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/store"
)

// payloadSize is the size of the shared random buffer streamed for downloads.
// 1 MB amortises syscall overhead vs. the previous 256 KB while keeping the
// resident memory footprint trivial.
const payloadSize = 1 << 20 // 1 MB

// randomPayload is a single high-entropy buffer generated once at process
// start and reused across every download response. Reuse is safe because the
// payload need only be incompressible (so intermediate gzip-aware proxies do
// not falsify throughput) — not unpredictable. Generating fresh randomness
// per chunk made downloads CPU-bound on gigabit+ links.
var randomPayload [payloadSize]byte

func init() {
	if _, err := rand.Read(randomPayload[:]); err != nil {
		// init() runs at boot; refusing to start is preferable to serving zeros.
		panic("speedtest: failed to seed download payload: " + err.Error())
	}
}

// maxUploadBytes is the hard cap for a single upload request body (10 GB).
// Combined with the concurrent-test semaphore this bounds total memory
// consumption from upload traffic.
const maxUploadBytes = 10 << 30

// maxBytesPerStream caps the ?bytes query-parameter override to 1 GB per
// stream. This is far above any realistic speed-test need and prevents a
// client from requesting unbounded downloads.
const maxBytesPerStream = 1 << 30 // 1 GB

// Handler holds the server configuration and exposes HTTP handler methods.
type Handler struct {
	cfg *config.Config
	// sem limits the number of concurrent active download/upload tests.
	sem chan struct{}

	// store is the optional persistence backend. When nil, history-related
	// endpoints return 503 and historyEnabled() reports false. Keeping the
	// dependency optional lets the server run in stateless mode without a
	// writable filesystem.
	store store.Store

	// startedAt is captured at New() time so /healthz can report uptime.
	startedAt time.Time

	// acceptedTotal counts download+upload tests that successfully reserved
	// a semaphore slot; rejectedTotal counts those rejected with 503.
	acceptedTotal atomic.Int64
	rejectedTotal atomic.Int64

	// Phase 4 injection points. Both are nil by default — agent A (Prometheus
	// metrics_handler.go) and agent B (ratelimit_handler.go) define the
	// concrete types and instantiate these fields in New() below. Keeping
	// them as interface{} pointers here means handler.go itself does not
	// import prometheus or x/time/rate; the optional dependencies stay
	// confined to their owning files.
	metrics any // Phase 4-A: *metricsRegistry (defined in metrics_handler.go)
	limiter any // Phase 4-B: *rateLimiter     (defined in ratelimit_handler.go)
}

// New creates a Handler bound to the given configuration. The store argument
// may be nil to disable history persistence entirely; callers that want
// history must pass a non-nil store.Store.
//
// If cfg.MaxConcurrent is zero (e.g. in tests that build Config directly),
// a default of 10 is used so the semaphore is always functional.
func New(cfg *config.Config, s store.Store) *Handler {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	h := &Handler{
		cfg:       cfg,
		sem:       make(chan struct{}, maxConcurrent),
		store:     s,
		startedAt: time.Now(),
	}
	// === [P4-A: metrics init] === — agent A constructs *metricsRegistry,
	// registers collectors with promauto, and assigns h.metrics = reg.
	// === [P4-A end] ===

	// === [P4-B: limiter init] === — agent B constructs *rateLimiter when
	// cfg.RatePerMin > 0 (no-op otherwise), starts the eviction goroutine,
	// and assigns h.limiter = rl.
	// === [P4-B end] ===
	return h
}

// acquire tries to take a slot from the concurrency semaphore.
// Returns true on success, false when the server is at capacity.
// On success acceptedTotal is incremented; on failure rejectedTotal is.
func (h *Handler) acquire() bool {
	select {
	case h.sem <- struct{}{}:
		h.acceptedTotal.Add(1)
		return true
	default:
		h.rejectedTotal.Add(1)
		return false
	}
}

func (h *Handler) release() { <-h.sem }

// historyEnabled reports whether the persistence backend is active.
// Exposed to /api/config so the frontend can hide history UI when off.
func (h *Handler) historyEnabled() bool { return h.store != nil }

// ── /api/config ───────────────────────────────────────────────────────────

type configResponse struct {
	Mode           string `json:"mode"`
	DurationSecs   int    `json:"durationSecs"`
	DownloadMB     int    `json:"downloadMB"`
	UploadMB       int    `json:"uploadMB"`
	Streams        int    `json:"streams"`
	// Phase 2-3 additions — let the frontend tailor its behaviour:
	//   warmupMs:       milliseconds of throughput samples F1 should discard
	//                   at the start of download/upload (slow-start trim).
	//   historyEnabled: true iff the server has a working SQLite store;
	//                   F3 hides history/trends UI when false.
	WarmupMs       int  `json:"warmupMs"`
	HistoryEnabled bool `json:"historyEnabled"`
}

// ConfigHandler exposes the server-side test configuration so the frontend
// can adapt its measurement strategy accordingly.
func (h *Handler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, configResponse{
		Mode:           string(h.cfg.Mode),
		DurationSecs:   int(h.cfg.Duration.Seconds()),
		DownloadMB:     h.cfg.DownloadMB,
		UploadMB:       h.cfg.UploadMB,
		Streams:        h.cfg.Streams,
		WarmupMs:       h.cfg.WarmupMs,
		HistoryEnabled: h.historyEnabled(),
	})
}

// ── /api/ip ───────────────────────────────────────────────────────────────

type ipResponse struct {
	IP string `json:"ip"`
}

// IPHandler returns the client's IP address as JSON.
func (h *Handler) IPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, ipResponse{IP: ClientIP(r)})
}

// ── /api/ping ─────────────────────────────────────────────────────────────

// PingHandler is a minimal latency probe: tiny response, no caching.
func (h *Handler) PingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok")) //nolint:errcheck
}

// ── /api/download ─────────────────────────────────────────────────────────

// maxDurationSecs is the upper bound for the ?duration query parameter (5 minutes).
const maxDurationSecs = 300

// DownloadHandler streams random bytes to the client so it can measure
// download throughput. Two modes:
//
//   - ModeSize: streams exactly cfg.DownloadMB megabytes (or ?bytes=N override).
//   - ModeTime: streams continuously for cfg.Duration seconds (or ?duration=N override).
func (h *Handler) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.acquire() {
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}
	defer h.release()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")

	// Allow frontend to override size per-request. `bytes` is used when the UI
	// splits a size-mode test across multiple parallel streams.
	// Cap at maxBytesPerStream (1 GB) to prevent abuse.
	totalBytes := h.cfg.DownloadMB * 1024 * 1024
	if s := r.URL.Query().Get("bytes"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= maxBytesPerStream {
			totalBytes = n
		}
	} else if s := r.URL.Query().Get("size"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 1024 {
			totalBytes = n * 1024 * 1024
		}
	}

	// If the client explicitly requested a specific byte count (size mode),
	// honour it regardless of the server's default mode setting.
	if r.URL.Query().Get("bytes") != "" || r.URL.Query().Get("size") != "" {
		h.downloadBySize(w, totalBytes)
		return
	}

	// Allow frontend to override the test duration for time-mode tests.
	// This lets the UI honour the duration the user selected (5/10/15/30/60 s)
	// independently of the server's SPEEDTEST_DURATION environment variable.
	duration := h.cfg.Duration
	if s := r.URL.Query().Get("duration"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= maxDurationSecs {
			duration = time.Duration(n) * time.Second
		}
	}

	switch h.cfg.Mode {
	case config.ModeTime:
		h.downloadByTime(w, duration)
	default:
		h.downloadBySize(w, totalBytes)
	}
}

func (h *Handler) downloadBySize(w http.ResponseWriter, total int) {
	w.Header().Set("Content-Length", intStr(total))

	written := 0
	for written < total {
		n := len(randomPayload)
		if written+n > total {
			n = total - written
		}
		if _, err := w.Write(randomPayload[:n]); err != nil {
			return
		}
		written += n
	}
}

func (h *Handler) downloadByTime(w http.ResponseWriter, duration time.Duration) {
	// No Content-Length: chunked transfer encoding until deadline.
	deadline := time.Now().Add(duration)
	flusher, canFlush := w.(http.Flusher)

	for time.Now().Before(deadline) {
		if _, err := w.Write(randomPayload[:]); err != nil {
			return
		}
		if canFlush {
			flusher.Flush()
		}
	}
}

// ── /api/upload ───────────────────────────────────────────────────────────

type uploadResponse struct {
	Received int64 `json:"received"`
}

// UploadHandler discards the request body and returns the byte count so the
// client can compute upload throughput. Only POST is accepted.
// The body is capped at maxUploadBytes to prevent unbounded resource use.
func (h *Handler) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.acquire() {
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}
	defer h.release()

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	received, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		// MaxBytesReader returns an error when the limit is exceeded.
		// Return 413 immediately; the partial byte count is not reported
		// because the client exceeded the hard cap and the result would
		// be meaningless for throughput measurement.
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	writeJSON(w, uploadResponse{Received: received})
}

// ── ClientIP (package-level, used by tests directly) ─────────────────────

// ClientIP extracts the real client IP. Proxy headers (X-Forwarded-For,
// X-Real-Ip) are only trusted when the direct peer address is a loopback
// or private (RFC 1918 / RFC 4193) address, indicating the request arrived
// through a trusted local reverse proxy.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if isPrivateOrLoopback(host) {
		// The forwarded headers are attacker-controllable in any deployment
		// where a proxy header is forwarded verbatim — even from a private
		// peer. Validate the value parses as a real IP before accepting it,
		// otherwise fall back to the direct peer. This stops the spoofed
		// value from polluting logs / DB rows / CSV exports.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			candidate := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			if parsed := net.ParseIP(candidate); parsed != nil {
				return parsed.String()
			}
		}
		if xri := r.Header.Get("X-Real-Ip"); xri != "" {
			if parsed := net.ParseIP(strings.TrimSpace(xri)); parsed != nil {
				return parsed.String()
			}
		}
	}

	return host
}

// privateCIDRs holds pre-parsed RFC-1918 and RFC-4193 private address ranges.
// Parsing once at init time avoids repeated allocations on every ClientIP call.
var privateCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7",
	} {
		_, network, _ := net.ParseCIDR(cidr)
		privateCIDRs = append(privateCIDRs, network)
	}
}

// isPrivateOrLoopback reports whether ip is a loopback or RFC-1918/4193
// private address and can therefore be trusted as a reverse-proxy peer.
func isPrivateOrLoopback(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, network := range privateCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ── helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func intStr(n int) string {
	return strconv.Itoa(n)
}
