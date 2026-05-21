package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"speedtest-go/internal/config"
)

// TestServerAllowsLongUploadsAndDownloads guards the timeout contract that
// makes long-running speed-test streams possible:
//
//   - ReadHeaderTimeout must be set so slow-header (slowloris) attacks are
//     still rejected.
//   - ReadTimeout must be 0 (disabled) because it covers the entire request
//     body. A 30 s cap kills any size-mode upload that takes longer than
//     30 s — which is the common case on links slower than ~3 Gbps.
//   - WriteTimeout must be 0 (disabled) because time-mode downloads stream
//     for up to maxDurationSecs (5 minutes).
//
// Bounds on body size and stream duration are enforced inside the handlers
// (maxUploadBytes, maxDurationSecs), not via these socket-level timeouts.
func TestServerAllowsLongUploadsAndDownloads(t *testing.T) {
	cfg := &config.Config{
		Host:          "127.0.0.1",
		Port:          "0",
		Mode:          config.ModeTime,
		Duration:      15 * time.Second,
		DownloadMB:    25,
		UploadMB:      10,
		Streams:       4,
		MaxConcurrent: 10,
	}
	srv := newServer(cfg)

	if srv.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0 (must not cap long upload bodies)", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (must not cap time-mode download streams)", srv.WriteTimeout)
	}
	if srv.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout = 0, want > 0 (slow-header protection required)")
	}
	if srv.ReadHeaderTimeout > 60*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want a tight cap (≤ 60 s) for slowloris protection", srv.ReadHeaderTimeout)
	}
}

// withCapturedLogger installs a slog JSON handler that writes into the
// returned buffer for the duration of the test, and restores the previous
// default logger when the test ends.
func withCapturedLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	prev := slog.Default()
	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// lastJSONLine returns the parsed last non-empty JSON record from buf. It
// trims trailing newlines so the caller can write straightforward assertions
// against a single record.
func lastJSONLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("no log lines captured")
	}
	last := lines[len(lines)-1]
	out := map[string]any{}
	if err := json.Unmarshal(last, &out); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, string(last))
	}
	return out
}

// TestLoggingMiddlewareEmitsJSON checks that loggingMiddleware writes one
// structured JSON record per request and that every contract field is
// present with the expected type. This is the contract downstream log
// shippers will rely on, so the field set is asserted explicitly.
func TestLoggingMiddlewareEmitsJSON(t *testing.T) {
	buf := withCapturedLogger(t)

	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/ping?foo=1", nil)
	req.Header.Set("User-Agent", "unit-test/1.0")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	rec2 := lastJSONLine(t, buf)

	required := map[string]string{
		"method":      "string",
		"path":        "string",
		"remote_addr": "string",
		"status":      "number",
		"duration_ms": "number",
		"bytes_sent":  "number",
		"bytes_recv":  "number",
		"request_id":  "string",
		"user_agent":  "string",
		"msg":         "string",
	}
	for field, kind := range required {
		v, ok := rec2[field]
		if !ok {
			t.Errorf("log record missing field %q", field)
			continue
		}
		switch kind {
		case "string":
			if _, ok := v.(string); !ok {
				t.Errorf("field %q = %v (%T), want string", field, v, v)
			}
		case "number":
			if _, ok := v.(float64); !ok {
				t.Errorf("field %q = %v (%T), want number", field, v, v)
			}
		}
	}

	if got := rec2["method"]; got != http.MethodGet {
		t.Errorf("method = %v, want GET", got)
	}
	if got := rec2["path"]; got != "/api/ping" {
		t.Errorf("path = %v, want /api/ping", got)
	}
	if got := rec2["status"].(float64); int(got) != http.StatusTeapot {
		t.Errorf("status = %v, want %d", got, http.StatusTeapot)
	}
	if got := rec2["user_agent"]; got != "unit-test/1.0" {
		t.Errorf("user_agent = %v, want unit-test/1.0", got)
	}
}

// TestLoggingMiddlewareCountsBytes confirms bytes_sent reflects the response
// body length and bytes_recv reflects the request body length, even when the
// inner handler discards the body via io.Copy(io.Discard, ...).
func TestLoggingMiddlewareCountsBytes(t *testing.T) {
	buf := withCapturedLogger(t)

	const reqBody = "abcdefghij"            // 10 bytes
	const respBody = "0123456789ABCDEFGHIJ" // 20 bytes

	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body so countingReader registers every byte.
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(respBody))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	rec2 := lastJSONLine(t, buf)
	if got := int(rec2["bytes_sent"].(float64)); got != len(respBody) {
		t.Errorf("bytes_sent = %d, want %d", got, len(respBody))
	}
	if got := int(rec2["bytes_recv"].(float64)); got != len(reqBody) {
		t.Errorf("bytes_recv = %d, want %d", got, len(reqBody))
	}
}

