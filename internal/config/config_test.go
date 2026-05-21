package config_test

import (
	"os"
	"path/filepath"
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

	// Phase 4.6: default host is "::" so the listener accepts both v4 and v6.
	if cfg.Host != "::" {
		t.Errorf("Host = %q, want %q", cfg.Host, "::")
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

func TestAddrWrapsIPv6InBrackets(t *testing.T) {
	cases := []struct {
		host, port, want string
	}{
		{"::", "8080", "[::]:8080"},
		{"::1", "9090", "[::1]:9090"},
		{"0.0.0.0", "8080", "0.0.0.0:8080"},
		{"localhost", "8080", "localhost:8080"},
		{"127.0.0.1", "8080", "127.0.0.1:8080"},
	}
	for _, tc := range cases {
		cfg := &config.Config{Host: tc.host, Port: tc.port}
		if got := cfg.Addr(); got != tc.want {
			t.Errorf("Addr(host=%q, port=%q) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

// ── LoadWithSources (Phase 4 Track C) ─────────────────────────────────────

// mockEnv returns an env getter backed by the supplied map. Missing keys
// resolve to the empty string, matching os.Getenv semantics.
func mockEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// writeJSON drops a JSON config file into a temp dir and returns its path.
func writeJSON(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "speedtest.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	return path
}

func TestLoadWithSources_EnvOnly(t *testing.T) {
	env := mockEnv(map[string]string{
		"SPEEDTEST_PORT": "8080",
		"SPEEDTEST_HOST": "127.0.0.1",
	})
	cfg, opts, err := config.LoadWithSources(nil, env)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if opts.ShowVersion {
		t.Errorf("ShowVersion should be false")
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1", cfg.Host)
	}
}

func TestLoadWithSources_CLIOverridesEnv(t *testing.T) {
	env := mockEnv(map[string]string{"SPEEDTEST_PORT": "8080"})
	cfg, _, err := config.LoadWithSources([]string{"--port", "9999"}, env)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != "9999" {
		t.Errorf("Port = %q, want 9999 (CLI must beat env)", cfg.Port)
	}
}

func TestLoadWithSources_EnvOverridesFile(t *testing.T) {
	path := writeJSON(t, `{"port":"7777"}`)
	env := mockEnv(map[string]string{
		"SPEEDTEST_PORT":   "8888",
		"SPEEDTEST_CONFIG": path,
	})
	cfg, _, err := config.LoadWithSources(nil, env)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != "8888" {
		t.Errorf("Port = %q, want 8888 (env must beat file)", cfg.Port)
	}
}

func TestLoadWithSources_AllThreeLayers(t *testing.T) {
	path := writeJSON(t, `{"port":"7777","host":"1.1.1.1"}`)
	env := mockEnv(map[string]string{
		"SPEEDTEST_PORT":   "8888",
		"SPEEDTEST_CONFIG": path,
	})
	cfg, _, err := config.LoadWithSources([]string{"--port", "9999"}, env)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != "9999" {
		t.Errorf("Port = %q, want 9999 (CLI top of stack)", cfg.Port)
	}
	// host was set only in the file — it should still show through.
	if cfg.Host != "1.1.1.1" {
		t.Errorf("Host = %q, want 1.1.1.1 (file value visible)", cfg.Host)
	}
}

func TestLoadWithSources_CLIConfigFlag(t *testing.T) {
	path := writeJSON(t, `{"port":"6666"}`)
	cfg, _, err := config.LoadWithSources([]string{"--config", path}, mockEnv(nil))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != "6666" {
		t.Errorf("Port = %q, want 6666 (from --config path)", cfg.Port)
	}
	if cfg.ConfigFilePath != path {
		t.Errorf("ConfigFilePath = %q, want %q", cfg.ConfigFilePath, path)
	}
}

func TestLoadWithSources_VersionFlag(t *testing.T) {
	_, opts, err := config.LoadWithSources([]string{"--version"}, mockEnv(nil))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !opts.ShowVersion {
		t.Errorf("ShowVersion should be true after --version")
	}
}

func TestLoadWithSources_InvalidFlag(t *testing.T) {
	_, _, err := config.LoadWithSources([]string{"--this-flag-does-not-exist"}, mockEnv(nil))
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestLoadWithSources_InvalidConfigPath(t *testing.T) {
	_, _, err := config.LoadWithSources(
		[]string{"--config", "/nonexistent/path/to/speedtest.json"},
		mockEnv(nil),
	)
	if err == nil {
		t.Fatal("expected error for missing --config path, got nil")
	}
}

func TestLoadWithSources_UnknownJSONField(t *testing.T) {
	// Strict decoding: unknown fields should fail loudly.
	path := writeJSON(t, `{"post":"7777"}`) // "post" instead of "port"
	_, _, err := config.LoadWithSources(nil, mockEnv(map[string]string{
		"SPEEDTEST_CONFIG": path,
	}))
	if err == nil {
		t.Fatal("expected error for unknown JSON field, got nil")
	}
}

func TestLoadWithSources_AllCLIFlags(t *testing.T) {
	args := []string{
		"--host", "10.0.0.1",
		"--port", "1234",
		"--mode", "size",
		"--duration", "20",
		"--streams", "8",
		"--download-mb", "50",
		"--upload-mb", "20",
		"--max-concurrent", "5",
		"--warmup-ms", "1000",
		"--db-path", "/tmp/foo.db",
		"--rate-per-min", "60",
	}
	cfg, _, err := config.LoadWithSources(args, mockEnv(nil))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Host", cfg.Host, "10.0.0.1"},
		{"Port", cfg.Port, "1234"},
		{"Mode", string(cfg.Mode), "size"},
		{"Duration", cfg.Duration, 20 * time.Second},
		{"Streams", cfg.Streams, 8},
		{"DownloadMB", cfg.DownloadMB, 50},
		{"UploadMB", cfg.UploadMB, 20},
		{"MaxConcurrent", cfg.MaxConcurrent, 5},
		{"WarmupMs", cfg.WarmupMs, 1000},
		{"DBPath", cfg.DBPath, "/tmp/foo.db"},
		{"RatePerMin", cfg.RatePerMin, 60},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadWithSources_FileFullSchema(t *testing.T) {
	path := writeJSON(t, `{
		"host": "0.0.0.0",
		"port": "8181",
		"mode": "size",
		"duration_sec": 30,
		"download_mb": 100,
		"upload_mb": 40,
		"streams": 2,
		"max_concurrent": 20,
		"warmup_ms": 250,
		"db_path": "/var/lib/speedtest.db",
		"history_retention_days": 7,
		"rate_per_min": 120
	}`)
	cfg, _, err := config.LoadWithSources(nil, mockEnv(map[string]string{
		"SPEEDTEST_CONFIG": path,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != "8181" || cfg.Mode != config.ModeSize {
		t.Errorf("file values not applied: host=%q port=%q mode=%q", cfg.Host, cfg.Port, cfg.Mode)
	}
	if cfg.Duration != 30*time.Second {
		t.Errorf("Duration = %v, want 30s", cfg.Duration)
	}
	if cfg.DownloadMB != 100 || cfg.UploadMB != 40 || cfg.Streams != 2 {
		t.Errorf("transfer params wrong: dl=%d ul=%d streams=%d", cfg.DownloadMB, cfg.UploadMB, cfg.Streams)
	}
	if cfg.MaxConcurrent != 20 || cfg.WarmupMs != 250 || cfg.RatePerMin != 120 {
		t.Errorf("limits wrong: maxC=%d warmup=%d rate=%d", cfg.MaxConcurrent, cfg.WarmupMs, cfg.RatePerMin)
	}
	if cfg.DBPath != "/var/lib/speedtest.db" || cfg.HistoryRetentionDays != 7 {
		t.Errorf("history wrong: db=%q ret=%d", cfg.DBPath, cfg.HistoryRetentionDays)
	}
}

func TestLoadWithSources_DefaultsWhenNothingSet(t *testing.T) {
	// No CLI, no env, no file.
	cfg, _, err := config.LoadWithSources(nil, mockEnv(nil))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Host != "::" || cfg.Port != "8080" || cfg.Mode != config.ModeTime {
		t.Errorf("defaults not applied: host=%q port=%q mode=%q", cfg.Host, cfg.Port, cfg.Mode)
	}
	if cfg.Duration != 15*time.Second || cfg.Streams != 4 {
		t.Errorf("baseline defaults wrong: dur=%v streams=%d", cfg.Duration, cfg.Streams)
	}
}

func TestLoadWithSources_CLIOverridesFileOnly(t *testing.T) {
	// Validates that CLI still wins when env is empty.
	path := writeJSON(t, `{"port":"7777"}`)
	cfg, _, err := config.LoadWithSources(
		[]string{"--config", path, "--port", "9999"},
		mockEnv(nil),
	)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != "9999" {
		t.Errorf("Port = %q, want 9999 (CLI must beat file)", cfg.Port)
	}
}

func TestLoadWithSources_NilEnvFallsBackToOS(t *testing.T) {
	// Pass nil env: function should default to os.Getenv without panic.
	t.Setenv("SPEEDTEST_PORT", "5500")
	cfg, _, err := config.LoadWithSources(nil, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Port != "5500" {
		t.Errorf("Port = %q, want 5500 (nil env should resolve to os.Getenv)", cfg.Port)
	}
}
