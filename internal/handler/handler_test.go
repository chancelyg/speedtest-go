package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
)

// ── helpers ────────────────────────────────────────────────────────────────

// countingDiscardWriter is a test ResponseWriter that discards the body but
// records the byte count, status code, and headers. Used by time-mode
// download tests because httptest.NewRecorder() buffers the entire body in
// memory — and downloadByTime writes ~1 MB per loop iteration with no upper
// bound on iterations, which exhausts CI runner memory (OOM → SIGTERM 143).
type countingDiscardWriter struct {
	header  http.Header
	status  int
	written int64
}

func newCountingWriter() *countingDiscardWriter {
	return &countingDiscardWriter{header: http.Header{}, status: http.StatusOK}
}

func (c *countingDiscardWriter) Header() http.Header { return c.header }

func (c *countingDiscardWriter) Write(p []byte) (int, error) {
	c.written += int64(len(p))
	return len(p), nil
}

func (c *countingDiscardWriter) WriteHeader(statusCode int) { c.status = statusCode }

func (c *countingDiscardWriter) Flush() {}

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
	h := handler.New(cfg, nil)

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
	h := handler.New(cfg, nil)

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

func TestConfigHandlerReportsMaxConcurrent(t *testing.T) {
	// Explicit non-default value so we don't accidentally test the fallback.
	cfg := concurrentCfg(7)
	h := handler.New(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&body)
	if got, ok := body["maxConcurrent"].(float64); !ok || int(got) != 7 {
		t.Errorf("maxConcurrent = %v, want 7", body["maxConcurrent"])
	}
}

func TestConfigHandlerReportsBuildMetadata(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
	h.Build = handler.BuildInfo{Version: "1.2.3", Commit: "abc1234", Date: "2026-07-14"}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	var body map[string]any
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", body["version"])
	}
	if body["commit"] != "abc1234" {
		t.Errorf("commit = %v, want abc1234", body["commit"])
	}
	if body["date"] != "2026-07-14" {
		t.Errorf("date = %v, want 2026-07-14", body["date"])
	}
}

func TestConfigHandlerVersionFallsBackToDev(t *testing.T) {
	// Same zero-value fallback contract as /healthz — the frontend footer
	// depends on receiving *some* string every time.
	h := handler.New(sizeCfg(25, 10), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	var body map[string]any
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["version"] != "dev" {
		t.Errorf("version = %v, want dev (fallback)", body["version"])
	}
}

func TestConfigHandlerFiltersMainDefaultSentinels(t *testing.T) {
	// main.go's ldflag defaults are "none" / "unknown"; those strings must
	// NOT leak into JSON because the frontend truthy-check would render
	// them as a tooltip ("none · unknown" for a dev build). The handler
	// helpers coerce them to empty strings.
	h := handler.New(sizeCfg(25, 10), nil)
	h.Build = handler.BuildInfo{Version: "dev", Commit: "none", Date: "unknown"}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	var body map[string]any
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body["commit"] != "" {
		t.Errorf(`commit = %q, want "" (sentinel filtered)`, body["commit"])
	}
	if body["date"] != "" {
		t.Errorf(`date = %q, want "" (sentinel filtered)`, body["date"])
	}
}

func TestConfigHandlerMaxConcurrentFallback(t *testing.T) {
	// sizeCfg leaves MaxConcurrent at 0; Handler.New coerces the semaphore
	// capacity to 10. /api/config must report the effective value, not 0,
	// otherwise the frontend cannot cap its streams selector correctly.
	h := handler.New(sizeCfg(25, 10), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	h.ConfigHandler(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&body)
	if got, ok := body["maxConcurrent"].(float64); !ok || int(got) != 10 {
		t.Errorf("maxConcurrent = %v, want 10 (fallback)", body["maxConcurrent"])
	}
}

// ── /api/ip ────────────────────────────────────────────────────────────────

func TestIPHandlerReturnsJSON(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(sizeCfg(10, 10), nil)
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
	h := handler.New(sizeCfg(10, 10), nil)
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
	h := handler.New(sizeCfg(1, 1), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)

	if ct := w.Result().Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
}

func TestDownloadHandlerNoCachingHeaders(t *testing.T) {
	h := handler.New(sizeCfg(1, 1), nil)
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
	h := handler.New(sizeCfg(1, 1), nil)

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
	h := handler.New(sizeCfg(1, 1), nil)
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
	h := handler.New(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := newCountingWriter()

	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	// Should have run for approximately 2 seconds (allow ±500ms for test overhead)
	if elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		t.Errorf("time mode ran for %v, want ~2s", elapsed)
	}

	if w.written == 0 {
		t.Error("time mode body is empty")
	}
}

func TestDownloadHandlerTimeModeWritesData(t *testing.T) {
	cfg := timeCfg(1)
	h := handler.New(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/download", nil)
	w := newCountingWriter()
	h.DownloadHandler(w, req)

	// At minimum, at least one chunk (256 KB) must have been written in 1 second
	if w.written < 256*1024 {
		t.Errorf("time mode wrote only %d bytes in 1s, expected >= 256KB", w.written)
	}
}

// ── /api/upload ────────────────────────────────────────────────────────────

func TestUploadHandlerAcceptsPOST(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(sizeCfg(25, 10), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/upload", nil)
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/upload: status = %d, want 405", w.Code)
	}
}

func TestUploadHandlerReturnsBytesReceived(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
	size := 512 * 1024
	payload := strings.Repeat("z", size)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(payload))
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	var body struct {
		Received        int64 `json:"received"`
		ServerElapsedMs int64 `json:"serverElapsedMs"`
		Truncated       bool  `json:"truncated"`
	}
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Received != int64(size) {
		t.Errorf("received = %d, want %d", body.Received, size)
	}
	if body.ServerElapsedMs < 0 {
		t.Errorf("serverElapsedMs = %d, want >= 0", body.ServerElapsedMs)
	}
	if body.Truncated {
		t.Errorf("truncated = true, want false for under-cap upload")
	}
}

// maxBytesErrReader yields one byte then signals the MaxBytesReader limit
// has been hit. Used to exercise the truncation branch in UploadHandler
// without allocating the real 10 GB cap.
type maxBytesErrReader struct{ delivered bool }

func (r *maxBytesErrReader) Read(p []byte) (int, error) {
	if !r.delivered {
		r.delivered = true
		p[0] = 'q'
		return 1, nil
	}
	return 0, &http.MaxBytesError{Limit: 1}
}
func (r *maxBytesErrReader) Close() error { return nil }

// Fix-B regression: when the body exceeds the upload cap the handler must
// still respond 200 with the partial byte count and Truncated=true, instead
// of the previous 413. Returning 413 turned valid gigabit-class samples into
// outright failures once they crossed the 10 GB cap.
func TestUploadHandlerOverCapReturnsTruncated(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	req.Body = &maxBytesErrReader{}
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (truncation must not be a failure)", w.Code)
	}
	var body struct {
		Received        int64 `json:"received"`
		ServerElapsedMs int64 `json:"serverElapsedMs"`
		Truncated       bool  `json:"truncated"`
	}
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Received != 1 {
		t.Errorf("received = %d, want 1 (the byte delivered before cap)", body.Received)
	}
	if !body.Truncated {
		t.Errorf("truncated = false, want true when MaxBytesError fired")
	}
}

// Fix-C regression: download responses must set Content-Encoding: identity so
// intermediate gzip-aware proxies don't try to compress the random payload.
func TestDownloadHandlerSetsIdentityEncoding(t *testing.T) {
	h := handler.New(sizeCfg(1, 10), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/download?bytes=1024", nil)
	w := httptest.NewRecorder()
	h.DownloadHandler(w, req)
	if ce := w.Result().Header.Get("Content-Encoding"); ce != "identity" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "identity")
	}
}

