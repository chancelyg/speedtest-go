package config_test

import (
	"os"
	"testing"
	"time"

	"speedtest-go/internal/config"
)

func TestDefaultValues(t *testing.T) {
	os.Unsetenv("SPEEDTEST_HOST")
	os.Unsetenv("SPEEDTEST_PORT")
	os.Unsetenv("SPEEDTEST_MODE")
	os.Unsetenv("SPEEDTEST_DURATION")
	os.Unsetenv("SPEEDTEST_DOWNLOAD_SIZE")
	os.Unsetenv("SPEEDTEST_UPLOAD_SIZE")
	os.Unsetenv("SPEEDTEST_STREAMS")

	cfg := config.Load()

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.Mode != config.ModeTime {
		t.Errorf("Mode = %q, want %q", cfg.Mode, config.ModeTime)
	}
	if cfg.Duration != 15*time.Second {
		t.Errorf("Duration = %v, want 15s", cfg.Duration)
	}
	if cfg.DownloadMB != 25 {
		t.Errorf("DownloadMB = %d, want 25", cfg.DownloadMB)
	}
	if cfg.UploadMB != 10 {
		t.Errorf("UploadMB = %d, want 10", cfg.UploadMB)
	}
	if cfg.Streams != 4 {
		t.Errorf("Streams = %d, want 4", cfg.Streams)
	}
}

func TestHostFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_HOST", "127.0.0.1")
	defer os.Unsetenv("SPEEDTEST_HOST")
	cfg := config.Load()
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
}

func TestPortFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_PORT", "9090")
	defer os.Unsetenv("SPEEDTEST_PORT")
	cfg := config.Load()
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
}

func TestAddr(t *testing.T) {
	os.Setenv("SPEEDTEST_HOST", "0.0.0.0")
	os.Setenv("SPEEDTEST_PORT", "3000")
	defer os.Unsetenv("SPEEDTEST_HOST")
	defer os.Unsetenv("SPEEDTEST_PORT")
	cfg := config.Load()
	if cfg.Addr() != "0.0.0.0:3000" {
		t.Errorf("Addr() = %q, want %q", cfg.Addr(), "0.0.0.0:3000")
	}
}

func TestInvalidPortFallsBackToDefault(t *testing.T) {
	os.Setenv("SPEEDTEST_PORT", "notaport")
	defer os.Unsetenv("SPEEDTEST_PORT")
	cfg := config.Load()
	if cfg.Port != "8080" {
		t.Errorf("invalid port should fallback to 8080, got %q", cfg.Port)
	}
}

func TestModeTimeFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_MODE", "time")
	defer os.Unsetenv("SPEEDTEST_MODE")
	cfg := config.Load()
	if cfg.Mode != config.ModeTime {
		t.Errorf("Mode = %q, want %q", cfg.Mode, config.ModeTime)
	}
}

func TestModeSizeFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_MODE", "size")
	defer os.Unsetenv("SPEEDTEST_MODE")
	cfg := config.Load()
	if cfg.Mode != config.ModeSize {
		t.Errorf("Mode = %q, want %q", cfg.Mode, config.ModeSize)
	}
}

func TestInvalidModeFallsBackToDefault(t *testing.T) {
	os.Setenv("SPEEDTEST_MODE", "invalid")
	defer os.Unsetenv("SPEEDTEST_MODE")
	cfg := config.Load()
	if cfg.Mode != config.ModeTime {
		t.Errorf("invalid mode should fallback to time, got %q", cfg.Mode)
	}
}

func TestDurationFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_DURATION", "15")
	defer os.Unsetenv("SPEEDTEST_DURATION")
	cfg := config.Load()
	if cfg.Duration != 15*time.Second {
		t.Errorf("Duration = %v, want 15s", cfg.Duration)
	}
}

