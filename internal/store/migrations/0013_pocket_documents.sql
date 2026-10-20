-- Pocket Documents replaces Paperless-ngx.
--
-- paperless_links and clients.paperless_correspondent(_id) stay as they are,
-- for history; nothing reads them any more except the one-off adoption of old
-- Paperless links (a document imported from Paperless into Pocket Documents
-- carries the external id "paperless:<id>").

-- Local records filed in Pocket Documents. Uploads answer at once with the
-- document, so there is no pending state: documents_id > 0 is filed,
-- documents_id = 0 with an error is a failed attempt.
CREATE TABLE IF NOT EXISTS documents_links (
  user_id INTEGER REFERENCES users(id),
  kind TEXT NOT NULL,                       -- document | invoice | attachment
  ref_id INTEGER NOT NULL,
  documents_id INTEGER NOT NULL DEFAULT 0,  -- the document's id in Pocket Documents
  title TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (kind, ref_id)
);
CREATE INDEX IF NOT EXISTS idx_documents_links_user ON documents_links(user_id);
CREATE INDEX IF NOT EXISTS idx_documents_links_doc ON documents_links(user_id, documents_id);

-- A client's correspondent in Pocket Documents, by NAME: ids there differ
-- from Paperless (its import recreated correspondents by name) and between
-- instances, so the id is looked up by name when it is needed.
ALTER TABLE clients ADD COLUMN documents_correspondent TEXT NOT NULL DEFAULT '';
UPDATE clients SET documents_correspondent = TRIM(paperless_correspondent)
  WHERE TRIM(paperless_correspondent) <> '';

-- The archiving preferences carry over from the Paperless settings (not the
-- URL, token or mode: Pocket Documents is a new connection).
UPDATE user_settings SET value = json_set(value, '$.documents_tags', json_extract(value, '$.paperless_tags'))
  WHERE json_type(value, '$.paperless_tags') = 'text' AND json_type(value, '$.documents_tags') IS NULL;
UPDATE user_settings SET value = json_set(value, '$.documents_invoice_type', json_extract(value, '$.paperless_invoice_type'))
  WHERE json_type(value, '$.paperless_invoice_type') = 'text' AND json_type(value, '$.documents_invoice_type') IS NULL;
UPDATE user_settings SET value = json_set(value, '$.documents_archive_invoices', json_extract(value, '$.paperless_archive_invoices'))
  WHERE json_type(value, '$.paperless_archive_invoices') = 'text' AND json_type(value, '$.documents_archive_invoices') IS NULL;
UPDATE user_settings SET value = json_set(value, '$.documents_create_correspondents', json(json_type(value, '$.paperless_create_correspondents')))
  WHERE json_type(value, '$.paperless_create_correspondents') IN ('true', 'false') AND json_type(value, '$.documents_create_correspondents') IS NULL;
UPDATE user_settings SET value = json_set(value, '$.documents_category_as_type', json(json_type(value, '$.paperless_category_as_type')))
  WHERE json_type(value, '$.paperless_category_as_type') IN ('true', 'false') AND json_type(value, '$.documents_category_as_type') IS NULL;
