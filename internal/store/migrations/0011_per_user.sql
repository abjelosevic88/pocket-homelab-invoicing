-- Every person is their own business: clients, invoices, payments, expenses, products, tax rates,
-- templates, documents, webhooks, the activity and email logs, and the company profile / numbering /
-- SMTP / Paperless settings all belong to exactly one user.
--
-- Child rows that are only ever reached through their parent inherit the owner:
--   invoice_items -> invoices (every query joins through the invoice or loads it by invoice id first).
-- Everything else gets its own user_id so queries can filter directly.
--
-- Existing rows belong to the owner: the first admin account (lowest id), falling back to the
-- lowest user id. On a fresh database with no users nothing is backfilled here; the app adopts any
-- ownerless rows for the first account it creates (store.AdoptOrphans).

ALTER TABLE clients ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE products ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE tax_rates ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE invoice_templates ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE invoices ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE payments ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE recurring_invoices ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE time_entries ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE expenses ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE webhooks ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE activity_log ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE email_log ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE attachments ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE documents ADD COLUMN user_id INTEGER REFERENCES users(id);
ALTER TABLE paperless_links ADD COLUMN user_id INTEGER REFERENCES users(id);

-- Personal settings (company profile, logo, numbering sequence, defaults, SMTP, Paperless, income tax).
-- The global settings row 'app' keeps only the box-wide part: base currency, rate provider, setup flag.
CREATE TABLE IF NOT EXISTS user_settings (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TEMP TABLE IF NOT EXISTS _owner AS
  SELECT COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users)) AS id;

UPDATE clients SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE products SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE tax_rates SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE invoice_templates SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE invoices SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE payments SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE recurring_invoices SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE time_entries SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE expenses SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE webhooks SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE activity_log SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE email_log SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE attachments SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE documents SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;
UPDATE paperless_links SET user_id = (SELECT id FROM _owner) WHERE user_id IS NULL;

-- Move the personal part of the settings blob to the owner, then strip it from the global row.
INSERT OR IGNORE INTO user_settings (user_id, value)
  SELECT (SELECT id FROM _owner), value FROM settings WHERE key = 'app' AND (SELECT id FROM _owner) IS NOT NULL;
UPDATE settings SET value = json_object(
    'base_currency', json_extract(value, '$.base_currency'),
    'exchange_rate_provider', COALESCE(json_extract(value, '$.exchange_rate_provider'), ''),
    'setup_complete', json(CASE WHEN json_extract(value, '$.setup_complete') THEN 'true' ELSE 'false' END)
  ), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
  WHERE key = 'app' AND (SELECT id FROM _owner) IS NOT NULL;

-- Accounts the app auto-created on first contact (Core / OIDC sign-in, Core device tokens,
-- proxy or no-auth mode) used to become admins. They have no local password; anyone other
-- than the owner who is admin only because of that becomes a member. Admins created on
-- purpose (with a password, via Settings → Users) keep their role.
UPDATE users SET role = 'member'
  WHERE role = 'admin' AND password_hash = '' AND id != COALESCE((SELECT id FROM _owner), 0);

DROP TABLE IF EXISTS _owner;

CREATE INDEX IF NOT EXISTS idx_clients_user ON clients(user_id);
CREATE INDEX IF NOT EXISTS idx_products_user ON products(user_id);
CREATE INDEX IF NOT EXISTS idx_tax_rates_user ON tax_rates(user_id);
CREATE INDEX IF NOT EXISTS idx_invoice_templates_user ON invoice_templates(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_user ON invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_user_number ON invoices(user_id, number);
CREATE INDEX IF NOT EXISTS idx_payments_user ON payments(user_id);
CREATE INDEX IF NOT EXISTS idx_recurring_user ON recurring_invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_time_entries_user ON time_entries(user_id);
CREATE INDEX IF NOT EXISTS idx_expenses_user ON expenses(user_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_user ON webhooks(user_id);
CREATE INDEX IF NOT EXISTS idx_activity_user ON activity_log(user_id);
CREATE INDEX IF NOT EXISTS idx_email_log_user ON email_log(user_id);
CREATE INDEX IF NOT EXISTS idx_attachments_user ON attachments(user_id);
CREATE INDEX IF NOT EXISTS idx_documents_user ON documents(user_id);
CREATE INDEX IF NOT EXISTS idx_paperless_links_user ON paperless_links(user_id);
