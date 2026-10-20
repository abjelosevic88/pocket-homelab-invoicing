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

// TestPocketDocumentsMigration fills a database the way v0.6 left it (a client
// linked to a Paperless correspondent, Paperless preferences in the person's
// settings), applies 0013 and checks what carries over: the correspondent by
// name, the archiving preferences, but never the Paperless URL or token.
func TestPocketDocumentsMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v06.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	old := &Store{DB: db, log: log}
	if err := old.migrateBefore("0013"); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`INSERT INTO users (id, email, password_hash, role) VALUES (1, 'owner@example.com', 'hash', 'admin'), (2, 'member@example.com', 'hash', 'member')`)
	mustExec(`INSERT INTO clients (id, user_id, name, paperless_correspondent_id, paperless_correspondent) VALUES (1, 1, 'Athena Studio S.à r.l.', 12, 'Athena Studio'), (2, 1, 'Acme', 0, '')`)
	ownerSettings, _ := json.Marshal(map[string]any{"company_name": "Owner Co", "paperless_url": "http://paperless:8000", "paperless_token": "secret",
		"paperless_mode": "direct", "paperless_tags": "invoicing, 2026", "paperless_invoice_type": "Faktura", "paperless_archive_invoices": "sent",
		"paperless_create_correspondents": false, "paperless_category_as_type": false})
	mustExec(`INSERT INTO user_settings (user_id, value) VALUES (1, ?), (2, '{"company_name": "Member"}')`, string(ownerSettings))
	db.Close()

	s, err := Open(path, log) // applies 0013
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := s.GetClient(ctx, 1, 1)
	if err != nil || c.DocumentsCorrespondent != "Athena Studio" {
		t.Fatalf("correspondent should carry over by name: %v %+v", err, c)
	}
	if c, _ := s.GetClient(ctx, 1, 2); c.DocumentsCorrespondent != "" {
		t.Fatalf("unlinked client got a correspondent: %q", c.DocumentsCorrespondent)
	}
	st, err := s.GetSettings(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st.DocumentsTags != "invoicing, 2026" || st.DocumentsInvoiceType != "Faktura" || st.DocumentsArchiveInvoices != "sent" || st.DocumentsCreateCorrespondents || st.DocumentsCategoryAsType {
		t.Fatalf("preferences should carry over: %+v", st)
	}
	if st.DocumentsURL != "" || st.DocumentsToken != "" || st.DocumentsMode != "" {
		t.Fatalf("the Paperless connection must not carry over: url=%q token=%q mode=%q", st.DocumentsURL, st.DocumentsToken, st.DocumentsMode)
	}
	d := DefaultSettings()
	if st, _ := s.GetSettings(ctx, 2); st.DocumentsTags != d.DocumentsTags || st.DocumentsInvoiceType != d.DocumentsInvoiceType || !st.DocumentsCreateCorrespondents || !st.DocumentsCategoryAsType || st.DocumentsArchiveInvoices != "off" {
		t.Fatalf("defaults for someone without Paperless settings: %+v", st)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(1) FROM documents_links`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("documents_links: %v %d", err, n)
	}
}
