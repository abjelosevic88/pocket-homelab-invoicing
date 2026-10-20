package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestPerUserMigrationBackfill runs migrations up to 0010 on a fresh file, fills it the way a
// single-owner install looked, then applies 0011 and checks that everything belongs to the owner.
func TestPerUserMigrationBackfill(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	legacy := &Store{DB: db, log: log}
	if err := legacy.migrateBefore("0011"); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`INSERT INTO users (id, email, password_hash, role) VALUES (1, 'owner@example.com', 'hash', 'admin')`)
	mustExec(`INSERT INTO users (id, email, password_hash, role) VALUES (2, 'core-auto@example.com', '', 'admin')`)        // auto-created by Core sign-in
	mustExec(`INSERT INTO users (id, email, password_hash, role) VALUES (3, 'second-admin@example.com', 'hash', 'admin')`) // made admin on purpose
	mustExec(`INSERT INTO clients (id, name) VALUES (1, 'Acme')`)
	mustExec(`INSERT INTO invoices (id, number, client_id, issue_date, due_date, currency, public_token) VALUES (1, 'INV-1', 1, '2026-01-01', '2026-01-15', 'EUR', 'tok')`)
	mustExec(`INSERT INTO invoice_items (invoice_id, description) VALUES (1, 'work')`)
	mustExec(`INSERT INTO payments (invoice_id, date, amount, currency, applied_amount) VALUES (1, '2026-01-10', 10, 'EUR', 10)`)
	mustExec(`INSERT INTO products (name) VALUES ('Consulting')`)
	mustExec(`INSERT INTO tax_rates (name, rate) VALUES ('VAT', 17)`)
	mustExec(`INSERT INTO invoice_templates (name) VALUES ('Classic')`)
	mustExec(`INSERT INTO expenses (date, amount, currency) VALUES ('2026-01-02', 5, 'EUR')`)
	mustExec(`INSERT INTO time_entries (client_id, started_at) VALUES (1, '2026-01-02T09:00:00Z')`)
	mustExec(`INSERT INTO recurring_invoices (name, client_id, start_date, next_run, currency) VALUES ('Monthly', 1, '2026-01-01', '2026-02-01', 'EUR')`)
	mustExec(`INSERT INTO webhooks (url) VALUES ('http://example.invalid')`)
	mustExec(`INSERT INTO activity_log (entity_type, entity_id, action) VALUES ('invoice', 1, 'created')`)
	mustExec(`INSERT INTO email_log (invoice_id, to_address, subject, status) VALUES (1, 'a@b', 's', 'sent')`)
	mustExec(`INSERT INTO attachments (invoice_id, filename, stored_name) VALUES (1, 'a.pdf', 'x.pdf')`)
	mustExec(`INSERT INTO documents (title, filename, stored_name) VALUES ('Contract', 'c.pdf', 'c.pdf')`)
	mustExec(`INSERT INTO paperless_links (kind, ref_id, paperless_id) VALUES ('invoice', 1, 9)`)
	app, _ := json.Marshal(map[string]any{"company_name": "Owner Co", "smtp_password": "secret", "base_currency": "BAM", "exchange_rate_provider": "cbbh", "setup_complete": true, "invoice_next_seq": 7})
	mustExec(`INSERT INTO settings (key, value) VALUES ('app', ?)`, string(app))
	db.Close()

	s, err := Open(path, log) // applies 0011
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tbl := range ContentTables {
		if tbl == "documents_links" {
			continue // 0013, empty right after the move
		}
		var total, owned int
		if err := s.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(user_id = 1), 0) FROM `+tbl).Scan(&total, &owned); err != nil {
			t.Fatal(err)
		}
		if total == 0 || total != owned {
			t.Errorf("%s: %d rows, %d owned by the owner", tbl, total, owned)
		}
	}
	st, err := s.GetSettings(ctx, 1)
	if err != nil || st.CompanyName != "Owner Co" || st.SMTPPassword != "secret" || st.InvoiceNextSeq != 7 || st.BaseCurrency != "BAM" || !st.SetupComplete {
		t.Fatalf("owner settings after migration: %v %+v", err, st)
	}
	var raw string
	_ = s.DB.QueryRow(`SELECT value FROM settings WHERE key = 'app'`).Scan(&raw)
	var box map[string]any
	_ = json.Unmarshal([]byte(raw), &box)
	if len(box) != 3 || box["base_currency"] != "BAM" || box["exchange_rate_provider"] != "cbbh" || box["setup_complete"] != true {
		t.Fatalf("global row should only keep box-wide keys: %s", raw)
	}
	if other, _ := s.GetSettings(ctx, 3); other.CompanyName == "Owner Co" || other.SMTPPassword != "" || other.BaseCurrency != "BAM" {
		t.Fatalf("another user must not inherit the owner's settings: %+v", other)
	}
	roles := map[int64]string{}
	users, _ := s.ListUsers(ctx)
	for _, u := range users {
		roles[u.ID] = u.Role
	}
	if roles[1] != "admin" || roles[2] != "member" || roles[3] != "admin" {
		t.Fatalf("roles after migration: %v", roles)
	}
}

// TestStoreScopesByUser checks the store layer directly: another user's ids behave like
// missing rows and lists never mix.
func TestStoreScopesByUser(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := testUser(t, s, "a@example.com")
	b := testUser(t, s, "b@example.com")
	c := &Client{Name: "A's client", Currency: "EUR"}
	if err := s.CreateClient(ctx, a, c); err != nil {
		t.Fatal(err)
	}
	inv := &Invoice{Number: "INV-1", ClientID: c.ID, Status: StatusSent, IssueDate: "2026-01-01", DueDate: "2026-01-15", Currency: "EUR", ExchangeRate: 1, Total: 100}
	if err := s.CreateInvoice(ctx, a, inv); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetClient(ctx, b, c.ID); err != ErrNotFound {
		t.Fatalf("B reads A's client: %v", err)
	}
	if _, err := s.GetInvoice(ctx, b, inv.ID); err != ErrNotFound {
		t.Fatalf("B reads A's invoice: %v", err)
	}
	if list, n, _ := s.ListInvoices(ctx, b, InvoiceFilter{Search: "INV"}); n != 0 || len(list) != 0 {
		t.Fatalf("B lists A's invoices: %d", n)
	}
	if err := s.UpdateClient(ctx, b, c); err != ErrNotFound {
		t.Fatalf("B updates A's client: %v", err)
	}
	if err := s.UpdateInvoiceStatus(ctx, b, inv.ID, StatusPaid, nil, nil, nil); err != ErrNotFound {
		t.Fatalf("B changes A's invoice status: %v", err)
	}
	if err := s.DeleteInvoice(ctx, b, inv.ID); err != ErrNotFound {
		t.Fatalf("B deletes A's invoice: %v", err)
	}
	if err := s.CreatePayment(ctx, b, &Payment{InvoiceID: inv.ID, Date: "2026-01-02", Amount: 1, Currency: "EUR", AppliedAmount: 1}); err != ErrNotFound {
		t.Fatalf("B pays A's invoice: %v", err)
	}
	if err := s.SaveDocsLink(ctx, b, &DocsLink{Kind: "invoice", RefID: inv.ID, DocumentsID: 1}); err != ErrNotFound {
		t.Fatalf("B links A's invoice to Pocket Documents: %v", err)
	}
	if d, _ := s.Dashboard(ctx, b); d.Outstanding != 0 || d.ActiveClients != 0 {
		t.Fatalf("B's dashboard shows A's numbers: %+v", d)
	}
	if exists, _ := s.NumberExists(ctx, b, "INV-1", 0); exists {
		t.Fatal("numbering must be per user")
	}
	// B's own client may reuse A's number: sequences are per person.
	bc := &Client{Name: "B's client", Currency: "EUR"}
	_ = s.CreateClient(ctx, b, bc)
	if err := s.CreateInvoice(ctx, b, &Invoice{Number: "INV-1", ClientID: bc.ID, Status: StatusDraft, IssueDate: "2026-01-01", DueDate: "2026-01-01", Currency: "EUR", ExchangeRate: 1}); err != nil {
		t.Fatalf("B reusing a number A uses: %v", err)
	}
	if n, _ := s.OwnedRecords(ctx, a); n != 2 {
		t.Fatalf("owned records of A: %d", n)
	}
}
