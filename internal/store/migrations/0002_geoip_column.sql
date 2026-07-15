-- Phase 5: add client_ip_location for the opt-in geoip enrichment feature.
--
-- SQLite has no `ADD COLUMN IF NOT EXISTS`, so this file is applied only
-- when store_sqlite.go's ensureColumn helper has confirmed the column is
-- absent (via PRAGMA table_info). Running it against an already-migrated
-- database would raise `duplicate column name`; the guard is the caller's
-- responsibility, not this SQL's.
ALTER TABLE results ADD COLUMN client_ip_location TEXT NOT NULL DEFAULT '';
