package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
	"speedtest-go/internal/store"
)

//go:embed static
var staticFiles embed.FS

// Build metadata injected at link time by goreleaser via -ldflags
// "-X main.version=... -X main.commit=... -X main.date=...". The "dev"
// defaults make `go run` / local builds visibly distinct from release
// artifacts without requiring any extra build steps.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// ctxKey is an unexported type used for context keys to avoid collisions with
// any package that also stores values in request contexts.
type ctxKey struct{ name string }

// requestIDKey is the context key under which the per-request ID is stored by
// loggingMiddleware. Downstream handlers may retrieve it via
// r.Context().Value(requestIDKey).(string) once they opt in.
var requestIDKey = ctxKey{name: "request_id"}

func main() {
	// === [P4-C: CLI flag parsing] ===
	// Layered configuration: CLI flags > env vars > JSON config file >
	// compiled-in defaults. LoadWithSources is pure (no os.Exit, no stderr)
	// so we handle --version / --help / parse errors at the call site below.
	cfg, opts, err := config.LoadWithSources(os.Args[1:], os.Getenv)
	if err != nil {
		// --help is signalled by flag.ErrHelp; treat it as a clean exit and
		// re-parse with stderr output so the user sees the auto-generated
		// usage. Anything else is a real parse / config error.
		if isHelpErr(err) {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "speedtest: "+err.Error())
		os.Exit(2)
	}
	// === [P4-C end] ===

	// === [P4-E: version flag prints] ===
	if opts.ShowVersion {
		fmt.Printf("speedtest-go %s (commit %s, built %s)\n", version, commit, date)
		os.Exit(0)
	}
	// === [P4-E end] ===

	// Initialise the default structured logger. JSON output to stdout makes
	// the binary friendly to log shippers (Loki, Vector, etc.) without
	// requiring any external runtime dependency.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("startup config",
		"mode", string(cfg.Mode),
		"download_mb", cfg.DownloadMB,
		"upload_mb", cfg.UploadMB,
		"duration", cfg.Duration.String(),
		"streams", cfg.Streams,
		"max_concurrent", cfg.MaxConcurrent,
		"listen", cfg.Addr(),
	)

	// Open the SQLite history store when configured. The store is *optional*:
	// if SPEEDTEST_DB_PATH is empty we skip opening, and if Open() fails we
	// log a warning and continue in stateless mode rather than refusing to
	// start the server. Speed-testing must still work without persistence.
	var historyStore store.Store
	if cfg.DBPath != "" {
		s, err := store.Open(cfg.DBPath)
		if err != nil {
			slog.Warn("history disabled", "db_path", cfg.DBPath, "err", err.Error())
		} else {
			historyStore = s
			slog.Info("history enabled", "db_path", cfg.DBPath)
			if cfg.HistoryRetentionDays > 0 {
				cutoff := time.Now().Add(-time.Duration(cfg.HistoryRetentionDays) * 24 * time.Hour).UnixMilli()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				n, perr := historyStore.PruneOlderThan(ctx, cutoff)
				cancel()
				if perr != nil {
					slog.Warn("history prune failed", "err", perr.Error())
				} else if n > 0 {
					slog.Info("history pruned", "rows", n, "older_than_days", cfg.HistoryRetentionDays)
				}
			}
		}
	}

	srv := newServerWithStore(cfg, historyStore)

	// Graceful shutdown: wait for SIGINT or SIGTERM, then drain in-flight
	// connections with a 30-second deadline so ongoing speed-test streams
	// can complete rather than being abruptly cut. The history store is
	// closed *after* Shutdown returns so handlers can still write the final
	// in-flight POST /api/results body.
	idleConnsClosed := make(chan struct{})
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)
		<-sigch
		slog.Info("shutting down, draining connections", "timeout", "30s")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "err", err.Error())
		}
		if historyStore != nil {
			if err := historyStore.Close(); err != nil {
				slog.Error("store close error", "err", err.Error())
			}
		}
		close(idleConnsClosed)
	}()

	slog.Info("listening", "addr", cfg.Addr(), "url", "http://"+cfg.Addr())
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "err", err.Error())
		os.Exit(1)
	}
	<-idleConnsClosed
	slog.Info("server stopped")
}

