-- Pocket Time (docs/worklog.md in pocket-homelab-time): time tracked there for
-- a client is written here through the worklog link, and the shared timer.
--
-- source: who made the entry — 'app' (here: the timer, a manual entry, an
-- import), or the Pocket app that wrote it through the worklog ('time').
-- external_id: that app's id for it ('time:<entry id>'); writing the same id
-- again updates the entry instead of adding a second one.
ALTER TABLE time_entries ADD COLUMN source TEXT NOT NULL DEFAULT 'app';
ALTER TABLE time_entries ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_time_entries_external ON time_entries(user_id, external_id) WHERE external_id <> '';

-- A timer started here runs in Pocket Time; what Pocket Time does not keep
-- (the free-text project, a rate override) waits here until the finished time
-- comes back through the worklog, keyed by the entry id in Pocket Time.
CREATE TABLE IF NOT EXISTS shared_timers (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  time_entry_id INTEGER NOT NULL,
  client_id INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  project TEXT NOT NULL DEFAULT '',
  rate REAL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, time_entry_id)
);
