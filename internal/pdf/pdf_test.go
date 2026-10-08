package pdf

import (
	"bytes"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func sample() BuildInput {
	ps, pe := "2026-09-01", "2026-09-30"
	return BuildInput{
		Invoice: &store.Invoice{
			Number: "INV-2026-0001", Status: "sent", IssueDate: "2026-10-01", DueDate: "2026-10-15", Currency: "USD", ExchangeRate: 0.92,
			BillingMode: "hourly", PeriodStart: &ps, PeriodEnd: &pe, DiscountType: "percent", DiscountValue: 5,
			Subtotal: 2299.99, DiscountTotal: 115, TaxTotal: 418, Total: 2602.99, AmountPaid: 500,
			Notes: "Thanks for the project — Ćirilica test: Здраво!", Terms: "Net 14",
			Items: []store.InvoiceItem{
				{Description: "Kubernetes cluster maintenance", Unit: "hour", Quantity: 10, UnitPrice: 85, TaxRate: 20, LineTotal: 850},
				{Description: "On-site day with a very long description that should wrap over multiple lines inside the table cell gracefully", Unit: "day", Quantity: 2.5, UnitPrice: 600, Discount: 10, TaxRate: 20, LineTotal: 1350},
				{Description: "Domain renewal", Unit: "unit", Quantity: 1, UnitPrice: 99.99, LineTotal: 99.99},
			},
		},
		Client:       &store.Client{Name: "Acme Corp", ContactName: "Jane Doe", Email: "jane@acme.test", Address1: "1 Main St", City: "Springfield", PostalCode: "12345", Country: "USA", TaxID: "US123"},
		Settings:     store.Settings{CompanyName: "Homelab Services", CompanyEmail: "me@example.com", BaseCurrency: "EUR", DateFormat: "02 Jan 2006", ShowTaxColumn: true, PaymentDetails: "IBAN: DE00 0000 0000\nBIC: ABCDEF"},
		Currency:     store.Currency{Code: "USD", Symbol: "$", Decimals: 2},
		BaseCurrency: store.Currency{Code: "EUR", Symbol: "€", Decimals: 2},
		Template:     &store.InvoiceTemplate{Layout: "classic", AccentColor: "#0f766e", Labels: map[string]string{}},
	}
}

func TestBuild(t *testing.T) {
	d := Build(sample())
	if d.Total != "$2,602.99" || d.Balance != "$2,102.99" || d.Discount != "-$115.00" {
		t.Fatalf("totals: %s %s %s", d.Total, d.Balance, d.Discount)
	}
	if len(d.Taxes) != 1 || d.Taxes[0].Amount != "$418.00" {
		t.Fatalf("taxes: %+v", d.Taxes)
	}
	if d.Period != "01 Sep 2026 – 30 Sep 2026" {
		t.Fatalf("period: %s", d.Period)
	}
	if d.BaseCurrencyNote == "" {
		t.Fatal("expected base currency note")
	}
}

func TestRenderNativeAllLayouts(t *testing.T) {
	for _, layout := range []string{"classic", "modern", "minimal"} {
		in := sample()
		in.Template.Layout = layout
		d := Build(in)
		out, err := RenderNative(d, nil, "")
		if err != nil {
			t.Fatalf("%s: %v", layout, err)
		}
		if !bytes.HasPrefix(out, []byte("%PDF")) || len(out) < 1000 {
			t.Fatalf("%s: not a pdf", layout)
		}
	}
}

func TestRenderHTML(t *testing.T) {
	d := Build(sample())
	out, err := RenderHTML(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("INV-2026-0001")) || !bytes.Contains(out, []byte("Здраво")) {
		t.Fatal("html missing content")
	}
}
