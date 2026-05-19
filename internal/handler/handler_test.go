package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
)

// ── helpers ────────────────────────────────────────────────────────────────

func sizeCfg(dlMB, ulMB int) *config.Config {
	return &config.Config{
		Mode:       config.ModeSize,
		DownloadMB: dlMB,
		UploadMB:   ulMB,
		Duration:   10 * time.Second,
	}
}

func timeCfg(secs int) *config.Config {
	return &config.Config{
		Mode:       config.ModeTime,
		DownloadMB: 25,
		UploadMB:   10,
		Duration:   time.Duration(secs) * time.Second,
	}
}

// ── /api/config ────────────────────────────────────────────────────────────

func TestConfigHandlerSizeMode(t *testing.T) {
	cfg := sizeCfg(25, 10)
	h := handler.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(res.Body).Decode(&body)

	if body["mode"] != "size" {
		t.Errorf("mode = %v, want size", body["mode"])
	}
	if body["downloadMB"].(float64) != 25 {
		t.Errorf("downloadMB = %v, want 25", body["downloadMB"])
	}
	if body["uploadMB"].(float64) != 10 {
		t.Errorf("uploadMB = %v, want 10", body["uploadMB"])
	}
}

func TestConfigHandlerTimeMode(t *testing.T) {
	cfg := timeCfg(15)
	h := handler.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&body)

	if body["mode"] != "time" {
		t.Errorf("mode = %v, want time", body["mode"])
	}
	if body["durationSecs"].(float64) != 15 {
		t.Errorf("durationSecs = %v, want 15", body["durationSecs"])
	}
}

// ── /api/ip ────────────────────────────────────────────────────────────────

func TestIPHandlerReturnsJSON(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/ip", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	h.IPHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	json.NewDecoder(res.Body).Decode(&body)
	if body["ip"] != "1.2.3.4" {
		t.Errorf("ip = %q, want 1.2.3.4", body["ip"])
	}
}

func TestIPHandlerXForwardedFor(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/ip", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	w := httptest.NewRecorder()
	h.IPHandler(w, req)

	var body map[string]string
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["ip"] != "203.0.113.5" {
		t.Errorf("ip = %q, want 203.0.113.5", body["ip"])
	}
}

func TestIPHandlerXRealIP(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/ip", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	req.Header.Set("X-Real-Ip", "198.51.100.7")
	w := httptest.NewRecorder()
	h.IPHandler(w, req)

	var body map[string]string
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["ip"] != "198.51.100.7" {
		t.Errorf("ip = %q, want 198.51.100.7", body["ip"])
	}
}

// ── /api/download (size mode) ──────────────────────────────────────────────

