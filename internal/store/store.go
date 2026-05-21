// Package store persists speed-test results for the single-binary deployment.
//
// Contract notes for Phase 2-3 agent B1:
//
//   - The Store interface is the only thing the rest of the codebase should
//     depend on. Concrete implementations live in this package.
//   - A nil Store value (or a *NoopStore) is acceptable when SPEEDTEST_DB_PATH
//     is empty; callers must handle that with feature-flag logic.
//   - The SQLite implementation uses modernc.org/sqlite (pure Go, no CGO) and
//     opens the database in WAL mode with busy_timeout=5000ms.
//
// Skeleton only — full implementation lives in store_sqlite.go (B1 to create).
package store

import (
	"context"
	"errors"
)

// Result is the single row persisted per completed speed test. It mirrors
// the JSON contract documented in internal/handler/results_handler.go so the
// HTTP API can encode/decode without an intermediate DTO.
type Result struct {
	ID                int64   `json:"id,omitempty"`
	CreatedAt         int64   `json:"created_at"` // unix ms
	DownloadMbps      float64 `json:"download_mbps"`
	UploadMbps        float64 `json:"upload_mbps"`
	LatencyIdleMs     float64 `json:"latency_idle_ms"`
	LatencyLoadedMs   float64 `json:"latency_loaded_ms"`
	DownloadJitterMs  float64 `json:"download_jitter_ms"`
	UploadJitterMs    float64 `json:"upload_jitter_ms"`
	PacketLoss        float64 `json:"packet_loss"`
	BufferbloatGrade  string  `json:"bufferbloat_grade"`
	ClientIP          string  `json:"client_ip"`
	UserAgent         string  `json:"user_agent"`
	SettingsJSON      string  `json:"settings_json"`
}

// Store is the minimal persistence interface used by the results handlers.
// All implementations must be safe for concurrent use.
type Store interface {
	// Save inserts r and returns the assigned id and CreatedAt (unix ms).
	// If r.CreatedAt is zero, implementations set it to time.Now().UnixMilli().
	Save(ctx context.Context, r Result) (id int64, createdAt int64, err error)

	// List returns the most-recent results, newest first.
	List(ctx context.Context, limit, offset int) ([]Result, error)

	// Range returns results with CreatedAt in [fromUnixMs, toUnixMs], oldest first.
	Range(ctx context.Context, fromUnixMs, toUnixMs int64) ([]Result, error)

	// Count returns the total number of stored results.
	Count(ctx context.Context) (int, error)

	// Delete removes a single result by id; returns false if it didn't exist.
	Delete(ctx context.Context, id int64) (bool, error)

	// DeleteAll wipes the table. Returns rows affected.
	DeleteAll(ctx context.Context) (int64, error)

	// PruneOlderThan deletes results with CreatedAt < cutoffUnixMs. Returns rows affected.
	PruneOlderThan(ctx context.Context, cutoffUnixMs int64) (int64, error)

	// Close releases the underlying database handle.
	Close() error
}

// ErrNotFound is returned by single-row reads when no row matches.
// (List/Range return empty slices instead.)
var ErrNotFound = errors.New("store: result not found")
