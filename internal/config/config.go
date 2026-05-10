package config

import (
	"fmt"
	"os"
	"strconv"
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
//	SPEEDTEST_HOST             Bind address                default: 0.0.0.0
//	SPEEDTEST_PORT             Listen port                 default: 8080
//	SPEEDTEST_MODE             "size" or "time"            default: time
//	SPEEDTEST_DURATION         Seconds (time mode)         default: 15
//	SPEEDTEST_DOWNLOAD_SIZE    Download MB (size mode)     default: 25
//	SPEEDTEST_UPLOAD_SIZE      Upload MB   (size mode)     default: 10
//	SPEEDTEST_STREAMS          Parallel streams            default: 4
//	SPEEDTEST_MAX_CONCURRENT   Max simultaneous tests      default: 10
type Config struct {
	Host          string
	Port          string
	Mode          Mode
	Duration      time.Duration
	DownloadMB    int
	UploadMB      int
	Streams       int
	MaxConcurrent int
}

// Load reads configuration from environment variables, applying defaults
// where variables are absent or invalid.
func Load() *Config {
	return &Config{
		Host:          envStr("SPEEDTEST_HOST", "0.0.0.0"),
		Port:          envPort("SPEEDTEST_PORT", "8080"),
		Mode:          envMode("SPEEDTEST_MODE", ModeTime),
		Duration:      envDuration("SPEEDTEST_DURATION", 15),
		DownloadMB:    envInt("SPEEDTEST_DOWNLOAD_SIZE", 25, 1, 10240),
		UploadMB:      envInt("SPEEDTEST_UPLOAD_SIZE", 10, 1, 10240),
		Streams:       envInt("SPEEDTEST_STREAMS", 4, 1, 32),
		MaxConcurrent: envInt("SPEEDTEST_MAX_CONCURRENT", 10, 1, 100),
	}
}

// Addr returns the combined "host:port" listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// ── env helpers ───────────────────────────────────────────────────────────

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
