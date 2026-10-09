-- pragma: no-transaction
-- Invoice numbers become unique per client instead of globally (SQLite cannot drop an
-- inline UNIQUE constraint, so the table is rebuilt; foreign keys are off for this file).
CREATE TABLE invoices_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  number TEXT NOT NULL,
  client_id INTEGER NOT NULL REFERENCES clients(id),
  status TEXT NOT NULL DEFAULT 'draft',
  issue_date TEXT NOT NULL,
  due_date TEXT NOT NULL,
  currency TEXT NOT NULL,
  exchange_rate REAL NOT NULL DEFAULT 1,
  billing_mode TEXT NOT NULL DEFAULT 'hourly',
  period_start TEXT,
  period_end TEXT,
  po_number TEXT NOT NULL DEFAULT '',
  discount_type TEXT NOT NULL DEFAULT 'none',
  discount_value REAL NOT NULL DEFAULT 0,
  subtotal REAL NOT NULL DEFAULT 0,
  discount_total REAL NOT NULL DEFAULT 0,
  tax_total REAL NOT NULL DEFAULT 0,
  total REAL NOT NULL DEFAULT 0,
  amount_paid REAL NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  terms TEXT NOT NULL DEFAULT '',
  footer TEXT NOT NULL DEFAULT '',
  template_id INTEGER REFERENCES invoice_templates(id) ON DELETE SET NULL,
  recurring_id INTEGER,
  public_token TEXT NOT NULL UNIQUE,
  sent_at TEXT,
  viewed_at TEXT,
  paid_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  custom_fields TEXT NOT NULL DEFAULT '{}',
  UNIQUE (client_id, number)
);
INSERT INTO invoices_new (id, number, client_id, status, issue_date, due_date, currency, exchange_rate, billing_mode, period_start, period_end, po_number, discount_type, discount_value, subtotal, discount_total, tax_total, total, amount_paid, notes, terms, footer, template_id, recurring_id, public_token, sent_at, viewed_at, paid_at, created_at, updated_at, custom_fields)
  SELECT id, number, client_id, status, issue_date, due_date, currency, exchange_rate, billing_mode, period_start, period_end, po_number, discount_type, discount_value, subtotal, discount_total, tax_total, total, amount_paid, notes, terms, footer, template_id, recurring_id, public_token, sent_at, viewed_at, paid_at, created_at, updated_at, custom_fields FROM invoices;
DROP TABLE invoices;
ALTER TABLE invoices_new RENAME TO invoices;
CREATE INDEX IF NOT EXISTS idx_invoices_client ON invoices(client_id);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_issue_date ON invoices(issue_date);
CREATE INDEX IF NOT EXISTS idx_invoices_number ON invoices(number);
