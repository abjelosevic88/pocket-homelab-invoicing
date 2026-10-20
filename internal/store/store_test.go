package store

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"), slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// testUser creates an account and returns its id.
func testUser(t *testing.T, s *Store, email string) int64 {
	t.Helper()
	u := &User{Email: email, Name: email, Role: "admin", PasswordHash: "x"}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestInvoiceLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := testUser(t, s, "owner@example.com")
	if err := s.SeedCurrencies(ctx, "EUR"); err != nil {
		t.Fatal(err)
	}
	c := &Client{Name: "Acme", Currency: "USD", BillingMode: "hourly", PaymentTermsDays: 14}
	if err := s.CreateClient(ctx, uid, c); err != nil {
		t.Fatal(err)
	}
	inv := &Invoice{Number: "INV-1", ClientID: c.ID, Status: StatusSent, IssueDate: "2026-01-01", DueDate: "2026-01-15", Currency: "USD", ExchangeRate: 0.9, Total: 100, Subtotal: 100,
		Items: []InvoiceItem{{Description: "work", Unit: "hour", Quantity: 1, UnitPrice: 100, LineTotal: 100}}}
	if err := s.CreateInvoice(ctx, uid, inv); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetInvoice(ctx, uid, inv.ID)
	if err != nil || len(got.Items) != 1 || got.ClientName != "Acme" || got.PublicToken == "" {
		t.Fatalf("get: %v %+v", err, got)
	}
	// overdue marking
	ids, err := s.MarkOverdue(ctx, uid)
	if err != nil || len(ids) != 1 {
		t.Fatalf("overdue: %v %v", err, ids)
	}
	// partial payment
	if err := s.CreatePayment(ctx, uid, &Payment{InvoiceID: inv.ID, Date: "2026-01-10", Amount: 40, Currency: "USD", ExchangeRate: 1, AppliedAmount: 40}); err != nil {
		t.Fatal(err)
	}
	got, err = s.RecalcAmountPaid(ctx, uid, inv.ID)
	if err != nil || got.Status != StatusOverdue || got.Balance != 60 { // partial but past due
		t.Fatalf("partial: %v %s %v", err, got.Status, got.Balance)
	}
	// full payment in another currency
	if err := s.CreatePayment(ctx, uid, &Payment{InvoiceID: inv.ID, Date: "2026-01-11", Amount: 54, Currency: "EUR", ExchangeRate: 1.1111112, AppliedAmount: 60}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.RecalcAmountPaid(ctx, uid, inv.ID)
	if got.Status != StatusPaid || got.Balance != 0 || got.PaidAt == nil {
		t.Fatalf("paid: %s %v", got.Status, got.Balance)
	}
	// reports in base currency
	stats, err := s.Dashboard(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Outstanding != 0 {
		t.Fatalf("outstanding %v", stats.Outstanding)
	}
	rev, err := s.RevenueByMonth(ctx, uid, "2026-01-01", "2026-12-31")
	if err != nil || len(rev) != 1 || rev[0].Invoiced != 90 || rev[0].Paid != 90 {
		t.Fatalf("revenue: %v %+v", err, rev)
	}
	// client stats
	cl, _ := s.GetClient(ctx, uid, c.ID)
	if cl.TotalBilled != 100 || cl.InvoiceCount != 1 {
		t.Fatalf("client stats %+v", cl)
	}
	// delete client should archive since it has invoices
	deleted, err := s.DeleteClient(ctx, uid, c.ID)
	if err != nil || deleted {
		t.Fatalf("delete client: %v %v", err, deleted)
	}
}

func TestRates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_ = s.UpsertRate(ctx, ExchangeRate{Base: "EUR", Quote: "USD", Rate: 1.1})
	_ = s.UpsertRate(ctx, ExchangeRate{Base: "EUR", Quote: "GBP", Rate: 0.85})
	if r, ok := s.GetRate(ctx, "EUR", "USD"); !ok || r != 1.1 {
		t.Fatalf("direct %v %v", r, ok)
	}
	if r, ok := s.GetRate(ctx, "USD", "EUR"); !ok || r < 0.909 || r > 0.91 {
		t.Fatalf("inverse %v %v", r, ok)
	}
	if r, ok := s.GetRate(ctx, "USD", "GBP"); !ok || r < 0.772 || r > 0.773 {
		t.Fatalf("cross %v %v", r, ok)
	}
	if _, ok := s.GetRate(ctx, "USD", "JPY"); ok {
		t.Fatal("expected missing")
	}
	// Rows against another base are pruned when the base changes.
	_ = s.UpsertRate(ctx, ExchangeRate{Base: "BAM", Quote: "USD", Rate: 0.57})
	if n, err := s.PruneRates(ctx, "BAM"); err != nil || n != 2 {
		t.Fatalf("PruneRates removed %d rows, err %v; want 2", n, err)
	}
	if left, _ := s.ListRates(ctx, "BAM"); len(left) != 1 || left[0].Quote != "USD" {
		t.Fatalf("BAM rates after prune: %+v", left)
	}
	if _, ok := s.GetRate(ctx, "EUR", "USD"); ok {
		t.Fatalf("EUR→USD should be gone after prune")
	}
}

func TestRoundMinutes(t *testing.T) {
	cases := [][3]int{{0, 15, 15}, {1, 15, 15}, {15, 15, 15}, {16, 15, 30}, {44, 15, 45}, {61, 1, 61}, {0, 1, 1}, {7, 6, 12}}
	for _, c := range cases {
		if got := RoundMinutes(c[0], c[1]); got != c[2] {
			t.Errorf("RoundMinutes(%d,%d)=%d want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestTemplatesAndSettings(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := testUser(t, s, "owner@example.com")
	st, _ := s.GetSettings(ctx, uid)
	st.CompanyName = "X"
	if err := s.SaveSettings(ctx, uid, st); err != nil {
		t.Fatal(err)
	}
	st2, _ := s.GetSettings(ctx, uid)
	if st2.IncomeTaxRate != 10 || st2.NumberFormat == "" || st2.EmailAttachmentMode != "generated" || !st2.IncomeTaxByPaymentDate {
		t.Fatalf("defaults missing: %+v", st2)
	}
	if st2.CompanyName != "X" || st2.InvoiceNumberFmt == "" {
		t.Fatalf("%+v", st2)
	}
	tpl := &InvoiceTemplate{Name: "A", Layout: "modern", AccentColor: "#000", Labels: map[string]string{"invoice": "RECHNUNG"}, IsDefault: true}
	if err := s.SaveTemplate(ctx, uid, tpl); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDefaultTemplate(ctx, uid)
	if d.ID != tpl.ID || d.Labels["invoice"] != "RECHNUNG" {
		t.Fatalf("%+v", d)
	}
}

func TestInvoiceNumberUniquePerClient(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := testUser(t, s, "owner@example.com")
	a := &Client{Name: "A", Currency: "EUR"}
	b := &Client{Name: "B", Currency: "USD"}
	_ = s.CreateClient(ctx, uid, a)
	_ = s.CreateClient(ctx, uid, b)
	mk := func(c *Client) error {
		return s.CreateInvoice(ctx, uid, &Invoice{Number: "INV-001-2026", ClientID: c.ID, Status: StatusDraft, IssueDate: "2026-01-01", DueDate: "2026-01-01", Currency: c.Currency, ExchangeRate: 1})
	}
	if err := mk(a); err != nil {
		t.Fatal(err)
	}
	if err := mk(b); err != nil {
		t.Fatalf("same number for another client should be allowed: %v", err)
	}
	if err := mk(b); err == nil {
		t.Fatal("same number for the same client must be rejected")
	}
	same, others, err := s.NumberUsage(ctx, uid, "INV-001-2026", a.ID, 0)
	if err != nil || !same || len(others) != 1 || others[0] != "B" {
		t.Fatalf("usage: %v %v %v", same, others, err)
	}
	// migration left the FK graph intact
	inv, _, _ := s.ListInvoices(ctx, uid, InvoiceFilter{})
	if len(inv) != 2 {
		t.Fatalf("expected 2 invoices, got %d", len(inv))
	}
}

func TestFollowDefaultText(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := testUser(t, s, "owner@example.com")
	other := testUser(t, s, "other@example.com")
	c := &Client{Name: "A", Currency: "EUR"}
	oc := &Client{Name: "B", Currency: "EUR"}
	_ = s.CreateClient(ctx, uid, c)
	_ = s.CreateClient(ctx, other, oc)
	mk := func(u, cid int64, n, status, notes string) *Invoice {
		inv := &Invoice{Number: n, ClientID: cid, Status: status, IssueDate: "2026-01-01", DueDate: "2026-01-01", Currency: "EUR", ExchangeRate: 1, Notes: notes}
		if err := s.CreateInvoice(ctx, u, inv); err != nil {
			t.Fatal(err)
		}
		return inv
	}
	follows := mk(uid, c.ID, "1", StatusDraft, "Old default")
	own := mk(uid, c.ID, "2", StatusDraft, "My own note")
	sent := mk(uid, c.ID, "3", StatusSent, "Old default")
	foreign := mk(other, oc.ID, "4", StatusDraft, "Old default")

	n, err := s.FollowDefaultText(ctx, uid, "notes", "Old default", "New default")
	if err != nil || n != 1 {
		t.Fatalf("updated %d, %v", n, err)
	}
	want := map[*Invoice]string{follows: "New default", own: "My own note", sent: "Old default"}
	for inv, w := range want {
		got, _ := s.GetInvoice(ctx, uid, inv.ID)
		if got.Notes != w {
			t.Errorf("invoice %s: %q, want %q", inv.Number, got.Notes, w)
		}
	}
	if got, _ := s.GetInvoice(ctx, other, foreign.ID); got.Notes != "Old default" {
		t.Errorf("other user's draft changed: %q", got.Notes)
	}
	if _, err := s.FollowDefaultText(ctx, uid, "items; drop", "a", "b"); err == nil {
		t.Error("unknown column accepted")
	}
}

func TestOverdueClearsWhenDueDateMovesOut(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := testUser(t, s, "owner@example.com")
	c := &Client{Name: "A", Currency: "EUR"}
	_ = s.CreateClient(ctx, uid, c)
	inv := &Invoice{Number: "1", ClientID: c.ID, Status: StatusOverdue, IssueDate: "2026-01-01", DueDate: "2026-01-01", Currency: "EUR", ExchangeRate: 1, Total: 100}
	if err := s.CreateInvoice(ctx, uid, inv); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RecalcAmountPaid(ctx, uid, inv.ID); got.Status != StatusOverdue {
		t.Fatalf("past due: %s", got.Status)
	}
	inv.DueDate = "2999-01-01"
	if err := s.UpdateInvoice(ctx, uid, inv); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.RecalcAmountPaid(ctx, uid, inv.ID); got.Status != StatusSent {
		t.Fatalf("future due: %s", got.Status)
	}
}
