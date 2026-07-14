package handler

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	h.metrics = newMetricsRegistry(h.sem)
	// === [P4-A end] ===

	// === [P4-B: limiter init] === — newRateLimiter returns nil when
	// cfg.RatePerMin <= 0, which is the default. In that mode RateLimit is a
	// transparent pass-through and no GC goroutine is started, so a vanilla
	// single-machine deployment pays zero runtime cost.
	h.limiter = newRateLimiter(cfg.RatePerMin)
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
		if reg, ok := h.metrics.(*metricsRegistry); ok && reg != nil {
			reg.recordAdmission(true)
		}
		return true
	default:
		h.rejectedTotal.Add(1)
		if reg, ok := h.metrics.(*metricsRegistry); ok && reg != nil {
			reg.recordAdmission(false)
		}
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
	//   maxConcurrent:  server-wide semaphore capacity; the frontend caps its
	//                   streams selector at this value so a slow-link test
	//                   never fires more requests than the server will admit
	//                   (surplus streams would 503 immediately and leave
	//                   orphaned in-flight sibling streams driving the gauge).
	WarmupMs       int  `json:"warmupMs"`
	HistoryEnabled bool `json:"historyEnabled"`
	MaxConcurrent  int  `json:"maxConcurrent"`
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
		// cap(h.sem) is authoritative: Handler.New coerces cfg.MaxConcurrent<=0
		// to a sensible default of 10 when sizing the semaphore, so mirroring
		// the raw cfg field here would report 0 for callers that used the
		// zero-value config in tests.
		MaxConcurrent:  cap(h.sem),
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

	// Record bytes streamed and wall-clock duration regardless of how the
	// handler exits. Tracked locally so that early client disconnects still
	// produce useful observability.
	started := time.Now()
	var sent int64
	defer func() { h.ObserveTest("down", sent, time.Since(started)) }()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Identity hints stop transparent gzip-aware proxies from attempting to
	// compress our random payload, which would skew throughput measurements.
	w.Header().Set("Content-Encoding", "identity")

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
		sent = h.downloadBySize(w, totalBytes)
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
		sent = h.downloadByTime(w, duration)
	default:
		sent = h.downloadBySize(w, totalBytes)
	}
}

func (h *Handler) downloadBySize(w http.ResponseWriter, total int) int64 {
	w.Header().Set("Content-Length", intStr(total))

	var written int64
	for int(written) < total {
		n := len(randomPayload)
		if int(written)+n > total {
			n = total - int(written)
		}
		m, err := w.Write(randomPayload[:n])
		written += int64(m)
		if err != nil {
			return written
		}
	}
	return written
}

// writeChunk caps the per-Write payload. Smaller chunks keep individual
// Writes short on slow links so the deadline check (and the
// SetWriteDeadline on the underlying conn) can fire promptly — a single
// 1 MB Write on a 256 Kbps uplink drains for ~30 s, which is what made
// time-mode tests blow past their configured duration before this fix.
const writeChunk = 64 << 10 // 64 KB

// writeDeadlineGrace is the slack added to the user-selected duration
// before the kernel-level write deadline fires. The natural exit is the
// loop-top deadline check; the grace lets the final Flush land on healthy
// links and only kicks in as a hard cap when a Write is stuck behind a
// full TCP send buffer.
const writeDeadlineGrace = 2 * time.Second

func (h *Handler) downloadByTime(w http.ResponseWriter, duration time.Duration) int64 {
	// No Content-Length: chunked transfer encoding until deadline.
	deadline := time.Now().Add(duration)
	flusher, canFlush := w.(http.Flusher)

	// SetWriteDeadline gives us a hard cap: when the deadline passes any
	// in-flight Write blocked on a full TCP send buffer returns with a
	// timeout error, so a slow-link 15 s test ends within deadline + grace
	// rather than waiting for the kernel to drain the buffer. ErrNotSupported
	// is expected in tests (httptest.ResponseRecorder doesn't implement it)
	// and is the only error we silently tolerate — anything else is logged
	// because it'd mean the deadline isn't enforced on this connection.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(deadline.Add(writeDeadlineGrace)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		slog.Warn("downloadByTime: SetWriteDeadline failed", "err", err.Error())
	}

	var written int64
	chunk := randomPayload[:writeChunk]
	for time.Now().Before(deadline) {
		m, err := w.Write(chunk)
		written += int64(m)
		if err != nil {
			// Deadline-induced timeout is the expected exit path on slow
			// links — treat the bytes already on the wire as the answer.
			return written
		}
		if canFlush {
			flusher.Flush()
		}
	}
	return written
}

// ── /api/upload ───────────────────────────────────────────────────────────

type uploadResponse struct {
	Received        int64 `json:"received"`
	ServerElapsedMs int64 `json:"serverElapsedMs"`
	// Truncated is true when the body hit maxUploadBytes mid-stream. The
	// client should still treat Received/ServerElapsedMs as a valid
	// throughput sample, but may want to surface that the cap was reached.
	Truncated bool `json:"truncated,omitempty"`
}

// UploadHandler discards the request body and returns the byte count so the
// client can compute upload throughput. Only POST is accepted.
// The body is capped at maxUploadBytes to prevent unbounded resource use.
//
// When the body exceeds the cap, the handler still responds 200 with the
// partial byte count and a Truncated flag. Returning 413 (the previous
// behaviour) made gigabit-class uploads look like outright failures once they
// crossed the 10 GB cap, even though the wire bytes already constitute a
// valid throughput sample.
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

	started := time.Now()
	var received int64
	defer func() { h.ObserveTest("up", received, time.Since(started)) }()

	// Identity hints prevent transparent reverse proxies from gzip-encoding
	// our random payload mid-stream, which would distort the throughput
	// measurement. The bytes themselves are incompressible, but the
	// header negotiation removes any ambiguity.
	w.Header().Set("Content-Encoding", "identity")

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	n, err := io.Copy(io.Discard, r.Body)
	received = n

	var truncated bool
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			// Hard cap reached. Report the bytes we did receive so the client
			// can still compute throughput, and flag the truncation.
			truncated = true
		} else {
			// Other body errors (client disconnect, malformed framing, …) are
			// reported as a server error so the client can retry.
			http.Error(w, "upload failed", http.StatusBadRequest)
			return
		}
	}

	writeJSON(w, uploadResponse{
		Received:        received,
		ServerElapsedMs: time.Since(started).Milliseconds(),
		Truncated:       truncated,
	})
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
