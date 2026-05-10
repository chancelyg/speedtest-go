package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
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

func main() {
	cfg := config.Load()

	log.Printf("mode=%s  download=%dMB  upload=%dMB  duration=%v  streams=%d  maxConcurrent=%d  listen=%s",
		cfg.Mode, cfg.DownloadMB, cfg.UploadMB, cfg.Duration, cfg.Streams, cfg.MaxConcurrent, cfg.Addr())

	mux := buildMux(cfg)

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // disabled: download/upload streams run longer than any fixed timeout
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown: wait for SIGINT or SIGTERM, then drain in-flight
	// connections with a 30-second deadline so ongoing speed-test streams
	// can complete rather than being abruptly cut.
	idleConnsClosed := make(chan struct{})
	go func() {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)
		<-sigch
		log.Println("shutting down — draining connections (max 30 s)…")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		close(idleConnsClosed)
	}()

	log.Printf("Speedtest server listening on http://%s", cfg.Addr())
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	<-idleConnsClosed
	log.Println("server stopped")
}

func buildMux(cfg *config.Config) *http.ServeMux {
	h := handler.New(cfg)
	mux := http.NewServeMux()

	// Static assets embedded in the binary.
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	// API
	mux.HandleFunc("/api/config", h.ConfigHandler)
	mux.HandleFunc("/api/ip", h.IPHandler)
	mux.HandleFunc("/api/ping", h.PingHandler)
	mux.HandleFunc("/api/download", h.DownloadHandler)
	mux.HandleFunc("/api/upload", h.UploadHandler)

	return mux
}

// loggingMiddleware logs each request with method, path, remote addr,
// response status, and elapsed time.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("%s %s %s %d %s",
			r.Method, r.URL.Path, r.RemoteAddr, lw.status, time.Since(start).Round(time.Millisecond))
	})
}

// statusWriter wraps http.ResponseWriter to capture the written status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
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
