package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
)

//go:embed static
var staticFiles embed.FS

// ctxKey is an unexported type used for context keys to avoid collisions with
// any package that also stores values in request contexts.
type ctxKey struct{ name string }

// requestIDKey is the context key under which the per-request ID is stored by
// loggingMiddleware. Downstream handlers may retrieve it via
// r.Context().Value(requestIDKey).(string) once they opt in.
var requestIDKey = ctxKey{name: "request_id"}

func main() {
	// Initialise the default structured logger. JSON output to stdout makes
	// the binary friendly to log shippers (Loki, Vector, etc.) without
	// requiring any external runtime dependency.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()

	slog.Info("startup config",
		"mode", string(cfg.Mode),
		"download_mb", cfg.DownloadMB,
		"upload_mb", cfg.UploadMB,
		"duration", cfg.Duration.String(),
		"streams", cfg.Streams,
		"max_concurrent", cfg.MaxConcurrent,
		"listen", cfg.Addr(),
	)

	srv := newServer(cfg)

	// Graceful shutdown: wait for SIGINT or SIGTERM, then drain in-flight
	// connections with a 30-second deadline so ongoing speed-test streams
	// can complete rather than being abruptly cut.
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

// newServer constructs the speedtest HTTP server with timeouts tuned for
// long-running download/upload streams.
//
//   - ReadHeaderTimeout protects against slow-header (slowloris) attacks.
//   - ReadTimeout is intentionally disabled because size-mode uploads of
//     multi-GB bodies can legitimately exceed any fixed deadline; body size
//     is bounded by maxUploadBytes and time by maxDurationSecs in the handler.
//   - WriteTimeout is intentionally disabled for the same reason on the
//     response side (time-mode downloads stream for up to 5 minutes).
//   - IdleTimeout reaps keep-alive connections between requests.
func newServer(cfg *config.Config) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           loggingMiddleware(buildMux(cfg)),
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
}

func buildMux(cfg *config.Config) *http.ServeMux {
	h := handler.New(cfg)
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

	// API
	mux.HandleFunc("/api/config", h.ConfigHandler)
	mux.HandleFunc("/api/ip", h.IPHandler)
	mux.HandleFunc("/api/ping", h.PingHandler)
	mux.HandleFunc("/api/download", h.DownloadHandler)
	mux.HandleFunc("/api/upload", h.UploadHandler)

	return mux
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
func loggingMiddleware(next http.Handler) http.Handler {
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
