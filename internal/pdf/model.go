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

// KV is a label/value pair.
type KV struct{ Label, Value string }

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
	BaseCurrencyNote string // sentence about the base-currency conversion
	BaseTotalLabel   string // e.g. "Total in BAM"
	BaseTotal        string // formatted total in base currency ("" when same currency)
	QuantityTotal    string // summed quantity, e.g. "21 days"
	CustomFields     []KV   // user-defined fields shown in the meta block
	SignatureLabel   string // prints a signature line when non-empty
	ShowRate         bool
	Data             map[string]any // flat placeholder data for Word templates (see DataKeys)

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
	"total_in":        "Total in",
	"quantity_total":  "Total",
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
	style := money.StyleFor(st.NumberFormat)
	if cur.Name == "" {
		cur.Name = cur.Code
	}
	m := func(v float64) string { return money.FormatStyle(v, cur.Decimals, cur.Symbol, symPos, style) }

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
		d.ShowRate = !in.Template.Options.HideRate
		d.SignatureLabel = in.Template.Options.SignatureLabel
		if in.Template.Options.HideLogo {
			d.LogoPath, d.LogoDataURL = "", ""
		}
	}
	if pd, ok := st.PaymentDetailsByCurrency[inv.Currency]; ok && strings.TrimSpace(pd) != "" {
		d.PaymentDetails = pd
	}
	for _, def := range st.CustomFields {
		if !def.ShowOnPDF {
			continue
		}
		if v := strings.TrimSpace(inv.CustomFields[def.Key]); v != "" {
			d.CustomFields = append(d.CustomFields, KV{Label: def.Label, Value: v})
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
	var qtySum float64
	qtyUnit := ""
	sameUnit := true
	for _, it := range inv.Items {
		qtySum += it.Quantity
		if qtyUnit == "" {
			qtyUnit = it.Unit
		} else if qtyUnit != it.Unit {
			sameUnit = false
		}
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
	quantityTotal := ""
	if sameUnit && len(inv.Items) > 0 {
		quantityTotal = trimNum(qtySum) + " " + unitLabel(d, qtyUnit, qtySum)
	}
	if in.Template != nil {
		d.ShowUnit = !in.Template.Options.HideUnit
		if in.Template.Options.ShowQuantityTotal {
			d.QuantityTotal = quantityTotal
		}
	}
	if !d.ShowUnit {
		// fold the unit into the quantity column: "21 days"
		for i := range d.Lines {
			d.Lines[i].Quantity = d.Lines[i].Quantity + " " + d.Lines[i].Unit
		}
	}
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
	if inv.Currency != st.BaseCurrency && inv.ExchangeRate > 0 && st.ShowBaseTotal {
		bc := in.BaseCurrency
		bpos := "before"
		if strings.HasSuffix(bc.Symbol, " ") {
			bpos = "after"
			bc.Symbol = strings.TrimSpace(bc.Symbol)
		}
		totalBase := money.FormatStyle(money.Round(inv.Total*inv.ExchangeRate, bc.Decimals), bc.Decimals, bc.Symbol, bpos, style)
		d.BaseTotalLabel = d.Label("total_in") + " " + st.BaseCurrency
		d.BaseTotal = totalBase
		rateStr := strings.Replace(trimNum(inv.ExchangeRate), ".", style.Decimal, 1)
		note := st.BaseTotalNote
		if note == "" {
			note = "Exchange rate 1 {currency} = {rate} {base}. Total in {base}: {total_base}."
		}
		r := strings.NewReplacer("{rate}", rateStr, "{currency}", inv.Currency, "{currency_name}", proseName(cur.Name), "{base}", st.BaseCurrency, "{base_name}", proseName(bc.Name), "{total_base}", totalBase, "{total}", d.Total)
		d.BaseCurrencyNote = r.Replace(note)
	}
	d.Data = buildData(d, in)
	return d
}

// buildData flattens the document into snake_case placeholders for Word templates.
func buildData(d *Document, in BuildInput) map[string]any {
	inv, st, c := in.Invoice, in.Settings, in.Client
	if c == nil {
		c = &store.Client{}
	}
	items := make([]map[string]any, 0, len(d.Lines))
	for i, l := range d.Lines {
		it := inv.Items[i]
		items = append(items, map[string]any{
			"n": fmt.Sprint(i + 1), "description": l.Description, "unit": l.Unit, "quantity": trimNum(it.Quantity), "quantity_unit": trimNum(it.Quantity) + " " + unitLabel(d, it.Unit, it.Quantity),
			"unit_price": l.UnitPrice, "discount": l.Discount, "tax_rate": l.TaxRate, "amount": l.Total,
		})
	}
	taxes := make([]map[string]any, 0, len(d.Taxes))
	for _, t := range d.Taxes {
		taxes = append(taxes, map[string]any{"label": t.Label, "amount": t.Amount})
	}
	customList := make([]map[string]any, 0, len(d.CustomFields))
	custom := map[string]any{}
	for _, kv := range d.CustomFields {
		customList = append(customList, map[string]any{"label": kv.Label, "value": kv.Value})
	}
	for _, def := range st.CustomFields {
		custom[def.Key] = inv.CustomFields[def.Key]
	}
	ps, pe := "", ""
	if inv.PeriodStart != nil {
		ps = fmtDate(st.DateFormat, *inv.PeriodStart)
	}
	if inv.PeriodEnd != nil {
		pe = fmtDate(st.DateFormat, *inv.PeriodEnd)
	}
	return map[string]any{
		"title": d.Title, "number": d.Number, "status": d.Status, "issue_date": d.IssueDate, "due_date": d.DueDate, "period": d.Period, "period_start": ps, "period_end": pe,
		"po_number": d.PONumber, "currency": d.Currency, "currency_name": in.Currency.Name, "currency_symbol": strings.TrimSpace(in.Currency.Symbol), "base_currency": st.BaseCurrency, "billing_mode": d.BillingMode,
		"exchange_rate": strings.Replace(trimNum(inv.ExchangeRate), ".", money.StyleFor(st.NumberFormat).Decimal, 1),
		"company":       map[string]any{"name": st.CompanyName, "email": st.CompanyEmail, "phone": st.CompanyPhone, "website": st.CompanyWebsite, "address1": st.Address1, "address2": st.Address2, "city": st.City, "state": st.State, "postal_code": st.PostalCode, "country": st.Country, "tax_id": st.TaxID, "address": strings.Join(d.From.Lines, ", ")},
		"client":        map[string]any{"name": c.Name, "contact": c.ContactName, "email": c.Email, "phone": c.Phone, "address1": c.Address1, "address2": c.Address2, "city": c.City, "state": c.State, "postal_code": c.PostalCode, "country": c.Country, "tax_id": c.TaxID, "website": c.Website, "address": strings.Join(d.To.Lines, ", ")},
		"items":         items, "taxes": taxes, "custom_fields": customList, "custom": custom,
		"subtotal": d.Subtotal, "discount_label": d.DiscountLabel, "discount": d.Discount, "tax_total": fmtMoney(in, inv.TaxTotal), "total": d.Total, "amount_paid": d.AmountPaid, "balance": d.Balance,
		"quantity_total": quantityTotalFor(d, inv), "total_in_base": d.BaseTotal, "base_total_label": d.BaseTotalLabel, "base_note": d.BaseCurrencyNote,
		"notes": d.Notes, "terms": d.Terms, "footer": d.Footer, "payment_details": d.PaymentDetails, "public_url": d.PublicURL, "paid": inv.Status == "paid",
		"has_discount": d.Discount != "", "has_tax": len(d.Taxes) > 0, "has_payments": d.ShowPaid, "is_foreign_currency": d.BaseTotal != "",
	}
}

// quantityTotalFor always returns the summed quantity when all lines share a unit
// (Word templates decide themselves whether to print it).
func quantityTotalFor(d *Document, inv *store.Invoice) string {
	if len(inv.Items) == 0 {
		return ""
	}
	var sum float64
	unit := inv.Items[0].Unit
	for _, it := range inv.Items {
		if it.Unit != unit {
			return ""
		}
		sum += it.Quantity
	}
	return trimNum(sum) + " " + unitLabel(d, unit, sum)
}

func fmtMoney(in BuildInput, v float64) string {
	cur := in.Currency
	pos := "before"
	if strings.HasSuffix(cur.Symbol, " ") {
		pos = "after"
	}
	return money.FormatStyle(v, cur.Decimals, strings.TrimSpace(cur.Symbol), pos, money.StyleFor(in.Settings.NumberFormat))
}

// DataKeys documents the placeholders available to Word templates.
var DataKeys = []struct{ Key, Description string }{
	{"number", "Invoice number"}, {"issue_date", "Issue date"}, {"due_date", "Due date"}, {"period", "Service period (start – end)"}, {"period_start", "Service period start"}, {"period_end", "Service period end"},
	{"po_number", "PO / reference"}, {"currency", "Currency code, e.g. EUR"}, {"currency_name", "Currency name"}, {"currency_symbol", "Currency symbol"}, {"base_currency", "Base currency code"}, {"exchange_rate", "Rate to base currency"}, {"status", "draft / sent / paid …"},
	{"company.name", "Your company name"}, {"company.address", "Your address on one line"}, {"company.address1 / address2 / city / postal_code / state / country", "Address parts"}, {"company.email / phone / website / tax_id", "Contact details"},
	{"client.name", "Client name"}, {"client.contact", "Contact person"}, {"client.address", "Client address on one line"}, {"client.address1 / address2 / city / postal_code / state / country", "Address parts"}, {"client.email / phone / tax_id / website", "Client contact details"},
	{"#items … /items", "Repeat a table row per line item; inside: n, description, unit, quantity, quantity_unit, unit_price, discount, tax_rate, amount"},
	{"#taxes … /taxes", "Repeat per tax rate; inside: label, amount"}, {"#custom_fields … /custom_fields", "Repeat per custom field; inside: label, value"}, {"custom.<key>", "A custom field by its key, e.g. custom.pfr_broj_racuna"},
	{"subtotal", "Subtotal"}, {"discount", "Document discount amount (with minus)"}, {"discount_label", "Discount label"}, {"tax_total", "Total tax"}, {"total", "Grand total"}, {"amount_paid", "Amount paid"}, {"balance", "Balance due"}, {"quantity_total", "Summed quantity, e.g. 21 days"},
	{"total_in_base", "Total converted to base currency"}, {"base_total_label", "e.g. Total in BAM"}, {"base_note", "The configured base-currency sentence"},
	{"payment_details", "Payment details for this currency"}, {"notes", "Notes"}, {"terms", "Terms"}, {"footer", "Footer"}, {"public_url", "Public link"},
	{"#paid … /paid", "Section shown only when paid (also: has_discount, has_tax, has_payments, is_foreign_currency, po_number, notes …)"}, {"^paid … /paid", "Inverted section: shown when NOT paid"},
}

// proseName lower-cases a currency name for use mid-sentence while keeping
// acronyms: "US Dollar" -> "US dollar", "Euro" -> "euro".
func proseName(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) <= 3 && strings.ToUpper(w) == w {
			continue
		}
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, " ")
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
