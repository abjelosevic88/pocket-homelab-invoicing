// Package pdf renders invoices to PDF and HTML.
//
// Three engines are available:
//   - native:    pure Go (go-pdf/fpdf) with embedded DejaVu fonts. Zero dependencies.
//   - chromium:  shells out to a headless Chromium binary to print the HTML template.
//   - gotenberg: posts the HTML template to a Gotenberg container.
package pdf

import (
	"fmt"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// Party is the issuer or the recipient.
type Party struct {
	Name    string
	Contact string
	Lines   []string // address lines
	Email   string
	Phone   string
	Website string
	TaxID   string
}

// Line is a rendered invoice line.
type Line struct {
	Description string
	Unit        string
	Quantity    string
	UnitPrice   string
	Discount    string
	TaxRate     string
	Total       string
}

// TaxLine is one tax rate subtotal.
type TaxLine struct {
	Label  string
	Amount string
}

// Document is everything a renderer needs, pre-formatted.
type Document struct {
	Title       string
	Number      string
	Status      string
	IssueDate   string
	DueDate     string
	Period      string
	PONumber    string
	Currency    string
	BillingMode string

	From Party
	To   Party

	Lines         []Line
	Subtotal      string
	DiscountLabel string
	Discount      string
	Taxes         []TaxLine
	Total         string
	AmountPaid    string
	Balance       string
	ShowPaid      bool
	ShowTax       bool
	ShowDiscount  bool
	ShowUnit      bool

	Notes            string
	Terms            string
	Footer           string
	PaymentDetails   string
	BaseCurrencyNote string

	LogoPath    string
	LogoDataURL string
	AccentColor string
	Layout      string
	Labels      map[string]string
	PublicURL   string
}

// DefaultLabels are the default printable labels (can be overridden per template for localisation).
var DefaultLabels = map[string]string{
	"invoice":         "INVOICE",
	"invoice_number":  "Invoice #",
	"issue_date":      "Date",
	"due_date":        "Due date",
	"period":          "Service period",
	"po_number":       "PO number",
	"from":            "From",
	"bill_to":         "Bill to",
	"description":     "Description",
	"unit":            "Unit",
	"quantity":        "Qty",
	"unit_price":      "Rate",
	"discount":        "Discount",
	"tax":             "Tax",
	"amount":          "Amount",
	"subtotal":        "Subtotal",
	"total":           "Total",
	"amount_paid":     "Paid",
	"balance_due":     "Balance due",
	"notes":           "Notes",
	"terms":           "Terms",
	"payment_details": "Payment details",
	"tax_id":          "Tax ID",
	"page":            "Page",
	"paid_stamp":      "PAID",
	"hour":            "hour",
	"hours":           "hours",
	"day":             "day",
	"days":            "days",
	"month":           "month",
	"months":          "months",
	"unit_":           "unit",
	"units":           "units",
	"fixed":           "fixed",
}

// Label returns a label with fallback.
func (d *Document) Label(key string) string {
	if v, ok := d.Labels[key]; ok && v != "" {
		return v
	}
	if v, ok := DefaultLabels[key]; ok {
		return v
	}
	return key
}

// BuildInput bundles everything needed to construct a Document.
type BuildInput struct {
	Invoice      *store.Invoice
	Client       *store.Client
	Settings     store.Settings
	Currency     store.Currency
	Template     *store.InvoiceTemplate
	BaseCurrency store.Currency
	PublicURL    string
	LogoDataURL  string
}

func fmtDate(layout, s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	if layout == "" {
		layout = "2006-01-02"
	}
	return t.Format(layout)
}

func trimNum(f float64) string {
	s := fmt.Sprintf("%.4f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// Build creates a Document from domain objects.
func Build(in BuildInput) *Document {
	inv, c, st, cur := in.Invoice, in.Client, in.Settings, in.Currency
	symPos := "before"
	if strings.HasSuffix(cur.Symbol, " ") {
		symPos = "after"
		cur.Symbol = strings.TrimSpace(cur.Symbol)
	}
	m := func(v float64) string { return money.Format(v, cur.Decimals, cur.Symbol, symPos) }

	d := &Document{
		Number:         inv.Number,
		Status:         inv.Status,
		IssueDate:      fmtDate(st.DateFormat, inv.IssueDate),
		DueDate:        fmtDate(st.DateFormat, inv.DueDate),
		PONumber:       inv.PONumber,
		Currency:       inv.Currency,
		BillingMode:    inv.BillingMode,
		Notes:          inv.Notes,
		Terms:          inv.Terms,
		Footer:         inv.Footer,
		PaymentDetails: st.PaymentDetails,
		LogoPath:       st.LogoPath,
		LogoDataURL:    in.LogoDataURL,
		AccentColor:    "#2563eb",
		Layout:         "classic",
		Labels:         map[string]string{},
		PublicURL:      in.PublicURL,
		ShowTax:        st.ShowTaxColumn,
	}
	if in.Template != nil {
		if in.Template.AccentColor != "" {
			d.AccentColor = in.Template.AccentColor
		}
		if in.Template.Layout != "" {
			d.Layout = in.Template.Layout
		}
		for k, v := range in.Template.Labels {
			d.Labels[k] = v
		}
	}
	d.Title = d.Label("invoice")
	if inv.PeriodStart != nil && *inv.PeriodStart != "" {
		d.Period = fmtDate(st.DateFormat, *inv.PeriodStart)
		if inv.PeriodEnd != nil && *inv.PeriodEnd != "" {
			d.Period += " – " + fmtDate(st.DateFormat, *inv.PeriodEnd)
		}
	}

	d.From = Party{Name: st.CompanyName, Email: st.CompanyEmail, Phone: st.CompanyPhone, Website: st.CompanyWebsite, TaxID: st.TaxID}
	d.From.Lines = addressLines(st.Address1, st.Address2, st.City, st.State, st.PostalCode, st.Country)
	if c != nil {
		d.To = Party{Name: c.Name, Contact: c.ContactName, Email: c.Email, Phone: c.Phone, TaxID: c.TaxID}
		d.To.Lines = addressLines(c.Address1, c.Address2, c.City, c.State, c.PostalCode, c.Country)
	}

	anyTax, anyDisc := false, false
	for _, it := range inv.Items {
		if it.TaxRate > 0 {
			anyTax = true
		}
		if it.Discount > 0 {
			anyDisc = true
		}
		d.Lines = append(d.Lines, Line{
			Description: it.Description,
			Unit:        unitLabel(d, it.Unit, it.Quantity),
			Quantity:    trimNum(it.Quantity),
			UnitPrice:   m(it.UnitPrice),
			Discount:    discLabel(it.Discount),
			TaxRate:     pctLabel(it.TaxRate),
			Total:       m(it.LineTotal),
		})
	}
	d.ShowTax = d.ShowTax && anyTax
	d.ShowDiscount = anyDisc
	d.ShowUnit = true
	d.Subtotal = m(inv.Subtotal)
	if inv.DiscountTotal > 0 {
		d.DiscountLabel = d.Label("discount")
		if inv.DiscountType == "percent" {
			d.DiscountLabel += fmt.Sprintf(" (%s%%)", trimNum(inv.DiscountValue))
		}
		d.Discount = "-" + m(inv.DiscountTotal)
	}
	// Tax breakdown
	tot := money.Compute(toLineItems(inv.Items), inv.DiscountType, inv.DiscountValue, cur.Decimals)
	keys := make([]string, 0, len(tot.TaxBreakdown))
	for k := range tot.TaxBreakdown {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		d.Taxes = append(d.Taxes, TaxLine{Label: fmt.Sprintf("%s %s%%", d.Label("tax"), k), Amount: m(tot.TaxBreakdown[k])})
	}
	d.Total = m(inv.Total)
	d.AmountPaid = m(inv.AmountPaid)
	d.Balance = m(inv.Total - inv.AmountPaid)
	d.ShowPaid = inv.AmountPaid > 0
	if inv.Currency != st.BaseCurrency && inv.ExchangeRate != 1 && inv.ExchangeRate > 0 {
		bc := in.BaseCurrency
		bpos := "before"
		if strings.HasSuffix(bc.Symbol, " ") {
			bpos = "after"
			bc.Symbol = strings.TrimSpace(bc.Symbol)
		}
		d.BaseCurrencyNote = fmt.Sprintf("≈ %s (1 %s = %s %s)", money.Format(inv.Total*inv.ExchangeRate, bc.Decimals, bc.Symbol, bpos), inv.Currency, trimNum(inv.ExchangeRate), st.BaseCurrency)
	}
	return d
}

func toLineItems(items []store.InvoiceItem) []money.LineItem {
	out := make([]money.LineItem, len(items))
	for i, it := range items {
		out[i] = money.LineItem{Quantity: it.Quantity, UnitPrice: it.UnitPrice, Discount: it.Discount, TaxRate: it.TaxRate}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func unitLabel(d *Document, unit string, qty float64) string {
	plural := qty != 1
	switch unit {
	case "hour":
		if plural {
			return d.Label("hours")
		}
		return d.Label("hour")
	case "day":
		if plural {
			return d.Label("days")
		}
		return d.Label("day")
	case "month":
		if plural {
			return d.Label("months")
		}
		return d.Label("month")
	case "fixed":
		return d.Label("fixed")
	default:
		if plural {
			return d.Label("units")
		}
		return d.Label("unit_")
	}
}

func discLabel(v float64) string {
	if v <= 0 {
		return ""
	}
	return trimNum(v) + "%"
}

func pctLabel(v float64) string {
	if v <= 0 {
		return "–"
	}
	return trimNum(v) + "%"
}

func addressLines(a1, a2, city, state, postal, country string) []string {
	var out []string
	if a1 != "" {
		out = append(out, a1)
	}
	if a2 != "" {
		out = append(out, a2)
	}
	cityLine := strings.TrimSpace(strings.Join(nonEmpty(postal, city), " "))
	if state != "" {
		if cityLine != "" {
			cityLine += ", "
		}
		cityLine += state
	}
	if cityLine != "" {
		out = append(out, cityLine)
	}
	if country != "" {
		out = append(out, country)
	}
	return out
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
