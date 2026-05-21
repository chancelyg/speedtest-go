package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// CLIOpts captures CLI-only flags that are not part of the runtime Config but
// drive process-level behavior (printing version + exiting, etc.).
type CLIOpts struct {
	// ShowVersion was requested via --version. Main should print the
	// version/commit/date triplet and exit with status 0.
	ShowVersion bool
}

// Load reads configuration from environment variables, applying defaults
// where variables are absent or invalid. This is the env-only entry point and
// is preserved for compatibility with existing tests and callers that don't
// need CLI flag or JSON config-file support.
//
// New callers should prefer LoadWithSources, which layers JSON file values
// underneath env values and CLI flags on top.
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

// LoadWithSources builds the runtime Config by layering, from lowest to
// highest precedence:
//
//  1. compiled-in defaults
//  2. JSON config file (searched in a fixed order; see configPath)
//  3. environment variables (SPEEDTEST_*)
//  4. CLI flags (long form, e.g. --port 9999)
//
// args is the argv slice *without* the program name (caller typically passes
// os.Args[1:]). env is an env lookup function (typically os.Getenv); injecting
// it makes the function trivially unit-testable. nil env means "use
// os.Getenv".
//
// LoadWithSources returns the resolved Config, a CLIOpts struct carrying
// process-level flags such as ShowVersion, and an error for unparseable flags
// or unreadable config files. It does NOT exit on --version: the caller is
// responsible for honouring opts.ShowVersion to keep this function pure.
func LoadWithSources(args []string, env func(string) string) (*Config, *CLIOpts, error) {
	if env == nil {
		env = os.Getenv
	}

	// Parse CLI flags first so we know whether the user supplied an explicit
	// --config <path> that should override the env/search-path discovery.
	cli, opts, err := parseCLI(args)
	if err != nil {
		return nil, nil, err
	}

	// Start from defaults, overlay the JSON file (if any), then overlay
	// env-set values, then overlay CLI-set values. Each subsequent layer
	// only touches the fields the operator explicitly provided at that
	// layer, leaving lower layers visible underneath.
	cfg := defaults()

	path := configPath(cli.configPath, env, fileSystemSearch)
	cfg.ConfigFilePath = path
	if path != "" {
		fc, ferr := readJSONFile(path)
		if ferr != nil {
			return nil, nil, fmt.Errorf("config file %s: %w", path, ferr)
		}
		applyJSON(cfg, fc)
	}

	overlayEnv(cfg, env)
	applyCLI(cfg, cli)

	// Echo back the resolved config path even when env supplied it but the
	// file didn't exist — operators expect to see whatever they passed.
	if cfg.ConfigFilePath == "" {
		if v := env("SPEEDTEST_CONFIG"); v != "" {
			cfg.ConfigFilePath = v
		}
	}

	return cfg, opts, nil
}

// defaults returns a Config populated with the hard-coded baseline values.
// It is the lowest layer of the LoadWithSources precedence chain.
func defaults() *Config {
	return &Config{
		Host:                 "::",
		Port:                 "8080",
		Mode:                 ModeTime,
		Duration:             15 * time.Second,
		DownloadMB:           25,
		UploadMB:             10,
		Streams:              4,
		MaxConcurrent:        10,
		WarmupMs:             500,
		DBPath:               "./speedtest.db",
		HistoryRetentionDays: 90,
		RatePerMin:           0,
		ConfigFilePath:       "",
	}
}

// ── CLI parsing ───────────────────────────────────────────────────────────

// cliValues collects every flag value alongside a "was it set" bit so the
// caller can distinguish "user supplied --port 8080" from "user did not
// supply --port at all".
type cliValues struct {
	configPath string

	host          string
	port          string
	mode          string
	durationSec   int
	streams       int
	downloadMB    int
	uploadMB      int
	maxConcurrent int
	warmupMs      int
	dbPath        string
	ratePerMin    int

	setHost          bool
	setPort          bool
	setMode          bool
	setDurationSec   bool
	setStreams       bool
	setDownloadMB    bool
	setUploadMB      bool
	setMaxConcurrent bool
	setWarmupMs      bool
	setDBPath        bool
	setRatePerMin    bool
}

