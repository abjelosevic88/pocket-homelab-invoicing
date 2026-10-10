package server

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// handleAccountantPackage streams a ZIP with everything an accountant needs for one
// year: every invoice as PDF (generated and/or the uploaded originals), CSVs for
// invoices, payments, expenses and exchange rates, the income-tax estimate and a
// human-readable summary.
//
//	GET /reports/accountant-package.zip?year=2026[&drafts=1][&pdf=generated|uploaded|both]
func (s *Server) handleAccountantPackage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	year := qInt(r, "year", time.Now().Year())
	includeDrafts := r.URL.Query().Get("drafts") == "1"
	pdfMode := firstNonEmpty(r.URL.Query().Get("pdf"), "both")
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	from, to := fmt.Sprintf("%d-01-01", year), fmt.Sprintf("%d-12-31", year)
	list, _, err := s.store.ListInvoices(ctx, store.InvoiceFilter{From: from, To: to})
	if err != nil {
		s.fail(w, err, "invoices")
		return
	}
	// oldest first reads better in CSV and in the folder
	sort.Slice(list, func(i, j int) bool {
		if list[i].IssueDate != list[j].IssueDate {
			return list[i].IssueDate < list[j].IssueDate
		}
		return list[i].ID < list[j].ID
	})
	style := money.StyleFor(st.NumberFormat)
	base := s.store.GetCurrency(ctx, st.BaseCurrency)
	fm := func(cur store.Currency, v float64) string {
		sym, pos := cur.Symbol, "before"
		if strings.HasSuffix(sym, " ") {
			sym, pos = strings.TrimSpace(sym), "after"
		}
		return money.FormatStyle(money.Round(v, cur.Decimals), cur.Decimals, sym, pos, style)
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%d-accountant-package.zip"`, safeFilename(firstNonEmpty(st.BrandName, st.CompanyName, "invoices")), year))
	zw := zip.NewWriter(w)
	defer zw.Close()
	now := time.Now()
	add := func(name string) io.Writer {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return io.Discard
		}
		return fw
	}
	var errs []string

	type lineInfo struct {
		inv  store.Invoice
		note string
	}
	var lines []lineInfo
	byClient := map[string]*struct {
		cur                     string
		count                   int
		invoiced, paid, invBase float64
		paidBase                float64
	}{}
	byCurrency := map[string]*struct {
		count          int
		invoiced, paid float64
		invBase        float64
	}{}

	for _, li := range list {
		if li.Status == store.StatusCancelled || (li.Status == store.StatusDraft && !includeDrafts) {
			continue
		}
		inv, err := s.store.GetInvoice(ctx, li.ID)
		if err != nil {
			errs = append(errs, li.Number+": "+err.Error())
			continue
		}
		doc, tpl, logo, logoType, err := s.buildDocument(ctx, inv, inv.TemplateID)
		if err != nil {
			errs = append(errs, inv.Number+": "+err.Error())
			continue
		}
		stem := safeFilename(inv.Number) + "_" + safeFilename(inv.ClientName)
		if pdfMode == "generated" || pdfMode == "both" || len(inv.Attachments) == 0 {
			if b, err := s.renderDoc(ctx, doc, tpl, logo, logoType); err != nil {
				errs = append(errs, inv.Number+": pdf: "+err.Error())
			} else {
				_, _ = add("invoices/" + stem + ".pdf").Write(b)
			}
		}
		if pdfMode == "uploaded" || pdfMode == "both" {
			for _, a := range inv.Attachments {
				data, err := os.ReadFile(s.attachmentPath(&a))
				if err != nil {
					errs = append(errs, inv.Number+": attachment "+a.Filename+": "+err.Error())
					continue
				}
				_, _ = add("attachments/" + safeFilename(inv.Number) + "/" + safeFilename(a.Filename)).Write(data)
			}
		}
		lines = append(lines, lineInfo{inv: *inv, note: doc.BaseCurrencyNote})
		bc := byClient[inv.ClientName]
		if bc == nil {
			bc = &struct {
				cur                     string
				count                   int
				invoiced, paid, invBase float64
				paidBase                float64
			}{cur: inv.Currency}
			byClient[inv.ClientName] = bc
		}
		bc.count++
		bc.invoiced += inv.Total
		bc.paid += inv.AmountPaid
		bc.invBase += inv.Total * inv.ExchangeRate
		bc.paidBase += inv.AmountPaid * inv.ExchangeRate
		cc := byCurrency[inv.Currency]
		if cc == nil {
			cc = &struct {
				count          int
				invoiced, paid float64
				invBase        float64
			}{}
			byCurrency[inv.Currency] = cc
		}
		cc.count++
		cc.invoiced += inv.Total
		cc.paid += inv.AmountPaid
		cc.invBase += inv.Total * inv.ExchangeRate
	}

	// ---- invoices.csv ----
	cw := csv.NewWriter(add("invoices.csv"))
	head := []string{"number", "client", "status", "issue_date", "due_date", "period_start", "period_end", "currency", "subtotal", "discount", "tax", "total", "paid", "balance", "exchange_rate", "total_" + strings.ToLower(st.BaseCurrency), "paid_at", "billing_mode"}
	for _, f := range st.CustomFields {
		head = append(head, f.Key)
	}
	head = append(head, "exchange_rate_note")
	_ = cw.Write(head)
	for _, l := range lines {
		inv := l.inv
		rec := []string{inv.Number, inv.ClientName, inv.Status, inv.IssueDate, inv.DueDate, deref(inv.PeriodStart), deref(inv.PeriodEnd), inv.Currency, f2s(inv.Subtotal), f2s(inv.DiscountTotal), f2s(inv.TaxTotal), f2s(inv.Total), f2s(inv.AmountPaid), f2s(inv.Balance), strconv.FormatFloat(inv.ExchangeRate, 'f', -1, 64), f2s(money.Round(inv.Total*inv.ExchangeRate, base.Decimals)), deref(inv.PaidAt), inv.BillingMode}
		for _, f := range st.CustomFields {
			rec = append(rec, inv.CustomFields[f.Key])
		}
		rec = append(rec, l.note)
		_ = cw.Write(rec)
	}
	cw.Flush()

	// ---- exchange-rates.csv ----
	cw = csv.NewWriter(add("exchange-rates.csv"))
	_ = cw.Write([]string{"invoice", "issue_date", "currency", "rate_to_" + strings.ToLower(st.BaseCurrency), "total", "total_" + strings.ToLower(st.BaseCurrency), "note"})
	for _, l := range lines {
		inv := l.inv
		_ = cw.Write([]string{inv.Number, inv.IssueDate, inv.Currency, strconv.FormatFloat(inv.ExchangeRate, 'f', -1, 64), f2s(inv.Total), f2s(money.Round(inv.Total*inv.ExchangeRate, base.Decimals)), l.note})
	}
	cw.Flush()

	// ---- payments.csv ----
	payments, _ := s.store.ListPayments(ctx, store.PaymentFilter{From: from, To: to})
	sort.Slice(payments, func(i, j int) bool { return payments[i].Date < payments[j].Date })
	cw = csv.NewWriter(add("payments.csv"))
	_ = cw.Write([]string{"date", "invoice", "client", "amount", "currency", "exchange_rate", "applied_amount", "method", "reference", "notes"})
	var paidBaseTotal float64
	invRate := map[int64]float64{}
	for _, l := range lines {
		invRate[l.inv.ID] = l.inv.ExchangeRate
	}
	for _, p := range payments {
		_ = cw.Write([]string{p.Date, p.InvoiceNumber, p.ClientName, f2s(p.Amount), p.Currency, strconv.FormatFloat(p.ExchangeRate, 'f', -1, 64), f2s(p.AppliedAmount), p.Method, p.Reference, p.Notes})
		if rt, ok := invRate[p.InvoiceID]; ok {
			paidBaseTotal += p.AppliedAmount * rt
		}
	}
	cw.Flush()

	// ---- expenses.csv ----
	expenses, _ := s.store.ListExpenses(ctx, store.ExpenseFilter{From: from, To: to})
	cw = csv.NewWriter(add("expenses.csv"))
	_ = cw.Write([]string{"date", "client", "category", "description", "amount", "currency", "exchange_rate", "amount_" + strings.ToLower(st.BaseCurrency), "billable", "invoice_id"})
	var expBase float64
	for _, e := range expenses {
		inv := ""
		if e.InvoiceID != nil {
			inv = fmt.Sprint(*e.InvoiceID)
		}
		expBase += e.Amount * e.ExchangeRate
		_ = cw.Write([]string{e.Date, e.ClientName, e.Category, e.Description, f2s(e.Amount), e.Currency, strconv.FormatFloat(e.ExchangeRate, 'f', -1, 64), f2s(e.Amount * e.ExchangeRate), fmt.Sprint(e.Billable), inv})
	}
	cw.Flush()

	// ---- income-tax.csv + summary ----
	tax, _ := s.incomeTax(ctx, st, year)
	if tax != nil {
		cw = csv.NewWriter(add("income-tax.csv"))
		_ = cw.Write([]string{"month", "income", "expenses", "taxable", "tax", "contributions", "net"})
		for _, rw := range tax.Rows {
			_ = cw.Write([]string{rw.Month, f2s(rw.Income), f2s(rw.Expenses), f2s(rw.Taxable), f2s(rw.Tax), f2s(rw.Contributions), f2s(rw.Net)})
		}
		_ = cw.Write([]string{fmt.Sprint(year), f2s(tax.Total.Income), f2s(tax.Total.Expenses), f2s(tax.Total.Taxable), f2s(tax.Total.Tax), f2s(tax.Total.Contributions), f2s(tax.Total.Net)})
		cw.Flush()
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s – %d\n\n", st.CompanyName, year)
	fmt.Fprintf(&sb, "Generated %s by Pocket Invoicing. Amounts in %s use the exchange rate locked on each invoice.\n\n", now.Format("2006-01-02 15:04"), st.BaseCurrency)
	if st.TaxID != "" {
		fmt.Fprintf(&sb, "Tax ID: %s  \n", st.TaxID)
	}
	addr := strings.Join(nonEmpty(st.Address1, st.Address2, strings.TrimSpace(st.PostalCode+" "+st.City), st.Country), ", ")
	if addr != "" {
		fmt.Fprintf(&sb, "Address: %s  \n", addr)
	}
	var invBaseTotal float64
	var invCount int
	for _, c := range byCurrency {
		invBaseTotal += c.invBase
		invCount += c.count
	}
	fmt.Fprintf(&sb, "\n## Totals\n\n| | |\n|---|---|\n")
	fmt.Fprintf(&sb, "| Invoices | %d |\n", invCount)
	fmt.Fprintf(&sb, "| Invoiced (%s) | %s |\n", st.BaseCurrency, fm(base, invBaseTotal))
	fmt.Fprintf(&sb, "| Received in %d (%s, by payment date) | %s |\n", year, st.BaseCurrency, fm(base, paidBaseTotal))
	fmt.Fprintf(&sb, "| Expenses (%s) | %s |\n", st.BaseCurrency, fm(base, expBase))
	codes := make([]string, 0, len(byCurrency))
	for c := range byCurrency {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	fmt.Fprintf(&sb, "\n## By currency\n\n| Currency | Invoices | Invoiced | Paid | Invoiced in %s |\n|---|---:|---:|---:|---:|\n", st.BaseCurrency)
	for _, code := range codes {
		c := byCurrency[code]
		cur := s.store.GetCurrency(ctx, code)
		fmt.Fprintf(&sb, "| %s | %d | %s | %s | %s |\n", code, c.count, fm(cur, c.invoiced), fm(cur, c.paid), fm(base, c.invBase))
	}
	names := make([]string, 0, len(byClient))
	for n := range byClient {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(&sb, "\n## By client\n\n| Client | Invoices | Invoiced | Paid | Invoiced in %s | Paid in %s |\n|---|---:|---:|---:|---:|---:|\n", st.BaseCurrency, st.BaseCurrency)
	for _, n := range names {
		c := byClient[n]
		cur := s.store.GetCurrency(ctx, c.cur)
		fmt.Fprintf(&sb, "| %s | %d | %s | %s | %s | %s |\n", n, c.count, fm(cur, c.invoiced), fm(cur, c.paid), fm(base, c.invBase), fm(base, c.paidBase))
	}
	fmt.Fprintf(&sb, "\n## Invoices\n\n| Number | Date | Client | Status | Total | Rate | Total in %s | Paid on |\n|---|---|---|---|---:|---:|---:|---|\n", st.BaseCurrency)
	for _, l := range lines {
		inv := l.inv
		cur := s.store.GetCurrency(ctx, inv.Currency)
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", inv.Number, inv.IssueDate, inv.ClientName, inv.Status, fm(cur, inv.Total), strconv.FormatFloat(inv.ExchangeRate, 'f', -1, 64), fm(base, inv.Total*inv.ExchangeRate), deref(inv.PaidAt))
	}
	if tax != nil {
		label := firstNonEmpty(st.IncomeTaxLabel, "Income tax")
		basis := "gross income received (cash basis)"
		if !st.IncomeTaxByPaymentDate {
			basis = "invoiced amounts (by invoice date)"
		}
		if st.IncomeTaxBasis == "profit" {
			basis += " minus expenses"
		}
		fmt.Fprintf(&sb, "\n## %s estimate %d\n\nRate %s%% on %s. This is an estimate from the app's data; the accountant's figures prevail.\n\n", label, year, trimFloat(st.IncomeTaxRate), basis)
		fmt.Fprintf(&sb, "| Month | Income | Expenses | Taxable | Tax | Contributions | Net |\n|---|---:|---:|---:|---:|---:|---:|\n")
		for _, rw := range tax.Rows {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s | %s |\n", rw.Month, fm(base, rw.Income), fm(base, rw.Expenses), fm(base, rw.Taxable), fm(base, rw.Tax), fm(base, rw.Contributions), fm(base, rw.Net))
		}
		t := tax.Total
		fmt.Fprintf(&sb, "| **%d** | **%s** | **%s** | **%s** | **%s** | **%s** | **%s** |\n", year, fm(base, t.Income), fm(base, t.Expenses), fm(base, t.Taxable), fm(base, t.Tax), fm(base, t.Contributions), fm(base, t.Net))
		if st.IncomeTaxDeduction > 0 || st.IncomeTaxMinYearly > 0 {
			fmt.Fprintf(&sb, "\nYearly deduction %s, minimum yearly tax %s.\n", fm(base, st.IncomeTaxDeduction), fm(base, st.IncomeTaxMinYearly))
		}
	}
	fmt.Fprintf(&sb, "\n## Files\n\n- `invoices/` – one PDF per invoice as rendered by the app\n- `attachments/<number>/` – files uploaded to the invoice (e.g. fiscalised or signed originals)\n- `invoices.csv` – all invoices with totals, exchange rate, %s total, custom fields and the exchange-rate sentence\n- `exchange-rates.csv` – the rate used on every invoice\n- `payments.csv`, `expenses.csv` – money in and out during %d\n- `income-tax.csv` – the monthly estimate above\n", st.BaseCurrency, year)
	if !includeDrafts {
		sb.WriteString("\nDrafts and cancelled invoices are not included.\n")
	}
	if len(errs) > 0 {
		sb.WriteString("\nSome files could not be produced; see errors.txt.\n")
		_, _ = io.WriteString(add("errors.txt"), strings.Join(errs, "\n")+"\n")
	}
	_, _ = io.WriteString(add("SUMMARY.md"), sb.String())
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	if len(*p) > 10 && (*p)[4] == '-' && (*p)[10] == 'T' {
		return (*p)[:10] // timestamps become plain dates in the accountant's files
	}
	return *p
}

func nonEmpty(v ...string) []string {
	out := v[:0]
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