// TestRequestIDHeader verifies that every response carries a 32-character
// lowercase hex X-Request-Id header so clients can correlate failures with
// the corresponding server access-log entry.
func TestRequestIDHeader(t *testing.T) {
	_ = withCapturedLogger(t)

	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-Id")
	if len(id) != 32 {
		t.Fatalf("X-Request-Id length = %d, want 32 (got %q)", len(id), id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("X-Request-Id is not valid hex: %v", err)
	}
}

// TestRequestIDIsUniquePerRequest sanity-checks that two consecutive
// requests do not collide on the request ID.
func TestRequestIDIsUniquePerRequest(t *testing.T) {
	_ = withCapturedLogger(t)

	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	ids := map[string]struct{}{}
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-Id")
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate X-Request-Id across requests: %s", id)
		}
		ids[id] = struct{}{}
	}
}

// TestRequestIDInContext confirms the middleware injects the request ID into
// the request context under requestIDKey, ready for downstream handlers to
// pick up without further wiring.
func TestRequestIDInContext(t *testing.T) {
	_ = withCapturedLogger(t)

	var seen string
	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(requestIDKey).(string); ok {
			seen = v
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen == "" {
		t.Fatal("handler did not see a request_id in context")
	}
	if seen != rec.Header().Get("X-Request-Id") {
		t.Errorf("ctx request_id %q != header %q", seen, rec.Header().Get("X-Request-Id"))
	}
}

// flusherProbeWriter is an http.ResponseWriter that records whether its
// Flush() method was invoked. It implements http.Flusher itself so that an
// upstream wrapper which transparently forwards Flush() will still trigger
// the underlying flush, which is the contract the time-mode download path
// depends on.
type flusherProbeWriter struct {
	header  http.Header
	flushed bool
	status  int
	body    bytes.Buffer
}

func newFlusherProbe() *flusherProbeWriter {
	return &flusherProbeWriter{header: http.Header{}}
}

func (f *flusherProbeWriter) Header() http.Header        { return f.header }
func (f *flusherProbeWriter) WriteHeader(code int)       { f.status = code }
func (f *flusherProbeWriter) Write(b []byte) (int, error) {
	if f.status == 0 {
		f.status = http.StatusOK
	}
	return f.body.Write(b)
}
func (f *flusherProbeWriter) Flush() { f.flushed = true }

// TestStatusWriterPreservesFlusher exercises the architectural invariant
// called out in CLAUDE.md: wrapping a ResponseWriter must not hide the
// underlying http.Flusher, otherwise time-mode downloads stall because the
// handler's `w.(http.Flusher)` type assertion fails.
func TestStatusWriterPreservesFlusher(t *testing.T) {
	probe := newFlusherProbe()
	sw := &statusWriter{ResponseWriter: probe, status: http.StatusOK}

	flusher, ok := any(sw).(http.Flusher)
	if !ok {
		t.Fatal("*statusWriter does not satisfy http.Flusher")
	}
	flusher.Flush()
	if !probe.flushed {
		t.Error("statusWriter.Flush did not propagate to the underlying ResponseWriter")
	}
}

// TestStatusWriterCapturesStatusAndBytes verifies the small bookkeeping
// contract used by the access logger: the first status code wins, and every
// Write contributes to bytesSent.
func TestStatusWriterCapturesStatusAndBytes(t *testing.T) {
	probe := newFlusherProbe()
	sw := &statusWriter{ResponseWriter: probe, status: http.StatusOK}

	sw.WriteHeader(http.StatusCreated)
	// A second WriteHeader should not change the recorded status.
	sw.WriteHeader(http.StatusInternalServerError)

	if _, err := sw.Write([]byte("hello ")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := sw.Write([]byte("world")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if sw.status != http.StatusCreated {
		t.Errorf("status = %d, want %d (first WriteHeader wins)", sw.status, http.StatusCreated)
	}
	if sw.bytesSent != int64(len("hello world")) {
		t.Errorf("bytesSent = %d, want %d", sw.bytesSent, len("hello world"))
	}
}

// TestNewRequestIDFormat verifies that the generated ID is a 32-character
// lowercase hex string — the contract the X-Request-Id header advertises.
func TestNewRequestIDFormat(t *testing.T) {
	id := newRequestID()
	if len(id) != 32 {
		t.Fatalf("newRequestID len = %d, want 32 (got %q)", len(id), id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("newRequestID returned non-hex: %v", err)
	}
}

// TestCountingReaderNilBody guards against nil-body panics: net/http
// guarantees r.Body is non-nil for handlers, but the middleware should still
// behave defensively if a future caller threads a request with a nil body
// (e.g. handcrafted in a test).
func TestCountingReaderNilBody(t *testing.T) {
	cr := &countingReader{rc: nil}
	n, err := cr.Read(make([]byte, 8))
	if n != 0 || err != io.EOF {
		t.Errorf("Read on nil body = (%d, %v), want (0, EOF)", n, err)
	}
	if err := cr.Close(); err != nil {
		t.Errorf("Close on nil body = %v, want nil", err)
	}
}