func TestInvalidDurationFallsBackToDefault(t *testing.T) {
	os.Setenv("SPEEDTEST_DURATION", "bad")
	defer os.Unsetenv("SPEEDTEST_DURATION")
	cfg := config.Load()
	if cfg.Duration != 15*time.Second {
		t.Errorf("invalid duration should fallback to 15s, got %v", cfg.Duration)
	}
}

func TestDurationMinimumClamped(t *testing.T) {
	os.Setenv("SPEEDTEST_DURATION", "0")
	defer os.Unsetenv("SPEEDTEST_DURATION")
	cfg := config.Load()
	if cfg.Duration < time.Second {
		t.Errorf("Duration %v should be clamped to at least 1s", cfg.Duration)
	}
}

func TestDownloadSizeFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_DOWNLOAD_SIZE", "100")
	defer os.Unsetenv("SPEEDTEST_DOWNLOAD_SIZE")
	cfg := config.Load()
	if cfg.DownloadMB != 100 {
		t.Errorf("DownloadMB = %d, want 100", cfg.DownloadMB)
	}
}

func TestUploadSizeFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_UPLOAD_SIZE", "50")
	defer os.Unsetenv("SPEEDTEST_UPLOAD_SIZE")
	cfg := config.Load()
	if cfg.UploadMB != 50 {
		t.Errorf("UploadMB = %d, want 50", cfg.UploadMB)
	}
}

// ── SPEEDTEST_MAX_CONCURRENT ───────────────────────────────────────────────

// TestMaxConcurrentDefault verifies the zero-env default is 10.
func TestMaxConcurrentDefault(t *testing.T) {
	os.Unsetenv("SPEEDTEST_MAX_CONCURRENT")
	cfg := config.Load()
	if cfg.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent = %d, want 10 (default)", cfg.MaxConcurrent)
	}
}

// TestMaxConcurrentFromEnv verifies that a valid SPEEDTEST_MAX_CONCURRENT
// value is loaded correctly.
func TestMaxConcurrentFromEnv(t *testing.T) {
	os.Setenv("SPEEDTEST_MAX_CONCURRENT", "25")
	defer os.Unsetenv("SPEEDTEST_MAX_CONCURRENT")
	cfg := config.Load()
	if cfg.MaxConcurrent != 25 {
		t.Errorf("MaxConcurrent = %d, want 25", cfg.MaxConcurrent)
	}
}

// TestMaxConcurrentInvalidFallsBackToDefault verifies that a non-numeric value
// causes the default (10) to be used.
func TestMaxConcurrentInvalidFallsBackToDefault(t *testing.T) {
	os.Setenv("SPEEDTEST_MAX_CONCURRENT", "notanumber")
	defer os.Unsetenv("SPEEDTEST_MAX_CONCURRENT")
	cfg := config.Load()
	if cfg.MaxConcurrent != 10 {
		t.Errorf("invalid MaxConcurrent should fallback to 10, got %d", cfg.MaxConcurrent)
	}
}

// TestMaxConcurrentBelowMinimumFallsBackToDefault verifies that 0 (below min
// of 1) causes the default to be used.
func TestMaxConcurrentBelowMinimumFallsBackToDefault(t *testing.T) {
	os.Setenv("SPEEDTEST_MAX_CONCURRENT", "0")
	defer os.Unsetenv("SPEEDTEST_MAX_CONCURRENT")
	cfg := config.Load()
	if cfg.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent=0 should fallback to 10, got %d", cfg.MaxConcurrent)
	}
}

// TestMaxConcurrentCappedAtMaximum verifies that values above 100 are clamped
// to 100 (the envInt ceiling for this field).
func TestMaxConcurrentCappedAtMaximum(t *testing.T) {
	os.Setenv("SPEEDTEST_MAX_CONCURRENT", "9999")
	defer os.Unsetenv("SPEEDTEST_MAX_CONCURRENT")
	cfg := config.Load()
	if cfg.MaxConcurrent != 100 {
		t.Errorf("MaxConcurrent=9999 should be clamped to 100, got %d", cfg.MaxConcurrent)
	}
}