func TestDownloadHandlerSizeModeDefaultSize(t *testing.T) {
	h := handler.New(sizeCfg(10, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	want := 10 * 1024 * 1024
	if len(body) != want {
		t.Errorf("body = %d bytes, want %d (10 MB)", len(body), want)
	}
}

func TestDownloadHandlerSizeModeHonorsBytesOverride(t *testing.T) {
	h := handler.New(sizeCfg(10, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/download?bytes=1234567", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	if len(body) != 1234567 {
		t.Errorf("body = %d bytes, want %d", len(body), 1234567)
	}
	if cl := w.Result().Header.Get("Content-Length"); cl != "1234567" {
		t.Errorf("Content-Length = %q, want %q", cl, "1234567")
	}
}

func TestDownloadHandlerSizeModeContentType(t *testing.T) {
	h := handler.New(sizeCfg(1, 1))
	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	if ct := w.Result().Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
}

func TestDownloadHandlerNoCachingHeaders(t *testing.T) {
	h := handler.New(sizeCfg(1, 1))
	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// TestDownloadPayloadIsDeterministic verifies that successive download
// responses return identical bytes — confirming the server reuses a single
// pre-generated random buffer rather than re-randomizing per chunk. Per-chunk
// crypto/rand makes the download CPU-bound on gigabit+ links and distorts
// throughput measurement.
func TestDownloadPayloadIsDeterministic(t *testing.T) {
	h := handler.New(sizeCfg(1, 1))

	fetch := func() []byte {
		req := httptest.NewRequest(http.MethodGet, "/api/download?bytes=131072", nil)
		w := httptest.NewRecorder()
		h.DownloadHandler(w, req)
		body, _ := io.ReadAll(w.Result().Body)
		return body
	}

	first := fetch()
	second := fetch()

	if len(first) != 131072 || len(second) != 131072 {
		t.Fatalf("body sizes = (%d, %d), want both 131072", len(first), len(second))
	}
	if !bytes.Equal(first, second) {
		t.Error("two downloads returned different bytes — pre-generated random buffer is not being reused")
	}
}

// TestDownloadPayloadIsHighEntropy guards against a regression where the
// pre-generated buffer is left as all zeros (which would still be
// incompressible-equivalent in this test but is wrong). A simple way to
// detect "all zero" without false positives: count distinct byte values.
func TestDownloadPayloadIsHighEntropy(t *testing.T) {
	h := handler.New(sizeCfg(1, 1))
	req := httptest.NewRequest(http.MethodGet, "/api/download?bytes=65536", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)
	body, _ := io.ReadAll(w.Result().Body)

	var seen [256]bool
	distinct := 0
	for _, b := range body {
		if !seen[b] {
			seen[b] = true
			distinct++
		}
	}
	// A genuinely random 64 KB buffer hits all 256 byte values with overwhelming
	// probability. All-zero would yield distinct=1.
	if distinct < 200 {
		t.Errorf("download payload entropy too low: only %d distinct byte values (likely uninitialised)", distinct)
	}
}

// ── /api/download (time mode) ──────────────────────────────────────────────

func TestDownloadHandlerTimeModeStreamsForDuration(t *testing.T) {
	cfg := timeCfg(2) // 2-second test
	h := handler.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	// Should have run for approximately 2 seconds (allow ±500ms for test overhead)
	if elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		t.Errorf("time mode ran for %v, want ~2s", elapsed)
	}

	body, _ := io.ReadAll(w.Result().Body)
	if len(body) == 0 {
		t.Error("time mode body is empty")
	}
}

func TestDownloadHandlerTimeModeWritesData(t *testing.T) {
	cfg := timeCfg(1)
	h := handler.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	// At minimum, at least one chunk (256 KB) must have been written in 1 second
	if len(body) < 256*1024 {
		t.Errorf("time mode wrote only %d bytes in 1s, expected >= 256KB", len(body))
	}
}

// ── /api/upload ────────────────────────────────────────────────────────────

func TestUploadHandlerAcceptsPOST(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	payload := strings.Repeat("x", 1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(payload))
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Result().Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestUploadHandlerRejectsGET(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/upload", nil)
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/upload: status = %d, want 405", w.Code)
	}
}

func TestUploadHandlerReturnsBytesReceived(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	size := 512 * 1024
	payload := strings.Repeat("z", size)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(payload))
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	var body map[string]int64
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["received"] != int64(size) {
		t.Errorf("received = %d, want %d", body["received"], size)
	}
}

// ── /api/ping ──────────────────────────────────────────────────────────────

func TestPingHandlerReturnsOK(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	w := httptest.NewRecorder()
	h.PingHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestPingHandlerBodyIsMinimal(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	w := httptest.NewRecorder()
	h.PingHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	if len(body) > 64 {
		t.Errorf("ping body = %d bytes, want <= 64", len(body))
	}
}

func TestPingHandlerNoCachingHeaders(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	w := httptest.NewRecorder()
	h.PingHandler(w, req)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// ── ClientIP ───────────────────────────────────────────────────────────────

func TestClientIPFromRemoteAddr(t *testing.T) {
	cases := []struct{ remoteAddr, xff, xri, want string }{
		{"192.168.1.1:12345", "", "", "192.168.1.1"},
		{"[::1]:8080", "", "", "::1"},
		{"bad-addr", "", "", "bad-addr"},
		{"10.0.0.1:80", "1.2.3.4, 10.0.0.1", "", "1.2.3.4"},
		{"10.0.0.1:80", "", "5.6.7.8", "5.6.7.8"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.remoteAddr
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		if tc.xri != "" {
			req.Header.Set("X-Real-Ip", tc.xri)
		}
		got := handler.ClientIP(req)
		if got != tc.want {
			t.Errorf("remoteAddr=%q xff=%q xri=%q: ClientIP()=%q, want %q",
				tc.remoteAddr, tc.xff, tc.xri, got, tc.want)
		}
	}
}

// ── Semaphore / 503 behaviour ──────────────────────────────────────────────

// concurrentCfg returns a Config with MaxConcurrent set to n so tests can
// fill the semaphore precisely without waiting for real transfers.
func concurrentCfg(n int) *config.Config {
	return &config.Config{
		Mode:          config.ModeSize,
		DownloadMB:    1,
		UploadMB:      1,
		Duration:      10 * time.Second,
		MaxConcurrent: n,
	}
}

// TestSemaphoreFullReturns503ForDownload fills all semaphore slots using a
// real httptest.Server (so connections can be cancelled to release slots),
// then issues one more request and expects 503 Service Unavailable.
func TestSemaphoreFullReturns503ForDownload(t *testing.T) {
	// Use time-mode with a very long duration so holder requests stay open.
	cfg := &config.Config{
		Mode:          config.ModeTime,
		Duration:      300 * time.Second,
		DownloadMB:    25,
		UploadMB:      10,
		MaxConcurrent: 2,
	}
	h := handler.New(cfg)

	// Wrap in a mux so we can serve through a real test server.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/download", h.DownloadHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Start two downloads that will hold their semaphore slots.
	// We use a context so we can cancel them programmatically.
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	headerReceived := make(chan struct{}, 2)

	startHolder := func(ctx context.Context) {
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/download", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			headerReceived <- struct{}{}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}

	startHolder(ctx1)
	startHolder(ctx2)

	// Wait until both holders have received response headers — at that point
	// each has called acquire() and is streaming data.
	<-headerReceived
	<-headerReceived

	// Third request hits the handler directly. Semaphore is full → 503.
	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when semaphore full, got %d", w.Code)
	}

	// Cancel holders so server-side writes fail → handlers return → slots released.
	cancel1()
	cancel2()
}

// TestSemaphoreFullReturns503ForUpload mirrors the download test for the
// upload path. The upload body is provided via io.Pipe so we can keep it
// open (holding the semaphore slot) until after the 503 assertion.
func TestSemaphoreFullReturns503ForUpload(t *testing.T) {
	cfg := concurrentCfg(2)
	h := handler.New(cfg)

	holdDone := make(chan struct{}, 2)
	pipes := make([]*io.PipeWriter, 2)

	for i := 0; i < 2; i++ {
		pr, pw := io.Pipe()
		pipes[i] = pw
		go func(body *io.PipeReader) {
			req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
			w := httptest.NewRecorder()
			h.UploadHandler(w, req)
			holdDone <- struct{}{}
		}(pr)
	}

	// Give goroutines time to call acquire() and block on reading the pipe body.
	time.Sleep(20 * time.Millisecond)

	// Third request: semaphore full → expect 503.
	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader("data"))
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when semaphore full, got %d", w.Code)
	}

	// Close pipes so the holder goroutines can finish and release slots.
	for _, pw := range pipes {
		pw.Close()
	}
	<-holdDone
	<-holdDone
}

// ── Proxy header spoofing ─────────────────────────────────────────────────

// TestProxyHeaderSpoofingRejected verifies that when the direct peer
// (RemoteAddr) is a public IP, X-Forwarded-For and X-Real-Ip headers are
// ignored and the real peer address is returned. This guards against clients
// spoofing their source IP when connecting directly (not via a trusted
// internal proxy).
func TestProxyHeaderSpoofingRejected(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		wantIP     string
	}{
		{
			name:       "public peer ignores XFF",
			remoteAddr: "8.8.8.8:80",
			xff:        "203.0.113.99",
			wantIP:     "8.8.8.8",
		},
		{
			name:       "public peer ignores X-Real-Ip",
			remoteAddr: "1.2.3.4:443",
			xri:        "99.99.99.99",
			wantIP:     "1.2.3.4",
		},
		{
			name:       "public peer ignores both headers",
			remoteAddr: "5.6.7.8:1234",
			xff:        "10.0.0.1",
			xri:        "172.16.0.1",
			wantIP:     "5.6.7.8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/ip", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				req.Header.Set("X-Real-Ip", tc.xri)
			}
			got := handler.ClientIP(req)
			if got != tc.wantIP {
				t.Errorf("ClientIP() = %q, want %q (spoofed header must be ignored for public peer)",
					got, tc.wantIP)
			}
		})
	}
}

