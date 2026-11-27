package server

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Invoicing (the
// capabilities value in links.go): one route each, in the shape every Pocket
// app answers in. All three only read, as the request's person and only that
// person's clients, invoices, quotes and payments: nothing is sent, reminded,
// marked paid, cancelled or made here. There is no quick capture: an invoice
// needs a client, lines and a number, which a one-line note cannot give.

func invoiceURL(id int64) string { return "/invoices/" + strconv.FormatInt(id, 10) }
func quoteURL(id int64) string   { return "/quotes/" + strconv.FormatInt(id, 10) }
func clientURL(id int64) string  { return "/clients/" + strconv.FormatInt(id, 10) }

// dots joins the parts of a line that are there: "Acme · 1,200.00 EUR · paid".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// plural writes a count with the right word: "1 invoice", "3 invoices".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// daysApart counts the days from one YYYY-MM-DD to another (0 if either is not a date).
func daysApart(from, to string) int {
	a, err1 := time.Parse("2006-01-02", from)
	b, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// isOpen: sent and not yet paid in full.
func isOpen(status string) bool {
	switch status {
	case store.StatusSent, store.StatusViewed, store.StatusPartial, store.StatusOverdue:
		return true
	}
	return false
}

// invoiceState says where an invoice stands in plain words: "paid",
// "overdue 12 days", "partly paid", "sent".
func invoiceState(inv *store.Invoice, today string) string {
	if isOpen(inv.Status) && inv.DueDate < today {
		n := daysApart(inv.DueDate, today)
		if n == 1 {
			return "overdue 1 day"
		}
		return "overdue " + strconv.Itoa(n) + " days"
	}
	switch inv.Status {
	case store.StatusViewed:
		return "opened by the client"
	case store.StatusPartial:
		return "partly paid"
	}
	return inv.Status
}

// quoteState says where a quote stands in plain words.
func quoteState(q *store.Quote) string {
	switch q.Status {
	case store.QuoteViewed:
		return "opened by the client"
	case store.QuoteInvoiced:
		return "invoiced"
	}
	return q.Status
}

// amounts formats money the way the app's own texts do ("1,200.00 EUR"),
// with the currency's own decimals, looked up once per currency.
type amounts struct {
	s   *store.Store
	r   *http.Request
	dec map[string]int
}

func (s *Server) amounts(r *http.Request) *amounts {
	return &amounts{s: s.store, r: r, dec: map[string]int{}}
}

func (a *amounts) decimals(code string) int {
	d, ok := a.dec[code]
	if !ok {
		d = a.s.GetCurrency(a.r.Context(), code).Decimals
		a.dec[code] = d
	}
	return d
}

func (a *amounts) text(v float64, code string) string {
	return money.FormatCode(money.Round(v, a.decimals(code)), a.decimals(code), code)
}

// minor is v in minor units of the currency (1200.50 EUR → 120050).
func (a *amounts) minor(v float64, code string) int64 {
	return int64(math.Round(v * math.Pow10(a.decimals(code))))
}

// sums adds up amounts per currency, in the order the currencies came up.
type sums struct {
	order []string
	by    map[string]float64
}

func (m *sums) add(cur string, v float64) {
	if m.by == nil {
		m.by = map[string]float64{}
	}
	if _, ok := m.by[cur]; !ok {
		m.order = append(m.order, cur)
	}
	m.by[cur] += v
}

// text writes the sums out: "1,200.00 EUR", "1,200.00 EUR and 300.00 USD".
func (m *sums) text(a *amounts, last string) string {
	parts := make([]string, 0, len(m.order))
	for _, cur := range m.order {
		parts = append(parts, a.text(m.by[cur], cur))
	}
	if n := len(parts); n > 1 {
		return strings.Join(parts[:n-1], ", ") + last + parts[n-1]
	}
	return strings.Join(parts, "")
}

// ---------- search ----------

// searchWords are the words of a query that have a letter or a digit; a
// query without any finds nothing. They keep their case: SQLite's LIKE only
// folds the case of plain ASCII letters, so "Čelik" must stay "Čelik".
func searchWords(q string) []string {
	var out []string
	for _, w := range strings.Fields(q) {
		if strings.IndexFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			out = append(out, w)
		}
	}
	return out
}

// pocketSearch answers Pocket Search: clients whose name has every word
// first, then invoices and quotes whose number, client, PO number, notes or a
// line has every word, the newest first.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	uid := uidOf(r)
	words := searchWords(q)
	if len(words) == 0 {
		return nil, nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	today := store.Today()
	a := s.amounts(r)
	out := []core.SearchItem{}

	// the client list's own query, narrowed by a word SQLite can match in any
	// case (plain ASCII); the names are then checked here, where "čelik"
	// finds "Čelik"
	narrow := ""
	for _, w := range words {
		if strings.IndexFunc(w, func(r rune) bool { return r > unicode.MaxASCII }) < 0 {
			narrow = w
			break
		}
	}
	clients, err := s.store.ListClients(ctx, uid, true, narrow)
	if err != nil {
		return nil, err
	}
	for _, c := range clients {
		name := strings.ToLower(c.Name)
		all := true
		for _, w := range words {
			all = all && strings.Contains(name, strings.ToLower(w))
		}
		if !all {
			continue
		}
		owed, archived := "", ""
		if c.Outstanding > 0.000001 {
			owed = a.text(c.Outstanding, c.Currency) + " outstanding"
		}
		if c.Archived {
			archived = "archived"
		}
		var count string
		if c.InvoiceCount > 0 {
			count = plural(int(c.InvoiceCount), "invoice", "invoices")
		}
		out = append(out, core.SearchItem{ID: "client:" + strconv.FormatInt(c.ID, 10), Title: c.Name, Kind: "client",
			Snippet: dots(c.ContactName, count, owed, archived), URL: clientURL(c.ID)})
	}

	type dated struct {
		item core.SearchItem
		date string
	}
	var docs []dated
	invs, err := s.store.SearchInvoices(ctx, uid, words, limit)
	if err != nil {
		return nil, err
	}
	for i := range invs {
		inv := &invs[i]
		docs = append(docs, dated{core.SearchItem{ID: "invoice:" + strconv.FormatInt(inv.ID, 10), Title: "Invoice " + inv.Number, Kind: "invoice",
			Snippet: dots(inv.ClientName, a.text(inv.Total, inv.Currency), invoiceState(inv, today), fmtDay(st.DateFormat, inv.IssueDate)),
			Date:    inv.IssueDate, URL: invoiceURL(inv.ID)}, inv.IssueDate})
	}
	quotes, err := s.store.SearchQuotes(ctx, uid, words, limit)
	if err != nil {
		return nil, err
	}
	for i := range quotes {
		qt := &quotes[i]
		docs = append(docs, dated{core.SearchItem{ID: "quote:" + strconv.FormatInt(qt.ID, 10), Title: "Quote " + qt.Number, Kind: "quote",
			Snippet: dots(qt.ClientName, a.text(qt.Total, qt.Currency), quoteState(qt), fmtDay(st.DateFormat, qt.IssueDate)),
			Date:    qt.IssueDate, URL: quoteURL(qt.ID)}, qt.IssueDate})
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].date > docs[j].date })
	for _, d := range docs {
		out = append(out, d.item)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// pocketDay answers Pocket Day: on that day, the invoices issued (not
// drafts), the payments received, the invoices that fell due and the quotes
// sent or accepted. Only a payment carries an amount, so a reader that adds
// the day's amounts up gets the money that came in, never what was billed;
// what an invoice or a quote is worth is in its second line. Invoices and
// payments carry a plain date; quotes are placed by the moment they were sent
// or accepted, in the caller's time zone, else the person's own (Settings),
// else the server's.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := uidOf(r)
	st, _ := s.store.GetSettings(ctx, uid)
	if tz == nil {
		tz = time.Local
		if loc, err := time.LoadLocation(st.Timezone); err == nil && st.Timezone != "" {
			tz = loc
		}
	}
	a := s.amounts(r)
	day := &core.Day{Date: date}

	pays, err := s.store.ListPayments(ctx, uid, store.PaymentFilter{From: date, To: date, Limit: 200})
	if err != nil {
		return nil, err
	}
	var received sums
	payers := map[string]bool{}
	for i := len(pays) - 1; i >= 0; i-- { // oldest first
		p := &pays[i]
		received.add(p.Currency, p.Amount)
		payers[p.ClientName] = true
		day.Items = append(day.Items, core.DayItem{ID: "payment:" + strconv.FormatInt(p.ID, 10), Title: "Payment from " + p.ClientName, Kind: "received",
			Sub: dots("Invoice "+p.InvoiceNumber, methodWords(p.Method), p.Reference), Done: true,
			Amount: a.minor(p.Amount, p.Currency), Currency: p.Currency, URL: invoiceURL(p.InvoiceID), Note: strings.TrimSpace(p.Notes)})
	}

	issued, _, err := s.store.ListInvoices(ctx, uid, store.InvoiceFilter{From: date, To: date, Limit: 200})
	if err != nil {
		return nil, err
	}
	var invoiced sums
	sent := 0
	for i := len(issued) - 1; i >= 0; i-- {
		inv := &issued[i]
		if inv.Status == store.StatusDraft || inv.Status == store.StatusCancelled {
			continue
		}
		sent++
		invoiced.add(inv.Currency, inv.Total)
		day.Items = append(day.Items, core.DayItem{ID: "invoice:" + strconv.FormatInt(inv.ID, 10), Title: "Invoice " + inv.Number + " to " + inv.ClientName, Kind: "issued",
			Sub: dots(a.text(inv.Total, inv.Currency), "due "+fmtDay(st.DateFormat, inv.DueDate)), URL: invoiceURL(inv.ID)})
	}

	due, err := s.store.InvoicesDueOn(ctx, uid, date, 200)
	if err != nil {
		return nil, err
	}
	for i := range due {
		inv := &due[i]
		left := "paid"
		if inv.Balance > 0.000001 && inv.Status != store.StatusPaid {
			left = a.text(inv.Balance, inv.Currency) + " to pay"
		}
		day.Items = append(day.Items, core.DayItem{ID: "due:" + strconv.FormatInt(inv.ID, 10), Title: "Invoice " + inv.Number + " due", Kind: "due",
			Sub: dots(inv.ClientName, left), Done: left == "paid", URL: invoiceURL(inv.ID)})
	}

	start, err := time.ParseInLocation("2006-01-02", date, tz)
	if err != nil {
		return nil, err
	}
	const utc = "2006-01-02 15:04:05"
	events, err := s.store.QuoteEventsBetween(ctx, uid, start.UTC().Format(utc), start.AddDate(0, 0, 1).UTC().Format(utc), 200)
	if err != nil {
		return nil, err
	}
	quotesSent, quotesAccepted := 0, 0
	for i := range events {
		e := &events[i]
		it := core.DayItem{ID: "quote-" + e.Event + ":" + strconv.FormatInt(e.ID, 10), URL: quoteURL(e.ID), Kind: "quote " + e.Event}
		if t, err := time.Parse(time.RFC3339Nano, e.At); err == nil {
			it.Time = t.In(tz).Format("15:04")
		}
		if e.Event == "accepted" {
			quotesAccepted++
			it.Title, it.Done = "Quote "+e.Number+" accepted by "+e.ClientName, true
			by := ""
			if e.AcceptedName != "" {
				by = "by " + e.AcceptedName
			}
			it.Sub = dots(a.text(e.Total, e.Currency), by)
		} else {
			quotesSent++
			it.Title = "Quote " + e.Number + " sent to " + e.ClientName
			it.Sub = dots(a.text(e.Total, e.Currency), "valid until "+fmtDay(st.DateFormat, e.ValidUntil))
		}
		day.Items = append(day.Items, it)
	}
	if len(day.Items) == 0 {
		return nil, nil
	}

	var parts []string
	if len(received.order) > 0 {
		from := ""
		if len(payers) == 1 {
			for name := range payers {
				from = " from " + name
			}
		} else {
			from = " from " + plural(len(payers), "client", "clients")
		}
		parts = append(parts, "Received "+received.text(a, " and ")+from)
		day.Metrics = append(day.Metrics, core.Metric{Label: "Received", Value: received.text(a, " + ")})
	}
	if sent > 0 {
		parts = append(parts, plural(sent, "invoice", "invoices")+" sent")
		day.Metrics = append(day.Metrics, core.Metric{Label: "Invoiced", Value: invoiced.text(a, " + ")})
	}
	if len(due) > 0 {
		parts = append(parts, plural(len(due), "invoice", "invoices")+" due")
	}
	if quotesSent > 0 {
		parts = append(parts, plural(quotesSent, "quote", "quotes")+" sent")
	}
	if quotesAccepted > 0 {
		parts = append(parts, plural(quotesAccepted, "quote", "quotes")+" accepted")
	}
	day.Summary = strings.Join(parts, "; ")
	if r := []rune(day.Summary); len(r) > 0 {
		day.Summary = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return day, nil
}

// methodWords names a payment method the way the payment form does.
func methodWords(m string) string {
	switch m {
	case store.PaymentBankTransfer:
		return "bank transfer"
	case "paypal":
		return "PayPal"
	case "stripe":
		return "Stripe"
	case "other", "":
		return ""
	}
	return m
}

// ---------- widget ----------

// dueWords says when an unpaid invoice is due: "12 days late", "1 day
// late", "due today", "due tomorrow", "due in 5 days".
func dueWords(due, today string) string {
	n := daysApart(today, due)
	switch {
	case n < -1:
		return strconv.Itoa(-n) + " days late"
	case n == -1:
		return "1 day late"
	case n == 0:
		return "due today"
	case n == 1:
		return "due tomorrow"
	}
	return "due in " + strconv.Itoa(n) + " days"
}

// widgetDueSoon is how far ahead the widget lists invoices still to be paid.
const widgetDueSoon = 14

// pocketWidget answers Pocket Hub: the unpaid invoices that are late, the
// longest late first, then those due in the next two weeks, and the first
// page's numbers (in the base currency): invoiced and received this month,
// and everything still to be paid. It reads the eight unpaid invoices due
// first and the dashboard's sums, whatever the size of the account.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := uidOf(r)
	stats, err := s.store.Dashboard(ctx, uid)
	if err != nil {
		return nil, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	today := store.Today()
	month := today[:7]
	revenue, err := s.store.RevenueByMonth(ctx, uid, month+"-01", today)
	if err != nil {
		return nil, err
	}
	var invoiced float64
	for _, m := range revenue {
		if m.Month == month {
			invoiced = m.Invoiced
		}
	}
	open, err := s.store.OpenInvoicesByDue(ctx, uid, 8)
	if err != nil {
		return nil, err
	}
	a := s.amounts(r)
	base := st.BaseCurrency
	w := &core.WidgetData{URL: "/"}
	for i := range open {
		inv := &open[i]
		if daysApart(today, inv.DueDate) > widgetDueSoon {
			break
		}
		w.Items = append(w.Items, core.WidgetItem{ID: strconv.FormatInt(inv.ID, 10), Title: "Invoice " + inv.Number,
			Detail: dots(inv.ClientName, dueWords(inv.DueDate, today), a.text(inv.Balance, inv.Currency)), URL: invoiceURL(inv.ID)})
	}
	if stats.OverdueCount > 0 {
		w.URL = "/invoices?status=overdue"
	}
	if invoiced < 0.005 && stats.PaidThisMonth < 0.005 && stats.OutstandingCount == 0 && len(w.Items) == 0 {
		return nil, nil // nothing billed, paid or owed
	}
	w.Metrics = []core.Metric{
		{Label: "Invoiced", Value: a.text(invoiced, base)},
		{Label: "Received", Value: a.text(stats.PaidThisMonth, base)},
		{Label: "Outstanding", Value: a.text(stats.Outstanding, base)},
	}
	var line []string
	if stats.OverdueCount > 0 {
		line = append(line, fmt.Sprintf("%d overdue", stats.OverdueCount))
	}
	if stats.OutstandingCount > 0 {
		line = append(line, a.text(stats.Outstanding, base)+" outstanding")
	}
	w.Line = strings.Join(line, ", ")
	return w, nil
}
