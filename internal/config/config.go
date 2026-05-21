package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mode controls how the speed test measures throughput.
type Mode string

const (
	// ModeSize stops the test after transferring a fixed number of bytes.
	// Controlled by SPEEDTEST_DOWNLOAD_SIZE and SPEEDTEST_UPLOAD_SIZE.
	ModeSize Mode = "size"

	// ModeTime stops the test after a fixed wall-clock duration.
	// Controlled by SPEEDTEST_DURATION.
	ModeTime Mode = "time"
)

// Config holds all runtime configuration loaded from environment variables.
//
//	SPEEDTEST_HOST                     Bind address                default: ::      (dual-stack)
//	SPEEDTEST_PORT                     Listen port                 default: 8080
//	SPEEDTEST_MODE                     "size" or "time"            default: time
//	SPEEDTEST_DURATION                 Seconds (time mode)         default: 15
//	SPEEDTEST_DOWNLOAD_SIZE            Download MB (size mode)     default: 25
//	SPEEDTEST_UPLOAD_SIZE              Upload MB   (size mode)     default: 10
//	SPEEDTEST_STREAMS                  Parallel streams            default: 4
//	SPEEDTEST_MAX_CONCURRENT           Max simultaneous tests      default: 10
//	SPEEDTEST_WARMUP_MS                Throughput slow-start trim  default: 500
//	SPEEDTEST_DB_PATH                  SQLite history file path    default: ./speedtest.db  ("" = disable history)
//	SPEEDTEST_HISTORY_RETENTION_DAYS   Days to keep history        default: 90              (0 = keep forever)
//	SPEEDTEST_RATE_PER_MIN             Per-IP rate limit (req/min) default: 0               (0 = unlimited)
//	SPEEDTEST_CONFIG                   Optional JSON config file   default: "" (no file)
type Config struct {
	Host                 string
	Port                 string
	Mode                 Mode
	Duration             time.Duration
	DownloadMB           int
	UploadMB             int
	Streams              int
	MaxConcurrent        int
	WarmupMs             int    // Phase 2: drop first N ms of throughput samples (slow-start trim)
	DBPath               string // Phase 3: SQLite path; empty disables persistence
	HistoryRetentionDays int    // Phase 3: 0 = keep forever
	RatePerMin           int    // Phase 4 (B): per-IP requests/min limit; 0 = disabled (default)
	ConfigFilePath       string // Phase 4 (C): JSON config file path (CLI/env override)
}

// Load reads configuration from environment variables, applying defaults
// where variables are absent or invalid.
//
// Phase 4 Track C will introduce LoadWithSources(args, env) that layers a
// JSON config file underneath the env defaults and CLI flags on top. This
// Load() function is preserved as the env-only entry point.
func Load() *Config {
	return &Config{
		Host:                 envStr("SPEEDTEST_HOST", "::"),
		Port:                 envPort("SPEEDTEST_PORT", "8080"),
		Mode:                 envMode("SPEEDTEST_MODE", ModeTime),
		Duration:             envDuration("SPEEDTEST_DURATION", 15),
		DownloadMB:           envInt("SPEEDTEST_DOWNLOAD_SIZE", 25, 1, 10240),
		UploadMB:             envInt("SPEEDTEST_UPLOAD_SIZE", 10, 1, 10240),
		Streams:              envInt("SPEEDTEST_STREAMS", 4, 1, 32),
		MaxConcurrent:        envInt("SPEEDTEST_MAX_CONCURRENT", 10, 1, 100),
		WarmupMs:             envInt("SPEEDTEST_WARMUP_MS", 500, 0, 10_000),
		DBPath:               envStrAllowEmpty("SPEEDTEST_DB_PATH", "./speedtest.db"),
		HistoryRetentionDays: envInt("SPEEDTEST_HISTORY_RETENTION_DAYS", 90, 0, 3650),
		RatePerMin:           envInt("SPEEDTEST_RATE_PER_MIN", 0, 0, 100_000),
		ConfigFilePath:       envStrAllowEmpty("SPEEDTEST_CONFIG", ""),
	}
}

// Addr returns the combined "host:port" listen address.
// Addr returns the canonical "host:port" listen string.
//
// IPv6 host literals (containing ':') are wrapped in square brackets so the
// net.Listen TCP parser accepts them, e.g. "::" -> "[::]:8080". Hostnames
// and IPv4 literals pass through untouched.
func (c *Config) Addr() string {
	host := c.Host
	if strings.ContainsRune(host, ':') && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%s", host, c.Port)
}

// ── env helpers ───────────────────────────────────────────────────────────

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envStrAllowEmpty differs from envStr in that an explicitly-empty env var
// overrides the default with the empty string (used by DB_PATH where empty
// means "disable history persistence").
func envStrAllowEmpty(key, def string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	return v
}

func envPort(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if _, err := strconv.Atoi(v); err != nil {
		return def
	}
	return v
}

func envMode(key string, def Mode) Mode {
	switch Mode(os.Getenv(key)) {
	case ModeSize:
		return ModeSize
	case ModeTime:
		return ModeTime
	default:
		return def
	}
}

func envDuration(key string, defaultSecs int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defaultSecs) * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return time.Duration(defaultSecs) * time.Second
	}
	return time.Duration(n) * time.Second
}

func envInt(key string, def, min, max int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}
