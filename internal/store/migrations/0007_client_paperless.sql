ALTER TABLE clients ADD COLUMN paperless_correspondent_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE clients ADD COLUMN paperless_correspondent TEXT NOT NULL DEFAULT '';