// parseCLI runs the flag.FlagSet over args and reports which flags were
// explicitly set so applyCLI can selectively overlay them later. Errors are
// surfaced as values; --help is treated as an error so main can exit 0.
func parseCLI(args []string) (cliValues, *CLIOpts, error) {
	var cli cliValues
	opts := &CLIOpts{}

	fs := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we surface errors as values, not stderr noise

	fs.StringVar(&cli.configPath, "config", "", "Path to JSON config file (overrides SPEEDTEST_CONFIG and search paths)")
	fs.StringVar(&cli.host, "host", "", "Bind address (default \"::\")")
	fs.StringVar(&cli.port, "port", "", "Listen port (default \"8080\")")
	fs.StringVar(&cli.mode, "mode", "", "Speed-test mode: time | size")
	fs.IntVar(&cli.durationSec, "duration", 0, "Time-mode duration in seconds")
	fs.IntVar(&cli.streams, "streams", 0, "Parallel stream count")
	fs.IntVar(&cli.downloadMB, "download-mb", 0, "Download size in MB (size mode)")
	fs.IntVar(&cli.uploadMB, "upload-mb", 0, "Upload size in MB (size mode)")
	fs.IntVar(&cli.maxConcurrent, "max-concurrent", 0, "Max simultaneous tests across all clients")
	fs.IntVar(&cli.warmupMs, "warmup-ms", 0, "Throughput slow-start trim in milliseconds")
	fs.StringVar(&cli.dbPath, "db-path", "", "SQLite history path (\"\" disables persistence)")
	fs.IntVar(&cli.ratePerMin, "rate-per-min", 0, "Per-IP rate limit (req/min, 0 = unlimited)")
	fs.BoolVar(&opts.ShowVersion, "version", false, "Print version information and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cli, opts, err
		}
		return cli, opts, fmt.Errorf("parse flags: %w", err)
	}

	// flag.Visit only walks flags that were explicitly set on the command
	// line — exactly the "was it set?" signal we need.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "host":
			cli.setHost = true
		case "port":
			cli.setPort = true
		case "mode":
			cli.setMode = true
		case "duration":
			cli.setDurationSec = true
		case "streams":
			cli.setStreams = true
		case "download-mb":
			cli.setDownloadMB = true
		case "upload-mb":
			cli.setUploadMB = true
		case "max-concurrent":
			cli.setMaxConcurrent = true
		case "warmup-ms":
			cli.setWarmupMs = true
		case "db-path":
			cli.setDBPath = true
		case "rate-per-min":
			cli.setRatePerMin = true
		}
	})

	return cli, opts, nil
}

// applyCLI overlays each explicitly-set CLI flag onto cfg, clamping numeric
// values into the same safe ranges that env parsing enforces.
func applyCLI(cfg *Config, cli cliValues) {
	if cli.setHost {
		cfg.Host = cli.host
	}
	if cli.setPort {
		if _, err := strconv.Atoi(cli.port); err == nil {
			cfg.Port = cli.port
		}
	}
	if cli.setMode {
		switch Mode(cli.mode) {
		case ModeSize:
			cfg.Mode = ModeSize
		case ModeTime:
			cfg.Mode = ModeTime
		}
	}
	if cli.setDurationSec && cli.durationSec >= 1 {
		cfg.Duration = time.Duration(cli.durationSec) * time.Second
	}
	if cli.setStreams {
		cfg.Streams = clamp(cli.streams, 1, 32, cfg.Streams)
	}
	if cli.setDownloadMB {
		cfg.DownloadMB = clamp(cli.downloadMB, 1, 10240, cfg.DownloadMB)
	}
	if cli.setUploadMB {
		cfg.UploadMB = clamp(cli.uploadMB, 1, 10240, cfg.UploadMB)
	}
	if cli.setMaxConcurrent {
		cfg.MaxConcurrent = clamp(cli.maxConcurrent, 1, 100, cfg.MaxConcurrent)
	}
	if cli.setWarmupMs {
		cfg.WarmupMs = clamp(cli.warmupMs, 0, 10_000, cfg.WarmupMs)
	}
	if cli.setDBPath {
		// "" is intentional: disables history persistence.
		cfg.DBPath = cli.dbPath
	}
	if cli.setRatePerMin {
		cfg.RatePerMin = clamp(cli.ratePerMin, 0, 100_000, cfg.RatePerMin)
	}
}

