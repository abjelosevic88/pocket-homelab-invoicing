-- Quotes: an offer sent to a client before the work. The client accepts or
-- declines it from its public link (/q/<token>); accepting starts a draft
-- invoice with the same lines (invoice_id), which the person checks and sends.
-- Quotes live apart from invoices so they never reach revenue, reports,
-- payments, overdue marking or anything else that reads invoices.
--
-- status: draft | sent | viewed | accepted | declined | expired | invoiced
-- revision: bumped each time a quote that was already sent is edited.
CREATE TABLE IF NOT EXISTS quotes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id),
  number TEXT NOT NULL,
  client_id INTEGER NOT NULL REFERENCES clients(id),
  status TEXT NOT NULL DEFAULT 'draft',
  issue_date TEXT NOT NULL,
  valid_until TEXT NOT NULL,
  currency TEXT NOT NULL,
  billing_mode TEXT NOT NULL DEFAULT 'hourly',
  po_number TEXT NOT NULL DEFAULT '',
  discount_type TEXT NOT NULL DEFAULT 'none',
  discount_value REAL NOT NULL DEFAULT 0,
  subtotal REAL NOT NULL DEFAULT 0,
  discount_total REAL NOT NULL DEFAULT 0,
  tax_total REAL NOT NULL DEFAULT 0,
  total REAL NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  terms TEXT NOT NULL DEFAULT '',
  footer TEXT NOT NULL DEFAULT '',
  template_id INTEGER REFERENCES invoice_templates(id) ON DELETE SET NULL,
  public_token TEXT NOT NULL UNIQUE,
  revision INTEGER NOT NULL DEFAULT 1,
  sent_at TEXT,
  viewed_at TEXT,
  accepted_at TEXT,
  accepted_name TEXT NOT NULL DEFAULT '',
  accepted_ip TEXT NOT NULL DEFAULT '',
  declined_at TEXT,
  decline_reason TEXT NOT NULL DEFAULT '',
  invoice_id INTEGER REFERENCES invoices(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_quotes_user ON quotes(user_id, status);
CREATE INDEX IF NOT EXISTS idx_quotes_client ON quotes(client_id);
CREATE INDEX IF NOT EXISTS idx_quotes_invoice ON quotes(invoice_id);

CREATE TABLE IF NOT EXISTS quote_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  quote_id INTEGER NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  description TEXT NOT NULL DEFAULT '',
  unit TEXT NOT NULL DEFAULT 'unit',
  quantity REAL NOT NULL DEFAULT 1,
  unit_price REAL NOT NULL DEFAULT 0,
  tax_rate REAL NOT NULL DEFAULT 0,
  discount REAL NOT NULL DEFAULT 0,
  line_total REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_quote_items_quote ON quote_items(quote_id);