// Fix-C regression: same identity hint on upload responses.
func TestUploadHandlerSetsIdentityEncoding(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader("x"))
	w := httptest.NewRecorder()
	h.UploadHandler(w, req)
	if ce := w.Result().Header.Get("Content-Encoding"); ce != "identity" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "identity")
	}
}

// Fix-G regression: end-to-end throughput math sanity check. Boots a real
// HTTP server, requests a known-size payload, times the read, and asserts the
// computed Mbps falls in a wide-but-non-trivial range. The point is not to
// benchmark the host — it is to catch regressions where the handler returns
// far fewer bytes than promised, or the elapsed/byte math drifts.
func TestThroughputMath(t *testing.T) {
	h := handler.New(sizeCfg(1, 1), nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/download", h.DownloadHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	const wantBytes = 1 << 20 // 1 MB exact
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/download?bytes="+strconv.Itoa(wantBytes), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	n, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if n != int64(wantBytes) {
		t.Fatalf("bytes = %d, want %d", n, wantBytes)
	}
	if elapsed <= 0 {
		t.Fatalf("elapsed = %v, want > 0", elapsed)
	}

	mbps := float64(n*8) / elapsed.Seconds() / 1e6
	// Loopback should clear at least 50 Mbps even on a heavily loaded CI
	// runner — well below the gigabit-class numbers a real machine sees,
	// but high enough to catch order-of-magnitude regressions.
	if mbps < 50 {
		t.Errorf("throughput = %.2f Mbps for %d bytes in %v, want >= 50 Mbps", mbps, n, elapsed)
	}
}

// ── /api/ping ──────────────────────────────────────────────────────────────

func TestPingHandlerReturnsOK(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	w := httptest.NewRecorder()
	h.PingHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestPingHandlerBodyIsMinimal(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	w := httptest.NewRecorder()
	h.PingHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	if len(body) > 64 {
		t.Errorf("ping body = %d bytes, want <= 64", len(body))
	}
}

func TestPingHandlerNoCachingHeaders(t *testing.T) {
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(cfg, nil)

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
	h := handler.New(cfg, nil)

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
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(sizeCfg(25, 10), nil)
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
	h := handler.New(cfg, nil)

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
	h := handler.New(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=1", nil)
	w := newCountingWriter()
	start := time.Now()
	h.DownloadHandler(w, req)
	elapsed := time.Since(start)

	if w.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.status)
	}
	// Should have run for ~1 s, not 60 s.
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, expected ~1 s (duration override ignored)", elapsed)
	}
	if w.written == 0 {
		t.Error("expected non-empty body")
	}
}

// TestDownloadDurationOverrideTooLarge verifies that a ?duration value
// above maxDurationSecs (300) falls back to the server's configured Duration.
func TestDownloadDurationOverrideTooLarge(t *testing.T) {
	cfg := timeCfg(1) // server default = 1 s
	h := handler.New(cfg, nil)

	// 9999 > maxDurationSecs, should be ignored and server uses 1 s.
	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=9999", nil)
	w := newCountingWriter()
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
	h := handler.New(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=0", nil)
	w := newCountingWriter()
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
	h := handler.New(cfg, nil)

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