// newServer constructs the speedtest HTTP server with no history store
// configured. Retained for the existing TestServerAllowsLongUploadsAndDownloads
// test which only needs the timeout contract.
//
//   - ReadHeaderTimeout protects against slow-header (slowloris) attacks.
//   - ReadTimeout is intentionally disabled because size-mode uploads of
//     multi-GB bodies can legitimately exceed any fixed deadline; body size
//     is bounded by maxUploadBytes and time by maxDurationSecs in the handler.
//   - WriteTimeout is intentionally disabled for the same reason on the
//     response side (time-mode downloads stream for up to 5 minutes).
//   - IdleTimeout reaps keep-alive connections between requests.
func newServer(cfg *config.Config) *http.Server {
	return newServerWithStore(cfg, nil)
}

// idleTimeout returns a keep-alive idle timeout long enough that a maximum-
// length test (?duration=300) plus the surrounding multi-phase orchestration
// (idle ping → download → loaded ping → upload) cannot be cut off by the
// server. The 60 s margin accommodates inter-phase gaps and pre-flight
// requests reusing the same connection.
func idleTimeout(testDuration time.Duration) time.Duration {
	const maxTestDuration = 300 * time.Second
	if testDuration < maxTestDuration {
		testDuration = maxTestDuration
	}
	return testDuration + 60*time.Second
}

// newServerWithStore is the full constructor used by main(). Tests that need
// the history endpoints wired in pass a non-nil store.
func newServerWithStore(cfg *config.Config, s store.Store) *http.Server {
	mux, h := buildMux(cfg, s)

	// === [P4-B: rate-limit middleware wrap] ===
	// Per-IP rate limiting wraps the mux. When cfg.RatePerMin <= 0 the
	// limiter is nil and h.RateLimit becomes a transparent pass-through,
	// which is the default single-machine-deployment behaviour.
	rateLimited := h.RateLimit(mux)
	// === [P4-B end] ===

	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           loggingMiddleware(h, rateLimited),
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		// IdleTimeout must outlive the longest possible test. maxDurationSecs
		// (300 s) is the hard ceiling for ?duration=, and the UI can run
		// multi-phase tests (idle ping → download → loaded ping → upload) over
		// the same keep-alive connection, so we add a 60 s safety margin.
		// A flat 120 s here would silently close mid-test on long runs.
		IdleTimeout: idleTimeout(cfg.Duration),
	}
}

func buildMux(cfg *config.Config, s store.Store) (*http.ServeMux, *handler.Handler) {
	h := handler.New(cfg, s)
	// Wire the ldflag-injected build metadata into the handler so /api/config
	// and /healthz can surface it. Local `go run` leaves the defaults ("dev"
	// / "none" / "unknown") which are legal — versionOrDev() maps "dev" to
	// the same sentinel for consistency across endpoints.
	h.Build = handler.BuildInfo{Version: version, Commit: commit, Date: date}
	mux := http.NewServeMux()

	// Static assets embedded in the binary.
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		slog.Error("embed sub", "err", err.Error())
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	// favicon.ico: prefer a custom file placed next to the binary at runtime;
	// fall back to the default icon embedded in static/favicon.ico.
	mux.HandleFunc("/favicon.ico", faviconHandler(sub))

	// sw.js is served through a version-injecting handler so CACHE_NAME
	// changes on every release without anyone having to remember to bump a
	// constant in the source. The token substitution happens once at boot;
	// per-request cost is a single map lookup + write.
	mux.HandleFunc("/sw.js", swjsHandler(sub, version))

	// API — speed test endpoints.
	mux.HandleFunc("/api/config", h.ConfigHandler)
	mux.HandleFunc("/api/ip", h.IPHandler)
	mux.HandleFunc("/api/ping", h.PingHandler)
	mux.HandleFunc("/api/download", h.DownloadHandler)
	mux.HandleFunc("/api/upload", h.UploadHandler)

	// Health probe — always wired; respects history availability.
	mux.HandleFunc("/healthz", h.HealthHandler)

	// === [P4-A: /metrics route] ===
	// Prometheus exposition endpoint. Kept at the conventional `/metrics`
	// path so default scrape configs Just Work without per-target overrides.
	mux.HandleFunc("/metrics", h.MetricsHandler)
	// === [P4-A end] ===

	// Results history. /api/results/export is registered before /api/results
	// so the longest-matching pattern wins (Go ServeMux semantics).
	mux.HandleFunc("/api/results/export", h.ResultsExport)
	mux.HandleFunc("/api/results", h.ResultsListOrCreate)

	return mux, h
}

