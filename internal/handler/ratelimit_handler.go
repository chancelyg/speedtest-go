package handler

// Phase 4 Track B — per-IP rate limiting middleware.
//
// Contract for agent B:
//
// Public surface:
//   - func (h *Handler) RateLimit(next http.Handler) http.Handler
//     — middleware exposed for main.go [P4-B] to wrap the mux. When
//     cfg.RatePerMin == 0 the middleware is a no-op (default state, single-
//     machine deployment).
//
// Internal state (kept on Handler as a *rateLimiter field, defined here):
//   - map[string]*rate.Limiter keyed by ClientIP (re-use handler.ClientIP
//     so the limiter respects trusted-proxy IP forwarding).
//   - sync.Mutex around the map.
//   - background goroutine (started in New() when RatePerMin > 0) that
//     evicts limiter entries last touched > 15 minutes ago.
//
// Response semantics on rejection:
//   - 429 Too Many Requests
//   - Retry-After: <seconds until next token>  (compute from rate.Limiter.Reserve())
//   - JSON body: {"error":"rate limit exceeded"}
//
// Scope: ONLY the /api/download, /api/upload, /api/ping endpoints. /metrics,
// /healthz, /api/config, /api/ip, /api/results* are exempt — they are cheap
// status / inventory calls and counting them would hide DoS attempts.
//
// Dependency: golang.org/x/time/rate (pure Go, semi-stdlib).
//
// Skeleton placeholder — the real implementation lands via agent B.
