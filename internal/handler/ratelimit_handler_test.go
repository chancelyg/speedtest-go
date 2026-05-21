package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubNext is the downstream handler the middleware wraps in tests. Each call
// is counted so a test can assert pass-through vs. block behaviour.
func stubNext() (http.Handler, func() int) {
	var (
		mu  sync.Mutex
		hit int
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hit++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	get := func() int {
		mu.Lock()
		defer mu.Unlock()
		return hit
	}
	return h, get
}

// newHandlerWithLimiter constructs a handler with a *rateLimiter (or nil) and
// returns the wrapped middleware ready to drive with httptest.
func newHandlerWithLimiter(t *testing.T, rl *rateLimiter, next http.Handler) http.Handler {
	t.Helper()
	h := &Handler{limiter: rl}
	if rl != nil {
		t.Cleanup(rl.stop)
	}
	return h.RateLimit(next)
}

// makeRequest forges a request from a specific peer. We use 127.0.0.1 so
// ClientIP treats X-Forwarded-For as trustworthy if a test sets one, but here
// the host bare-IP itself is the rate-limit key for the default cases.
func makeRequest(method, path, peer string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = peer + ":12345"
	return r
}

func TestRateLimit_DisabledWhenLimiterNil(t *testing.T) {
	next, hits := stubNext()
	mw := newHandlerWithLimiter(t, nil, next)

	for i := 0; i < 100; i++ {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, makeRequest(http.MethodGet, "/api/ping", "10.0.0.1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("iter %d: status = %d, want 200 (limiter disabled)", i, rec.Code)
		}
	}
	if hits() != 100 {
		t.Errorf("downstream hits = %d, want 100", hits())
	}
}

func TestRateLimit_DisabledViaNewRateLimiter(t *testing.T) {
	// newRateLimiter(0) must return a nil pointer so RateLimit is a no-op.
	if rl := newRateLimiter(0); rl != nil {
		t.Fatalf("newRateLimiter(0) = %v, want nil", rl)
	}
	if rl := newRateLimiter(-5); rl != nil {
		t.Fatalf("newRateLimiter(-5) = %v, want nil", rl)
	}
}

func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	rl := newRateLimiter(2) // 2 req/min, burst = 2
	next, hits := stubNext()
	mw := newHandlerWithLimiter(t, rl, next)

	// First 2 requests from the same IP succeed (burst window).
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, makeRequest(http.MethodGet, "/api/ping", "203.0.113.7"))
		if rec.Code != http.StatusOK {
			t.Fatalf("burst iter %d: status = %d, want 200", i, rec.Code)
		}
	}
	// 3rd request must be rejected with 429.
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(http.MethodGet, "/api/ping", "203.0.113.7"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("post-burst status = %d, want 429", rec.Code)
	}
	if hits() != 2 {
		t.Errorf("downstream hits = %d, want 2 (3rd should not reach handler)", hits())
	}
}

func TestRateLimit_RejectionResponseShape(t *testing.T) {
	rl := newRateLimiter(1)
	next, _ := stubNext()
	mw := newHandlerWithLimiter(t, rl, next)

	// Burn the single allowed token.
	mw.ServeHTTP(httptest.NewRecorder(), makeRequest(http.MethodGet, "/api/ping", "198.51.100.42"))

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(http.MethodGet, "/api/ping", "198.51.100.42"))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	ra := rec.Header().Get("Retry-After")
	if ra == "" {
		t.Error("Retry-After header missing")
	}
	if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Errorf("Retry-After = %q, want positive integer seconds", ra)
	}
	var body errResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if body.Error != "rate limit exceeded" {
		t.Errorf("error = %q, want %q", body.Error, "rate limit exceeded")
	}
}

func TestRateLimit_PerIPIsolation(t *testing.T) {
	rl := newRateLimiter(1)
	next, _ := stubNext()
	mw := newHandlerWithLimiter(t, rl, next)

	// First IP exhausts its single token, second IP is unaffected.
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, makeRequest(http.MethodGet, "/api/ping", "203.0.113.1"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("IP1 first req: status = %d, want 200", rec1.Code)
	}

	rec1b := httptest.NewRecorder()
	mw.ServeHTTP(rec1b, makeRequest(http.MethodGet, "/api/ping", "203.0.113.1"))
	if rec1b.Code != http.StatusTooManyRequests {
		t.Fatalf("IP1 second req: status = %d, want 429", rec1b.Code)
	}

	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, makeRequest(http.MethodGet, "/api/ping", "203.0.113.2"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("IP2 first req: status = %d, want 200 (independent bucket)", rec2.Code)
	}
}

