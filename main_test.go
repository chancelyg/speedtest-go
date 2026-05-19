package main

import (
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
