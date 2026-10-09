-- Company documents: a single place for contracts, registrations, bank letters, certificates …
CREATE TABLE IF NOT EXISTS documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT '',
  client_id INTEGER REFERENCES clients(id) ON DELETE SET NULL,
  doc_date TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  filename TEXT NOT NULL,
  stored_name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT 'application/octet-stream',
  size INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_documents_category ON documents(category);
CREATE INDEX IF NOT EXISTS idx_documents_client ON documents(client_id);

-- Links between local records and documents stored in Paperless-ngx.
-- paperless_id = 0 while the consume task is still pending (task_id set).
CREATE TABLE IF NOT EXISTS paperless_links (
  kind TEXT NOT NULL,              -- document | invoice | attachment
  ref_id INTEGER NOT NULL,
  paperless_id INTEGER NOT NULL DEFAULT 0,
  task_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (kind, ref_id)
);
