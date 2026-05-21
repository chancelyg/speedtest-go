// Package store defines the abstract interface for persisting speed-test
// results and the data types shared between the storage layer and the HTTP
// handler layer.
//
// The interface is intentionally small (Save / List / Count / Delete /
// DeleteAll / PruneOlderThan / Close) so it can be implemented by a future
// in-memory or remote backend without dragging the rest of the codebase along.
//
// Time semantics: CreatedAt is expressed in Unix milliseconds. Milliseconds
// (rather than seconds) keep the ordering stable when multiple results are
// saved in the same second.
package store

import "context"

// Result is one stored speed-test measurement record.
//
// The handler layer maps the JSON payload it receives directly onto this
// struct, and the storage layer maps it onto SQL columns. Fields are
// intentionally flat — there are no nested objects — so CSV export is trivial.
type Result struct {
	ID                int64   `json:"id"`
	CreatedAt         int64   `json:"created_at"` // Unix milliseconds
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

// Store is the abstract persistence interface for speed-test results.
//
// Implementations must be safe for concurrent use. All methods that take a
// context.Context must honour cancellation.
type Store interface {
	// Save inserts a new result and returns the generated id and created_at
	// timestamp. If r.CreatedAt is 0, the implementation assigns it.
	Save(ctx context.Context, r Result) (Result, error)

	// List returns up to `limit` results, newest first, skipping the first
	// `offset`. limit must be > 0; offset must be >= 0.
	List(ctx context.Context, limit, offset int) ([]Result, error)

	// Count returns the total number of stored results.
	Count(ctx context.Context) (int64, error)

	// Delete removes a single result by id. Returns true if a row was deleted.
	Delete(ctx context.Context, id int64) (bool, error)

	// DeleteAll wipes the entire results table. Returns the number of rows removed.
	DeleteAll(ctx context.Context) (int64, error)

	// PruneOlderThan deletes all rows with createdAt < cutoffMs. Returns the
	// number of rows removed. A cutoff <= 0 is a no-op (returns 0, nil).
	PruneOlderThan(ctx context.Context, cutoffMs int64) (int64, error)

	// Close releases the underlying resources. Safe to call multiple times.
	Close() error
}