func TestRateLimit_ScopedToSpeedTestEndpoints(t *testing.T) {
	rl := newRateLimiter(1)
	next, hits := stubNext()
	mw := newHandlerWithLimiter(t, rl, next)

	// /api/config, /api/ip, /healthz, /metrics, /api/results, / must NOT
	// be rate-limited regardless of how many requests one IP fires.
	exemptPaths := []string{
		"/", "/healthz", "/metrics",
		"/api/config", "/api/ip",
		"/api/results", "/api/results/export",
	}
	for _, path := range exemptPaths {
		for i := 0; i < 5; i++ {
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, makeRequest(http.MethodGet, path, "203.0.113.99"))
			if rec.Code != http.StatusOK {
				t.Fatalf("exempt path %s iter %d: status = %d, want 200", path, i, rec.Code)
			}
		}
	}
	if hits() != len(exemptPaths)*5 {
		t.Errorf("downstream hits = %d, want %d", hits(), len(exemptPaths)*5)
	}

	// The bucket on /api/ping is independent — and still bounded.
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(http.MethodGet, "/api/ping", "203.0.113.99"))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/ping first req: status = %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	mw.ServeHTTP(rec, makeRequest(http.MethodGet, "/api/ping", "203.0.113.99"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("/api/ping second req: status = %d, want 429", rec.Code)
	}
}

func TestRateLimit_CoversAllSpeedTestPaths(t *testing.T) {
	// Each of /api/download, /api/upload, /api/ping must be limited.
	cases := []string{"/api/download", "/api/upload", "/api/ping"}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			rl := newRateLimiter(1)
			next, _ := stubNext()
			mw := newHandlerWithLimiter(t, rl, next)

			method := http.MethodGet
			if path == "/api/upload" {
				method = http.MethodPost
			}

			ok1 := httptest.NewRecorder()
			mw.ServeHTTP(ok1, makeRequest(method, path, "192.0.2.50"))
			if ok1.Code != http.StatusOK {
				t.Fatalf("first req: status = %d, want 200", ok1.Code)
			}
			ok2 := httptest.NewRecorder()
			mw.ServeHTTP(ok2, makeRequest(method, path, "192.0.2.50"))
			if ok2.Code != http.StatusTooManyRequests {
				t.Fatalf("second req: status = %d, want 429", ok2.Code)
			}
		})
	}
}

func TestRateLimit_GCEvictsIdleEntries(t *testing.T) {
	// Shrink the TTL and tick interval so this test runs in milliseconds.
	prevTTL := ratelimitIdleTTL
	prevInterval := ratelimitGCInterval
	ratelimitIdleTTL = 20 * time.Millisecond
	ratelimitGCInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		ratelimitIdleTTL = prevTTL
		ratelimitGCInterval = prevInterval
	})

	rl := newRateLimiter(10)
	t.Cleanup(rl.stop)

	// Seed an entry by calling allow once.
	if ok, _ := rl.allow("203.0.113.123"); !ok {
		t.Fatal("initial allow should pass")
	}
	rl.mu.Lock()
	if _, ok := rl.limiters["203.0.113.123"]; !ok {
		rl.mu.Unlock()
		t.Fatal("limiter entry should exist after first allow")
	}
	rl.mu.Unlock()

	// Wait long enough for the entry to age out and at least one GC pass to
	// run. Total 100 ms covers both even on a heavily-loaded CI runner.
	time.Sleep(100 * time.Millisecond)

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if _, present := rl.limiters["203.0.113.123"]; present {
		t.Errorf("idle entry should have been GC'd after %v", ratelimitIdleTTL)
	}
}

func TestRateLimit_StopIsIdempotent(t *testing.T) {
	rl := newRateLimiter(1)
	rl.stop()
	rl.stop() // must not panic on a double-close.

	var nilRL *rateLimiter
	nilRL.stop() // must not panic on nil.
}
