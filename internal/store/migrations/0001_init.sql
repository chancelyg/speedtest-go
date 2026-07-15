CREATE TABLE IF NOT EXISTS results (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at INTEGER NOT NULL,
  download_mbps REAL NOT NULL DEFAULT 0,
  upload_mbps REAL NOT NULL DEFAULT 0,
  latency_idle_ms REAL NOT NULL DEFAULT 0,
  latency_loaded_ms REAL NOT NULL DEFAULT 0,
  download_jitter_ms REAL NOT NULL DEFAULT 0,
  upload_jitter_ms REAL NOT NULL DEFAULT 0,
  packet_loss REAL NOT NULL DEFAULT 0,
  bufferbloat_grade TEXT NOT NULL DEFAULT '',
  client_ip TEXT NOT NULL DEFAULT '',
  client_ip_location TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  settings_json TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_results_created_at ON results(created_at);