// swjsCachePlaceholder is the token that static/sw.js contains verbatim in
// its CACHE_NAME assignment; swjsHandler replaces it with the running
// build's cache-name string so PWA installs on the previous release get
// invalidated automatically on the first fetch after upgrade.
const swjsCachePlaceholder = "__SPEEDTEST_CACHE_NAME__"

// swjsHandler reads the embedded sw.js once at boot, substitutes the
// CACHE_NAME placeholder with `speedtest-<version>`, and serves the result.
// Doing the substitution at boot rather than per-request keeps the hot path
// a single Write call and means an unparseable embed would surface at
// startup rather than deep inside a request.
func swjsHandler(embedded fs.FS, version string) http.HandlerFunc {
	raw, err := fs.ReadFile(embedded, "sw.js")
	if err != nil {
		// Broken embed means the binary is misassembled; surface it clearly
		// per-request rather than crashing the whole server on startup.
		slog.Error("sw.js: read embedded", "err", err.Error())
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "sw.js unavailable", http.StatusInternalServerError)
		}
	}
	body := bytes.ReplaceAll(raw, []byte(swjsCachePlaceholder), []byte("speedtest-"+version))
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		// Browsers do byte-for-byte SW comparison on the update check;
		// no-cache keeps intermediary proxies from serving a stale copy.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}

// faviconHandler returns a handler that serves ./favicon.ico from the working
// directory when it exists, and otherwise falls back to the embedded default.
func faviconHandler(embedded fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const name = "favicon.ico"
		if data, err := os.ReadFile(name); err == nil {
			w.Header().Set("Content-Type", "image/x-icon")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(data) //nolint:errcheck
			return
		}
		// Fall back to the embedded icon.
		http.ServeFileFS(w, r, embedded, name)
	}
}

// newRequestID returns a 32-character hex string (16 random bytes) used to
// correlate access logs with client-side telemetry. Falls back to a
// timestamp-derived ID if the system entropy source is unavailable, which
// keeps the logger from blocking startup in pathological environments.
func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Extremely unlikely: crypto/rand failing means the OS RNG is
		// unavailable. Degrade gracefully rather than panicking inside a
		// hot request path.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf[:])
}

// loggingMiddleware emits a structured JSON access log for every request and
// stamps an X-Request-Id header on the response so clients can correlate
// failures with server-side records. The request ID is also injected into the
// request context under requestIDKey for downstream handlers.
func loggingMiddleware(h *handler.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := newRequestID()
		w.Header().Set("X-Request-Id", reqID)

		// Count bytes read from the request body. The handler may further
		// wrap the body (e.g. http.MaxBytesReader), but every Read still
		// flows through this counter first.
		cr := &countingReader{rc: r.Body}
		r.Body = cr

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(sw, r.WithContext(ctx))

		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes_sent", sw.bytesSent,
			"bytes_recv", cr.bytesRecv,
			"request_id", reqID,
			"user_agent", r.UserAgent(),
		)

		// Phase 4-A: record the request in the Prometheus registry.
		// h may be nil in tests that build a bare middleware chain; the
		// metrics observation is then a no-op.
		if h != nil {
			h.ObserveRequest(r.Method, r.URL.Path, strconv.Itoa(sw.status))
		}
	})
}