// clamp returns n if it sits within [min, max]; otherwise it returns the
// fallback value. The caller passes the *existing* cfg value as the fallback
// so out-of-range input never silently demotes a sensible default.
func clamp(n, min, max, fallback int) int {
	if n < min || n > max {
		return fallback
	}
	return n
}

// ── JSON file loading ─────────────────────────────────────────────────────

// jsonConfig mirrors the subset of fields a JSON config file is allowed to
// set. Every field is a pointer so we can distinguish "absent" from "zero".
type jsonConfig struct {
	Host                 *string `json:"host,omitempty"`
	Port                 *string `json:"port,omitempty"`
	Mode                 *string `json:"mode,omitempty"`
	DurationSec          *int    `json:"duration_sec,omitempty"`
	DownloadMB           *int    `json:"download_mb,omitempty"`
	UploadMB             *int    `json:"upload_mb,omitempty"`
	Streams              *int    `json:"streams,omitempty"`
	MaxConcurrent        *int    `json:"max_concurrent,omitempty"`
	WarmupMs             *int    `json:"warmup_ms,omitempty"`
	DBPath               *string `json:"db_path,omitempty"`
	HistoryRetentionDays *int    `json:"history_retention_days,omitempty"`
	RatePerMin           *int    `json:"rate_per_min,omitempty"`
}

// readJSONFile parses path with strict (unknown-fields-rejected) decoding.
// We choose strict to fail fast on typos like "post" instead of "port" —
// otherwise misconfiguration silently falls back to defaults, which is
// extremely confusing for operators in production.
func readJSONFile(path string) (jsonConfig, error) {
	var jc jsonConfig
	f, err := os.Open(path) //nolint:gosec // operator-provided path is intentional
	if err != nil {
		return jc, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&jc); err != nil {
		return jc, fmt.Errorf("decode JSON: %w", err)
	}
	return jc, nil
}

// applyJSON overlays the file values onto cfg. Same clamping rules as env
// and CLI to keep the binary unstartable-free in the face of typos.
func applyJSON(cfg *Config, fc jsonConfig) {
	if fc.Host != nil {
		cfg.Host = *fc.Host
	}
	if fc.Port != nil {
		if _, err := strconv.Atoi(*fc.Port); err == nil {
			cfg.Port = *fc.Port
		}
	}
	if fc.Mode != nil {
		switch Mode(*fc.Mode) {
		case ModeSize:
			cfg.Mode = ModeSize
		case ModeTime:
			cfg.Mode = ModeTime
		}
	}
	if fc.DurationSec != nil && *fc.DurationSec >= 1 {
		cfg.Duration = time.Duration(*fc.DurationSec) * time.Second
	}
	if fc.DownloadMB != nil {
		cfg.DownloadMB = clamp(*fc.DownloadMB, 1, 10240, cfg.DownloadMB)
	}
	if fc.UploadMB != nil {
		cfg.UploadMB = clamp(*fc.UploadMB, 1, 10240, cfg.UploadMB)
	}
	if fc.Streams != nil {
		cfg.Streams = clamp(*fc.Streams, 1, 32, cfg.Streams)
	}
	if fc.MaxConcurrent != nil {
		cfg.MaxConcurrent = clamp(*fc.MaxConcurrent, 1, 100, cfg.MaxConcurrent)
	}
	if fc.WarmupMs != nil {
		cfg.WarmupMs = clamp(*fc.WarmupMs, 0, 10_000, cfg.WarmupMs)
	}
	if fc.DBPath != nil {
		cfg.DBPath = *fc.DBPath
	}
	if fc.HistoryRetentionDays != nil {
		cfg.HistoryRetentionDays = clamp(*fc.HistoryRetentionDays, 0, 3650, cfg.HistoryRetentionDays)
	}
	if fc.RatePerMin != nil {
		cfg.RatePerMin = clamp(*fc.RatePerMin, 0, 100_000, cfg.RatePerMin)
	}
}

