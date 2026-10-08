package docx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// The shipped sample templates must fill cleanly with a realistic document.
func TestSampleTemplates(t *testing.T) {
	ps, pe := "2026-09-01", "2026-09-30"
	in := pdf.BuildInput{
		Invoice: &store.Invoice{Number: "INV-007-2026", Status: "sent", IssueDate: "2026-10-01", DueDate: "2026-10-01", Currency: "EUR", ExchangeRate: 1.95583, PeriodStart: &ps, PeriodEnd: &pe, DiscountType: "none",
			Subtotal: 8400, Total: 8400, CustomFields: map[string]string{"pfr": "ELJA9MLH-16"},
			Items: []store.InvoiceItem{{Description: "Software Development Services", Unit: "day", Quantity: 21, UnitPrice: 400, LineTotal: 8400}}},
		Client:       &store.Client{Name: "Athena Studio S.à r.l.", Address1: "32, Rue Philippe II", City: "Luxembourg", PostalCode: "2340"},
		Settings:     store.Settings{CompanyName: "„WISESTACK“ Aleksandar Bjelošević s.p.", BaseCurrency: "BAM", NumberFormat: "1.234,56", ShowBaseTotal: true, BaseTotalNote: "Rate {rate}. Total {total_base}.", PaymentDetails: "IBAN: BA39\nSWIFT: UNCRBA22", CustomFields: []store.CustomFieldDef{{Key: "pfr", Label: "PFR broj računa", ShowOnPDF: true}}},
		Currency:     store.Currency{Code: "EUR", Name: "Euro", Symbol: "€", Decimals: 2},
		BaseCurrency: store.Currency{Code: "BAM", Name: "Mark", Symbol: "KM ", Decimals: 2},
		Template:     &store.InvoiceTemplate{Options: store.TemplateOptions{ShowQuantityTotal: true, HideUnit: true}},
	}
	doc := pdf.Build(in)
	for _, name := range []string{"invoice-simple.docx", "invoice-wisestack.docx"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", name))
		if err != nil {
			t.Skipf("sample missing: %v", err)
		}
		ph, err := Placeholders(b)
		if err != nil || len(ph) < 10 {
			t.Fatalf("%s placeholders: %v %d", name, err, len(ph))
		}
		out, err := Fill(b, doc.Data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		left, _ := Placeholders(out)
		if len(left) != 0 {
			t.Fatalf("%s: unresolved placeholders %v", name, left)
		}
	}
}