// statusWriter wraps http.ResponseWriter to capture both the status code and
// the number of bytes written to the body. It also explicitly forwards
// Flush() so chunked-streaming handlers (e.g. downloadByTime) can push bytes
// through the middleware wrapper without the underlying http.Flusher being
// hidden by the embedding.
type statusWriter struct {
	http.ResponseWriter
	status     int
	bytesSent  int64
	hdrWritten bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.hdrWritten {
		sw.status = code
		sw.hdrWritten = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.hdrWritten {
		// Mirror net/http's implicit 200 on first Write without a prior
		// WriteHeader, so the logged status matches what the client sees.
		sw.hdrWritten = true
	}
	n, err := sw.ResponseWriter.Write(b)
	sw.bytesSent += int64(n)
	return n, err
}

// Flush implements http.Flusher so that handlers using chunked streaming
// (e.g. downloadByTime) can push bytes to the client through the middleware
// wrapper. Without this, the type assertion w.(http.Flusher) in
// downloadByTime would always fail when the middleware is active, silently
// preventing chunk flushing and stalling streaming downloads.
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the wrapped ResponseWriter so http.NewResponseController
// can reach the underlying *http.response and call SetWriteDeadline on the
// real connection. Without this the deadline used by downloadByTime to
// bound time-mode tests on slow links would silently no-op behind the
// middleware (ErrNotSupported), and a 15 s test could run for 30+ s.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

// countingReader wraps an io.ReadCloser and tallies bytes read. Used by the
// logging middleware to record request-body sizes (upload bytes) without
// requiring the inner handler to opt in.
type countingReader struct {
	rc        io.ReadCloser
	bytesRecv int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.rc == nil {
		return 0, io.EOF
	}
	n, err := c.rc.Read(p)
	c.bytesRecv += int64(n)
	return n, err
}

func (c *countingReader) Close() error {
	if c.rc == nil {
		return nil
	}
	return c.rc.Close()
}

// isHelpErr reports whether err originated from flag.ErrHelp, i.e. the user
// passed -h / --help. The config package wraps the original error with %w so
// errors.Is unwraps correctly.
func isHelpErr(err error) bool {
	return errors.Is(err, flag.ErrHelp)
}

// printUsage writes the same flag descriptions the user would see for any
// other CLI tool. We build a one-shot FlagSet here purely for its Usage()
// formatting; LoadWithSources owns the real parser and stays test-friendly.
func printUsage() {
	fs := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("config", "", "Path to JSON config file (overrides SPEEDTEST_CONFIG and search paths)")
	fs.String("host", "", "Bind address (default \"::\")")
	fs.String("port", "", "Listen port (default \"8080\")")
	fs.String("mode", "", "Speed-test mode: time | size")
	fs.Int("duration", 0, "Time-mode duration in seconds")
	fs.Int("streams", 0, "Parallel stream count")
	fs.Int("download-mb", 0, "Download size in MB (size mode)")
	fs.Int("upload-mb", 0, "Upload size in MB (size mode)")
	fs.Int("max-concurrent", 0, "Max simultaneous tests across all clients")
	fs.Int("warmup-ms", 0, "Throughput slow-start trim in milliseconds")
	fs.String("db-path", "", "SQLite history path (\"\" disables persistence)")
	fs.Int("rate-per-min", 0, "Per-IP rate limit (req/min, 0 = unlimited)")
	fs.Bool("version", false, "Print version information and exit")
	fmt.Fprintln(os.Stderr, "Usage: speedtest [flags]")
	fs.PrintDefaults()
}
