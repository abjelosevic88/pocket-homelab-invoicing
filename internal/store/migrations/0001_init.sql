CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE COLLATE NOCASE,
  name TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT 'admin',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS api_tokens (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL,
  last_used_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS currencies (
  code TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  symbol TEXT NOT NULL,
  decimals INTEGER NOT NULL DEFAULT 2,
  enabled INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS exchange_rates (
  base TEXT NOT NULL,
  quote TEXT NOT NULL,
  rate REAL NOT NULL,
  source TEXT NOT NULL DEFAULT 'manual',
  fetched_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (base, quote)
);

CREATE TABLE IF NOT EXISTS tax_rates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  rate REAL NOT NULL DEFAULT 0,
  is_default INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS clients (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  contact_name TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  address1 TEXT NOT NULL DEFAULT '',
  address2 TEXT NOT NULL DEFAULT '',
  city TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  postal_code TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT '',
  tax_id TEXT NOT NULL DEFAULT '',
  website TEXT NOT NULL DEFAULT '',
  currency TEXT NOT NULL DEFAULT 'EUR',
  billing_mode TEXT NOT NULL DEFAULT 'hourly',
  default_rate REAL NOT NULL DEFAULT 0,
  payment_terms_days INTEGER NOT NULL DEFAULT 14,
  notes TEXT NOT NULL DEFAULT '',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS products (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  unit TEXT NOT NULL DEFAULT 'hour',
  unit_price REAL NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT '',
  tax_rate REAL NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS invoice_templates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  layout TEXT NOT NULL DEFAULT 'classic',
  accent_color TEXT NOT NULL DEFAULT '#2563eb',
  labels TEXT NOT NULL DEFAULT '{}',
  html TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS invoices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  number TEXT NOT NULL UNIQUE,
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
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_invoices_client ON invoices(client_id);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_issue_date ON invoices(issue_date);

CREATE TABLE IF NOT EXISTS invoice_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  invoice_id INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  description TEXT NOT NULL DEFAULT '',
  unit TEXT NOT NULL DEFAULT 'hour',
  quantity REAL NOT NULL DEFAULT 1,
  unit_price REAL NOT NULL DEFAULT 0,
  tax_rate REAL NOT NULL DEFAULT 0,
  discount REAL NOT NULL DEFAULT 0,
  line_total REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_invoice_items_invoice ON invoice_items(invoice_id);

CREATE TABLE IF NOT EXISTS payments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  invoice_id INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  amount REAL NOT NULL,
  currency TEXT NOT NULL,
  exchange_rate REAL NOT NULL DEFAULT 1,
  applied_amount REAL NOT NULL,
  method TEXT NOT NULL DEFAULT 'bank_transfer',
  reference TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_payments_invoice ON payments(invoice_id);

CREATE TABLE IF NOT EXISTS recurring_invoices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  client_id INTEGER NOT NULL REFERENCES clients(id),
  status TEXT NOT NULL DEFAULT 'active',
  frequency TEXT NOT NULL DEFAULT 'monthly',
  interval INTEGER NOT NULL DEFAULT 1,
  start_date TEXT NOT NULL,
  end_date TEXT,
  next_run TEXT NOT NULL,
  last_run TEXT,
  occurrences INTEGER NOT NULL DEFAULT 0,
  max_occurrences INTEGER NOT NULL DEFAULT 0,
  due_days INTEGER NOT NULL DEFAULT 14,
  currency TEXT NOT NULL,
  billing_mode TEXT NOT NULL DEFAULT 'monthly',
  items TEXT NOT NULL DEFAULT '[]',
  discount_type TEXT NOT NULL DEFAULT 'none',
  discount_value REAL NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  terms TEXT NOT NULL DEFAULT '',
  auto_send INTEGER NOT NULL DEFAULT 0,
  template_id INTEGER REFERENCES invoice_templates(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS time_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id INTEGER NOT NULL REFERENCES clients(id),
  project TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  ended_at TEXT,
  duration_minutes INTEGER NOT NULL DEFAULT 0,
  billable INTEGER NOT NULL DEFAULT 1,
  rate REAL,
  invoice_id INTEGER REFERENCES invoices(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_time_entries_client ON time_entries(client_id);
CREATE INDEX IF NOT EXISTS idx_time_entries_invoice ON time_entries(invoice_id);

CREATE TABLE IF NOT EXISTS expenses (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id INTEGER REFERENCES clients(id) ON DELETE SET NULL,
  date TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  amount REAL NOT NULL,
  currency TEXT NOT NULL,
  exchange_rate REAL NOT NULL DEFAULT 1,
  billable INTEGER NOT NULL DEFAULT 0,
  invoice_id INTEGER REFERENCES invoices(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS webhooks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  url TEXT NOT NULL,
  events TEXT NOT NULL DEFAULT '*',
  secret TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS activity_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  entity_type TEXT NOT NULL,
  entity_id INTEGER NOT NULL,
  action TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_activity_entity ON activity_log(entity_type, entity_id);

CREATE TABLE IF NOT EXISTS email_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  invoice_id INTEGER REFERENCES invoices(id) ON DELETE CASCADE,
  to_address TEXT NOT NULL,
  subject TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