// fileSystemSearch is the production implementation of "first existing file
// wins" search. It is a package-level variable so tests can stub it out and
// pass a deterministic candidate list.
var fileSystemSearch = func(candidates []string) string {
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// configPath resolves the JSON config file location using the precedence:
//
//  1. CLI --config <path>            (explicit; returned even if missing)
//  2. SPEEDTEST_CONFIG env var       (explicit; returned even if missing)
//  3. ./speedtest.json
//  4. $XDG_CONFIG_HOME/speedtest/config.json (or $HOME/.config/...)
//  5. /etc/speedtest/config.json
//
// Explicit paths (1 and 2) propagate even when the file doesn't exist so the
// caller can report a meaningful "config file not found" error. Implicit
// search-path entries silently fall through when absent.
func configPath(cliPath string, env func(string) string, search func([]string) string) string {
	if cliPath != "" {
		return cliPath
	}
	if p := env("SPEEDTEST_CONFIG"); p != "" {
		return p
	}
	candidates := []string{
		"./speedtest.json",
		xdgConfigPath(env),
		"/etc/speedtest/config.json",
	}
	return search(candidates)
}

// xdgConfigPath returns the XDG-style user config location, falling back to
// ~/.config/speedtest/config.json when $XDG_CONFIG_HOME is unset.
func xdgConfigPath(env func(string) string) string {
	if xdg := env("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "speedtest", "config.json")
	}
	if home := env("HOME"); home != "" {
		return filepath.Join(home, ".config", "speedtest", "config.json")
	}
	return ""
}

// overlayEnv selectively copies SPEEDTEST_* values from env onto cfg. Unset
// env vars leave the existing cfg field untouched, preserving the file
// overlay (or default) underneath. Numeric parse failures and out-of-range
// values are ignored (the underlying layer's value wins) rather than
// downgrading to the hard-coded default — the operator's intent is closer
// to the file/default than to a numeric reset.
func overlayEnv(cfg *Config, env func(string) string) {
	if v := env("SPEEDTEST_HOST"); v != "" {
		cfg.Host = v
	}
	if v := env("SPEEDTEST_PORT"); v != "" {
		if _, err := strconv.Atoi(v); err == nil {
			cfg.Port = v
		}
	}
	if v := env("SPEEDTEST_MODE"); v != "" {
		switch Mode(v) {
		case ModeSize:
			cfg.Mode = ModeSize
		case ModeTime:
			cfg.Mode = ModeTime
		}
	}
	if v := env("SPEEDTEST_DURATION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			cfg.Duration = time.Duration(n) * time.Second
		}
	}
	if v := env("SPEEDTEST_DOWNLOAD_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DownloadMB = clamp(n, 1, 10240, cfg.DownloadMB)
		}
	}
	if v := env("SPEEDTEST_UPLOAD_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.UploadMB = clamp(n, 1, 10240, cfg.UploadMB)
		}
	}
	if v := env("SPEEDTEST_STREAMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Streams = clamp(n, 1, 32, cfg.Streams)
		}
	}
	if v := env("SPEEDTEST_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrent = clamp(n, 1, 100, cfg.MaxConcurrent)
		}
	}
	if v := env("SPEEDTEST_WARMUP_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WarmupMs = clamp(n, 0, 10_000, cfg.WarmupMs)
		}
	}
	// DB_PATH: any non-empty env value overrides; explicit "" via env is not
	// supported by the injected getter contract (use the JSON file or CLI
	// flag if you need to set DBPath="" to disable persistence).
	if v := env("SPEEDTEST_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := env("SPEEDTEST_HISTORY_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.HistoryRetentionDays = clamp(n, 0, 3650, cfg.HistoryRetentionDays)
		}
	}
	if v := env("SPEEDTEST_RATE_PER_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RatePerMin = clamp(n, 0, 100_000, cfg.RatePerMin)
		}
	}
}

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

// ── env helpers (legacy Load path) ────────────────────────────────────────

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
