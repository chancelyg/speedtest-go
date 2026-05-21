package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
)

// benchCfg is sized for the hottest paths — small per-iteration totals so
// b.N can swing several orders of magnitude without exploding wall time.
func benchCfg() *config.Config {
	return &config.Config{
		Mode:          config.ModeTime,
		DownloadMB:    1,
		UploadMB:      1,
		Duration:      50 * time.Millisecond,
		Streams:       1,
		MaxConcurrent: 32,
	}
}

// BenchmarkDownloadByTime measures the time-mode streaming throughput from
// the server's perspective. The countingDiscardWriter sinks bytes without
// allocating, isolating Handler overhead from net/http response buffering.
// Reported numbers should be interpreted as a regression detector — the
// absolute rate is bounded by memory bandwidth on the test host.
func BenchmarkDownloadByTime(b *testing.B) {
	h := handler.New(benchCfg(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/download?duration=1", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := newCountingWriter()
		h.DownloadHandler(w, req)
		b.SetBytes(w.written)
	}
}

// BenchmarkDownloadBySize exercises the size-mode hot path. Each iteration
// streams exactly 1 MB so byte accounting is deterministic.
func BenchmarkDownloadBySize(b *testing.B) {
	h := handler.New(benchCfg(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/download?bytes=1048576", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := newCountingWriter()
		h.DownloadHandler(w, req)
	}
	b.SetBytes(1 << 20)
}

// BenchmarkClientIP covers the request-classification hot path that runs
// on every API call. We exercise three representative shapes so a future
// regression in any single branch shows up as a divergent series.
func BenchmarkClientIP(b *testing.B) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		xRealIP    string
	}{
		{
			name:       "public-peer-no-headers",
			remoteAddr: "203.0.113.42:54321",
		},
		{
			name:       "private-peer-trusted-xff",
			remoteAddr: "10.0.0.1:54321",
			xff:        "198.51.100.7, 10.0.0.1",
		},
		{
			name:       "loopback-peer-x-real-ip",
			remoteAddr: "127.0.0.1:54321",
			xRealIP:    "198.51.100.9",
		},
	}

	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/api/ip", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xRealIP != "" {
				req.Header.Set("X-Real-Ip", tc.xRealIP)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = handler.ClientIP(req)
			}
		})
	}
}
