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

func TestInvoiceLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.SeedCurrencies(ctx, "EUR"); err != nil {
		t.Fatal(err)
	}
	c := &Client{Name: "Acme", Currency: "USD", BillingMode: "hourly", PaymentTermsDays: 14}
	if err := s.CreateClient(ctx, c); err != nil {
		t.Fatal(err)
	}
	inv := &Invoice{Number: "INV-1", ClientID: c.ID, Status: StatusSent, IssueDate: "2026-01-01", DueDate: "2026-01-15", Currency: "USD", ExchangeRate: 0.9, Total: 100, Subtotal: 100,
		Items: []InvoiceItem{{Description: "work", Unit: "hour", Quantity: 1, UnitPrice: 100, LineTotal: 100}}}
	if err := s.CreateInvoice(ctx, inv); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetInvoice(ctx, inv.ID)
	if err != nil || len(got.Items) != 1 || got.ClientName != "Acme" || got.PublicToken == "" {
		t.Fatalf("get: %v %+v", err, got)
	}
	// overdue marking
	ids, err := s.MarkOverdue(ctx)
	if err != nil || len(ids) != 1 {
		t.Fatalf("overdue: %v %v", err, ids)
	}
	// partial payment
	if err := s.CreatePayment(ctx, &Payment{InvoiceID: inv.ID, Date: "2026-01-10", Amount: 40, Currency: "USD", ExchangeRate: 1, AppliedAmount: 40}); err != nil {
		t.Fatal(err)
	}
	got, err = s.RecalcAmountPaid(ctx, inv.ID)
	if err != nil || got.Status != StatusOverdue || got.Balance != 60 { // partial but past due
		t.Fatalf("partial: %v %s %v", err, got.Status, got.Balance)
	}
	// full payment in another currency
	if err := s.CreatePayment(ctx, &Payment{InvoiceID: inv.ID, Date: "2026-01-11", Amount: 54, Currency: "EUR", ExchangeRate: 1.1111112, AppliedAmount: 60}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.RecalcAmountPaid(ctx, inv.ID)
	if got.Status != StatusPaid || got.Balance != 0 || got.PaidAt == nil {
		t.Fatalf("paid: %s %v", got.Status, got.Balance)
	}
	// reports in base currency
	stats, err := s.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Outstanding != 0 {
		t.Fatalf("outstanding %v", stats.Outstanding)
	}
	rev, err := s.RevenueByMonth(ctx, "2026-01-01", "2026-12-31")
	if err != nil || len(rev) != 1 || rev[0].Invoiced != 90 || rev[0].Paid != 90 {
		t.Fatalf("revenue: %v %+v", err, rev)
	}
	// client stats
	cl, _ := s.GetClient(ctx, c.ID)
	if cl.TotalBilled != 100 || cl.InvoiceCount != 1 {
		t.Fatalf("client stats %+v", cl)
	}
	// delete client should archive since it has invoices
	deleted, err := s.DeleteClient(ctx, c.ID)
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
	st, _ := s.GetSettings(ctx)
	st.CompanyName = "X"
	if err := s.SaveSettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	st2, _ := s.GetSettings(ctx)
	if st2.IncomeTaxRate != 10 || st2.NumberFormat == "" || st2.EmailAttachmentMode != "generated" || !st2.IncomeTaxByPaymentDate {
		t.Fatalf("defaults missing: %+v", st2)
	}
	if st2.CompanyName != "X" || st2.InvoiceNumberFmt == "" {
		t.Fatalf("%+v", st2)
	}
	tpl := &InvoiceTemplate{Name: "A", Layout: "modern", AccentColor: "#000", Labels: map[string]string{"invoice": "RECHNUNG"}, IsDefault: true}
	if err := s.SaveTemplate(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDefaultTemplate(ctx)
	if d.ID != tpl.ID || d.Labels["invoice"] != "RECHNUNG" {
		t.Fatalf("%+v", d)
	}
}