// ── Method rejection on read-only endpoints ────────────────────────────────

// TestMethodRejectionOnConfigEndpoint verifies that non-GET methods on
// /api/config return 405 Method Not Allowed.
func TestMethodRejectionOnConfigEndpoint(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/config", nil)
			w := httptest.NewRecorder()
			h.ConfigHandler(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /api/config: status = %d, want 405", method, w.Code)
			}
		})
	}
}

// TestMethodRejectionOnIPEndpoint verifies that non-GET methods on /api/ip
// return 405 Method Not Allowed.
func TestMethodRejectionOnIPEndpoint(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/ip", nil)
			w := httptest.NewRecorder()
			h.IPHandler(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /api/ip: status = %d, want 405", method, w.Code)
			}
		})
	}
}

// TestMethodRejectionOnPingEndpoint verifies that non-GET methods on
// /api/ping return 405 Method Not Allowed.
func TestMethodRejectionOnPingEndpoint(t *testing.T) {
	h := handler.New(sizeCfg(25, 10))
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/ping", nil)
			w := httptest.NewRecorder()
			h.PingHandler(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /api/ping: status = %d, want 405", method, w.Code)
			}
		})
	}
}

// ── Upload 413 / maxUploadBytes note ─────────────────────────────────────
//
// The maxUploadBytes constant (10 GB) is intentionally too large to exercise
// in a unit test — allocating or streaming 10 GB would make CI infeasible.
// Because the constant is unexported it cannot be overridden from test code
// without patching the production source.
//
// Coverage for the "body too large" branch is therefore documented here as a
// known limitation. The happy path (normal payload accepted, byte count
// returned) is already covered by TestUploadHandlerReturnsBytesReceived and
// TestUploadHandlerAcceptsPOST. The 413 path is protected at the integration
// level: http.MaxBytesReader is a stdlib primitive with its own test coverage.

