package handler

// Phase 4 Track B — per-IP rate limiting middleware.
//
// Default is OFF (cfg.RatePerMin == 0 → newRateLimiter returns nil → middleware
// becomes a no-op). When enabled the limiter is keyed by handler.ClientIP, so
// the same trusted-proxy chain that protects ClientIP from spoofing also
// protects the limiter from per-request key forgery.

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ratelimitGCInterval is how often the eviction goroutine wakes up.
// ratelimitIdleTTL is how long an idle limiter entry is kept before being
// evicted. Both are var (not const) so tests can shorten them.
var (
	ratelimitGCInterval = 1 * time.Minute
	ratelimitIdleTTL    = 15 * time.Minute
)

// rateLimitedPaths is the exact set of endpoints to which the middleware
// applies. Status / inventory paths (/metrics, /healthz, /api/config, /api/ip,
// /api/results*) are exempt — they are cheap and we don't want their counters
// to mask DoS traffic against the speed-test endpoints.
var rateLimitedPaths = map[string]struct{}{
	"/api/download": {},
	"/api/upload":   {},
	"/api/ping":     {},
}

// rateLimiter keeps a per-IP token bucket. The zero value is not usable —
// always construct via newRateLimiter.
type rateLimiter struct {
	ratePerMin int

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	lastSeen map[string]time.Time

	done chan struct{} // closed by stop() to terminate the GC goroutine
}

// newRateLimiter returns a *rateLimiter ready to admit traffic. When
// ratePerMin <= 0 it returns nil — callers must treat nil as the "no-op,
// limiter disabled" mode. This matches the default configuration where
// SPEEDTEST_RATE_PER_MIN is unset.
func newRateLimiter(ratePerMin int) *rateLimiter {
	if ratePerMin <= 0 {
		return nil
	}
	rl := &rateLimiter{
		ratePerMin: ratePerMin,
		limiters:   make(map[string]*rate.Limiter),
		lastSeen:   make(map[string]time.Time),
		done:       make(chan struct{}),
	}
	go rl.gcLoop(ratelimitGCInterval, ratelimitIdleTTL)
	return rl
}

// stop terminates the GC goroutine. Safe to call multiple times; safe to call
// on a nil receiver so handler tear-down code can be unconditional.
func (rl *rateLimiter) stop() {
	if rl == nil {
		return
	}
	select {
	case <-rl.done:
		// already stopped
	default:
		close(rl.done)
	}
}

// gcLoop evicts limiter entries that have been idle for longer than
// ratelimitIdleTTL. Keeps the map from growing unbounded under churn from
// transient client IPs (e.g. mobile/NAT/cgnat). The interval and TTL are
// snapshotted at goroutine start so test code can mutate the package-level
// knobs before constructing the limiter without racing against this loop.
func (rl *rateLimiter) gcLoop(interval, ttl time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-rl.done:
			return
		case now := <-t.C:
			rl.gc(now, ttl)
		}
	}
}

func (rl *rateLimiter) gc(now time.Time, ttl time.Duration) {
	cutoff := now.Add(-ttl)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, seen := range rl.lastSeen {
		if seen.Before(cutoff) {
			delete(rl.lastSeen, ip)
			delete(rl.limiters, ip)
		}
	}
}

// limiterFor returns the per-IP limiter, creating one on first use. The
// caller is expected to hold rl.mu via the helper itself, which encapsulates
// the locking. lastSeen is refreshed on every lookup so active clients are
// never evicted.
func (rl *rateLimiter) limiterFor(ip string, now time.Time) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.lastSeen[ip] = now
	if lim, ok := rl.limiters[ip]; ok {
		return lim
	}
	// Tokens per second = ratePerMin / 60. Burst = ratePerMin so a freshly-
	// created bucket starts full (matches "N requests per minute, no warm-up
	// throttling on a cold cache"). This means the very first GC-evicted +
	// returning client sees the same allowance as a first-time visitor —
	// intentional, since this limiter is anti-DoS, not anti-burst-pricing.
	lim := rate.NewLimiter(rate.Limit(float64(rl.ratePerMin)/60.0), rl.ratePerMin)
	rl.limiters[ip] = lim
	return lim
}

// allow reports whether the request from ip may proceed. When it returns
// false, retryAfter is the duration the caller should advertise in the
// Retry-After header. A nil receiver is treated as "limiter disabled" → always
// allow, so callers do not have to nil-check.
func (rl *rateLimiter) allow(ip string) (bool, time.Duration) {
	if rl == nil {
		return true, 0
	}
	now := time.Now()
	lim := rl.limiterFor(ip, now)
	res := lim.ReserveN(now, 1)
	if !res.OK() {
		// Burst exceeded — should not happen since burst == ratePerMin > 0,
		// but guard anyway. Treat as deny with a 1-second backoff.
		return false, time.Second
	}
	delay := res.DelayFrom(now)
	if delay > 0 {
		// Hand the token back so it does not count against the bucket: this
		// request is being rejected, not queued.
		res.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// RateLimit returns an http.Handler middleware that enforces the per-IP rate
// limit on /api/download, /api/upload and /api/ping. All other paths pass
// through untouched. If h.limiter is nil (default), the middleware is a
// transparent pass-through.
func (h *Handler) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rl, _ := h.limiter.(*rateLimiter)
		if rl == nil {
			next.ServeHTTP(w, r)
			return
		}
		if _, scoped := rateLimitedPaths[r.URL.Path]; !scoped {
			next.ServeHTTP(w, r)
			return
		}

		ip := ClientIP(r)
		ok, retryAfter := rl.allow(ip)
		if ok {
			next.ServeHTTP(w, r)
			return
		}

		// Round up to the next whole second — Retry-After is integer-seconds
		// per RFC 7231 § 7.1.3 (delta-seconds form). A delay of 0.4 s still
		// means "not yet", so we advertise at least 1.
		secs := int(math.Ceil(retryAfter.Seconds()))
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(errResponse{Error: "rate limit exceeded"})
	})
}
