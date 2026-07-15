package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	// Pure-Go SQLite driver (modernc.org/sqlite). No CGO required, so the
	// resulting binary still cross-compiles trivially. The driver registers
	// itself under the name "sqlite" — NOT "sqlite3" — when imported.
	_ "modernc.org/sqlite"
)

//go:embed migrations/0001_init.sql
var schemaInit string

// Phase 5: opt-in geoip feature adds a client_ip_location column. Applied
// only when the column is absent — see ensureColumn in Open().
//
//go:embed migrations/0002_geoip_column.sql
var migrationGeoIPColumn string

// SQLite is the SQLite-backed implementation of Store.
//
// It uses WAL journaling for concurrent readers while a writer is active and a
// 5-second busy_timeout so transient lock contention surfaces as a retry-able
// wait rather than an immediate SQLITE_BUSY error. The driver name is "sqlite"
// (modernc.org/sqlite); using "sqlite3" silently fails to find a driver.
type SQLite struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path, applies the schema, and
// configures pragmas appropriate for an embedded single-writer workload.
//
// path is passed verbatim to the driver; ":memory:" works for tests.
func Open(path string) (*SQLite, error) {
	// _pragma= query parameters set pragmas at connection open time, which
	// is required for journal_mode=WAL to persist across pooled connections.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite at %q: %w", path, err)
	}

	// SQLite is a single-writer engine. Capping the pool at one connection
	// avoids contention on the write lock — `database/sql` would otherwise
	// open arbitrarily many connections under load, each blocking on the
	// busy_timeout. One connection + WAL still allows reads to make progress.
	db.SetMaxOpenConns(1)

	// modernc.org/sqlite returns errors lazily; ping forces a real connection
	// so misconfiguration (e.g. unwritable directory) fails fast at startup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping sqlite: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaInit); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}

	// Additive column migrations. Each ensureColumn call is a no-op on a
	// freshly-created database (the CREATE TABLE above already includes the
	// column) AND on a database that was previously upgraded (the column is
	// already present). SQLite lacks `ADD COLUMN IF NOT EXISTS`, so we
	// check via PRAGMA table_info before executing the ALTER.
	//
	// Note: the future story for a proper migrations table lives in
	// docs/deployment.md; for two additive migrations this guard is
	// simpler and won't corrupt schema_migrations tracking.
	if err := ensureColumn(ctx, db, "results", "client_ip_location", migrationGeoIPColumn); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: apply 0002 geoip column: %w", err)
	}

	return &SQLite{db: db}, nil
}

// ensureColumn adds a column to a table by running migrationSQL, but only
// when the column is not yet present. Idempotent — safe to call on every
// Open regardless of database age. columnName must not contain a quote;
// callers pass compile-time constants so injection is not a concern.
func ensureColumn(ctx context.Context, db *sql.DB, table, column, migrationSQL string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, colType    string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info(%s): %w", table, err)
		}
		if name == column {
			return nil // already present — nothing to do
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info(%s): %w", table, err)
	}
	if _, err := db.ExecContext(ctx, migrationSQL); err != nil {
		return fmt.Errorf("exec migration for %s.%s: %w", table, column, err)
	}
	return nil
}

// Close closes the underlying *sql.DB. Safe to call multiple times.
func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Save inserts r and returns it with ID and CreatedAt populated.
func (s *SQLite) Save(ctx context.Context, r Result) (Result, error) {
	if r.CreatedAt == 0 {
		r.CreatedAt = time.Now().UnixMilli()
	}
	const q = `
INSERT INTO results (
  created_at, download_mbps, upload_mbps,
  latency_idle_ms, latency_loaded_ms,
  download_jitter_ms, upload_jitter_ms,
  packet_loss, bufferbloat_grade,
  client_ip, client_ip_location, user_agent, settings_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	res, err := s.db.ExecContext(ctx, q,
		r.CreatedAt, r.DownloadMbps, r.UploadMbps,
		r.LatencyIdleMs, r.LatencyLoadedMs,
		r.DownloadJitterMs, r.UploadJitterMs,
		r.PacketLoss, r.BufferbloatGrade,
		r.ClientIP, r.ClientIPLocation, r.UserAgent, r.SettingsJSON,
	)
	if err != nil {
		return Result{}, fmt.Errorf("store: save: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Result{}, fmt.Errorf("store: last insert id: %w", err)
	}
	r.ID = id
	return r, nil
}

// List returns up to limit rows starting at offset, newest first.
func (s *SQLite) List(ctx context.Context, limit, offset int) ([]Result, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("store: list: limit must be > 0, got %d", limit)
	}
	if offset < 0 {
		offset = 0
	}
	const q = `
SELECT id, created_at, download_mbps, upload_mbps,
       latency_idle_ms, latency_loaded_ms,
       download_jitter_ms, upload_jitter_ms,
       packet_loss, bufferbloat_grade,
       client_ip, client_ip_location, user_agent, settings_json
FROM results
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?
`
	rows, err := s.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// Count returns the total row count.
func (s *SQLite) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM results`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count: %w", err)
	}
	return n, nil
}

// Delete removes a single row by id.
func (s *SQLite) Delete(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM results WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("store: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: rows affected: %w", err)
	}
	return n > 0, nil
}

// DeleteAll wipes the results table.
func (s *SQLite) DeleteAll(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM results`)
	if err != nil {
		return 0, fmt.Errorf("store: delete all: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// PruneOlderThan removes rows older than cutoffMs.
func (s *SQLite) PruneOlderThan(ctx context.Context, cutoffMs int64) (int64, error) {
	if cutoffMs <= 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM results WHERE created_at < ?`, cutoffMs)
	if err != nil {
		return 0, fmt.Errorf("store: prune: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// scanRows is a shared row-scanning helper used by List and Range.
func scanRows(rows *sql.Rows) ([]Result, error) {
	out := make([]Result, 0, 16)
	for rows.Next() {
		var r Result
		if err := rows.Scan(
			&r.ID, &r.CreatedAt, &r.DownloadMbps, &r.UploadMbps,
			&r.LatencyIdleMs, &r.LatencyLoadedMs,
			&r.DownloadJitterMs, &r.UploadJitterMs,
			&r.PacketLoss, &r.BufferbloatGrade,
			&r.ClientIP, &r.ClientIPLocation, &r.UserAgent, &r.SettingsJSON,
		); err != nil {
			return nil, fmt.Errorf("store: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: rows: %w", err)
	}
	return out, nil
}

// compile-time interface assertion
var _ Store = (*SQLite)(nil)