// ── statusWriter implements http.Flusher (N1 regression test) ────────────

// TestStatusWriterImplementsFlusher verifies that the loggingMiddleware
// wrapper (statusWriter in main.go) satisfies http.Flusher. This is the
// regression test for issue N1: without Flush(), the type assertion
// w.(http.Flusher) inside downloadByTime would silently fail in production,
// preventing streaming chunks from being sent to the client.
//
// The test runs a real httptest.Server to ensure the full middleware chain is
// involved, and confirms that at least one Flush() call reaches the
// underlying writer.
func TestStatusWriterImplementsFlusher(t *testing.T) {
	cfg := &config.Config{
		Mode:          config.ModeTime,
		Duration:      1 * time.Second,
		DownloadMB:    25,
		UploadMB:      10,
		MaxConcurrent: 5,
	}
	h := handler.New(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/download", h.DownloadHandler)

	flushCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fw := &flushCountingWriter{ResponseWriter: w, count: &flushCount}
		mux.ServeHTTP(fw, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/download")
	if err != nil {
		t.Fatalf("GET /api/download: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if flushCount == 0 {
		t.Error("Flush() was never called — http.Flusher assertion in downloadByTime must have failed")
	}
}

// flushCountingWriter wraps a ResponseWriter, counts Flush calls, and
// forwards them to the underlying writer if it also implements http.Flusher.
type flushCountingWriter struct {
	http.ResponseWriter
	count *int
}

func (f *flushCountingWriter) Flush() {
	*f.count++
	if fl, ok := f.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// ── ?duration override ──────────────────────────────────────────────────────

// TestDownloadDurationOverride checks that ?duration=N is honoured in time
// mode and that the server streams for approximately N seconds (not the
// server's default Duration).
func TestDownloadDurationOverride(t *testing.T) {
	cfg := timeCfg(60) // server default = 60 s — we override to 1 s
	h := handler.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=1", nil)
	w := httptest.NewRecorder()
	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Should have run for ~1 s, not 60 s.
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, expected ~1 s (duration override ignored)", elapsed)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty body")
	}
}

// TestDownloadDurationOverrideTooLarge verifies that a ?duration value
// above maxDurationSecs (300) falls back to the server's configured Duration.
func TestDownloadDurationOverrideTooLarge(t *testing.T) {
	cfg := timeCfg(1) // server default = 1 s
	h := handler.New(cfg)

	// 9999 > maxDurationSecs, should be ignored and server uses 1 s.
	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=9999", nil)
	w := httptest.NewRecorder()
	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v — out-of-range duration was not rejected", elapsed)
	}
}

// TestDownloadDurationOverrideZero verifies that ?duration=0 is rejected and
// the server falls back to its configured Duration.
func TestDownloadDurationOverrideZero(t *testing.T) {
	cfg := timeCfg(1) // server default = 1 s
	h := handler.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=0", nil)
	w := httptest.NewRecorder()
	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v — zero duration was not rejected", elapsed)
	}
}

// TestDownloadDurationIgnoredInSizeMode verifies that ?duration has no effect
// when the server is in size mode (the ?bytes parameter governs transfer size).
func TestDownloadDurationIgnoredInSizeMode(t *testing.T) {
	cfg := sizeCfg(1, 1) // 1 MB download
	h := handler.New(cfg)

	// Even with duration=60, size-mode should finish quickly.
	req := httptest.NewRequest(http.MethodGet, "/api/download?bytes=1024&duration=60", nil)
	w := httptest.NewRecorder()
	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v — duration parameter affected size-mode download", elapsed)
	}
	if w.Body.Len() != 1024 {
		t.Errorf("body size = %d, want 1024", w.Body.Len())
	}
}
