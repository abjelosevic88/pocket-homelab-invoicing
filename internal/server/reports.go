package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := s.store.Dashboard(ctx)
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	from, to := dateRange(r)
	revenue, _ := s.store.RevenueByMonth(ctx, from, to)
	recent, _, _ := s.store.ListInvoices(ctx, store.InvoiceFilter{Limit: 8})
	overdue, _, _ := s.store.ListInvoices(ctx, store.InvoiceFilter{Status: "overdue", Limit: 8})
	running, _ := s.store.RunningTimer(ctx)
	activity, _ := s.store.ListActivity(ctx, "", 0, 10)
	upcoming, _ := s.store.ListRecurring(ctx)
	var next []store.RecurringInvoice
	for _, rr := range upcoming {
		if rr.Status == "active" {
			next = append(next, rr)
			if len(next) >= 5 {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats": stats, "base_currency": s.store.GetCurrency(ctx, st.BaseCurrency), "revenue": revenue,
		"recent": recent, "overdue": overdue, "running_timer": running, "activity": activity, "upcoming_recurring": next,
	})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListActivity(r.Context(), r.URL.Query().Get("entity_type"), qInt64(r, "entity_id"), qInt(r, "limit", 50))
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleReportRevenue(w http.ResponseWriter, r *http.Request) {
	from, to := dateRange(r)
	list, err := s.store.RevenueByMonth(r.Context(), from, to)
	if err != nil {
		s.fail(w, err, "revenue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "months": list})
}

func (s *Server) handleReportClients(w http.ResponseWriter, r *http.Request) {
	from, to := dateRange(r)
	list, err := s.store.RevenueByClient(r.Context(), from, to)
	if err != nil {
		s.fail(w, err, "clients report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "clients": list})
}

func (s *Server) handleReportAging(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Aging(r.Context())
	if err != nil {
		s.fail(w, err, "aging")
		return
	}
	open, _, _ := s.store.ListInvoices(r.Context(), store.InvoiceFilter{Status: "open", Limit: 500})
	writeJSON(w, http.StatusOK, map[string]any{"buckets": list, "invoices": open})
}

func (s *Server) handleReportTax(w http.ResponseWriter, r *http.Request) {
	from, to := dateRange(r)
	list, err := s.store.TaxByRate(r.Context(), from, to)
	if err != nil {
		s.fail(w, err, "tax")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "rates": list})
}

func (s *Server) handleReportCurrencies(w http.ResponseWriter, r *http.Request) {
	from, to := dateRange(r)
	list, err := s.store.ByCurrency(r.Context(), from, to)
	if err != nil {
		s.fail(w, err, "currencies report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "currencies": list})
}

func (s *Server) handleReportTime(w http.ResponseWriter, r *http.Request) {
	from, to := dateRange(r)
	list, err := s.store.TimeByClient(r.Context(), from, to)
	if err != nil {
		s.fail(w, err, "time report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "rows": list})
}

func f2s(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// handleExportCSV exports invoices, payments, time or expenses as CSV.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind := r.URL.Query().Get("type")
	from, to := dateRange(r)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s-%s.csv"`, firstNonEmpty(kind, "invoices"), from, to))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	switch kind {
	case "payments":
		list, err := s.store.ListPayments(ctx, store.PaymentFilter{From: from, To: to})
		if err != nil {
			s.fail(w, err, "export")
			return
		}
		_ = cw.Write([]string{"date", "invoice", "client", "amount", "currency", "exchange_rate", "applied_amount", "method", "reference", "notes"})
		for _, p := range list {
			_ = cw.Write([]string{p.Date, p.InvoiceNumber, p.ClientName, f2s(p.Amount), p.Currency, strconv.FormatFloat(p.ExchangeRate, 'f', -1, 64), f2s(p.AppliedAmount), p.Method, p.Reference, p.Notes})
		}
	case "time":
		list, err := s.store.ListTimeEntries(ctx, store.TimeFilter{From: from, To: to})
		if err != nil {
			s.fail(w, err, "export")
			return
		}
		_ = cw.Write([]string{"started_at", "ended_at", "client", "project", "description", "minutes", "hours", "billable", "invoice_id"})
		for _, t := range list {
			end, inv := "", ""
			if t.EndedAt != nil {
				end = *t.EndedAt
			}
			if t.InvoiceID != nil {
				inv = fmt.Sprint(*t.InvoiceID)
			}
			_ = cw.Write([]string{t.StartedAt, end, t.ClientName, t.Project, t.Description, fmt.Sprint(t.DurationMinutes), f2s(float64(t.DurationMinutes) / 60), fmt.Sprint(t.Billable), inv})
		}
	case "expenses":
		list, err := s.store.ListExpenses(ctx, store.ExpenseFilter{From: from, To: to})
		if err != nil {
			s.fail(w, err, "export")
			return
		}
		_ = cw.Write([]string{"date", "client", "category", "description", "amount", "currency", "exchange_rate", "billable", "invoice_id"})
		for _, e := range list {
			inv := ""
			if e.InvoiceID != nil {
				inv = fmt.Sprint(*e.InvoiceID)
			}
			_ = cw.Write([]string{e.Date, e.ClientName, e.Category, e.Description, f2s(e.Amount), e.Currency, strconv.FormatFloat(e.ExchangeRate, 'f', -1, 64), fmt.Sprint(e.Billable), inv})
		}
	case "items":
		list, _, err := s.store.ListInvoices(ctx, store.InvoiceFilter{From: from, To: to})
		if err != nil {
			s.fail(w, err, "export")
			return
		}
		_ = cw.Write([]string{"invoice", "issue_date", "client", "description", "unit", "quantity", "unit_price", "discount_pct", "tax_pct", "line_total", "currency"})
		for _, inv := range list {
			full, err := s.store.GetInvoice(ctx, inv.ID)
			if err != nil {
				continue
			}
			for _, it := range full.Items {
				_ = cw.Write([]string{inv.Number, inv.IssueDate, inv.ClientName, it.Description, it.Unit, strconv.FormatFloat(it.Quantity, 'f', -1, 64), f2s(it.UnitPrice), strconv.FormatFloat(it.Discount, 'f', -1, 64), strconv.FormatFloat(it.TaxRate, 'f', -1, 64), f2s(it.LineTotal), inv.Currency})
			}
		}
	default:
		list, _, err := s.store.ListInvoices(ctx, store.InvoiceFilter{From: from, To: to, Status: r.URL.Query().Get("status")})
		if err != nil {
			s.fail(w, err, "export")
			return
		}
		_ = cw.Write([]string{"number", "client", "status", "issue_date", "due_date", "currency", "subtotal", "discount", "tax", "total", "paid", "balance", "exchange_rate", "total_base", "billing_mode", "period_start", "period_end", "po_number", "paid_at"})
		for _, inv := range list {
			ps, pe, paidAt := "", "", ""
			if inv.PeriodStart != nil {
				ps = *inv.PeriodStart
			}
			if inv.PeriodEnd != nil {
				pe = *inv.PeriodEnd
			}
			if inv.PaidAt != nil {
				paidAt = *inv.PaidAt
			}
			_ = cw.Write([]string{inv.Number, inv.ClientName, inv.Status, inv.IssueDate, inv.DueDate, inv.Currency, f2s(inv.Subtotal), f2s(inv.DiscountTotal), f2s(inv.TaxTotal), f2s(inv.Total), f2s(inv.AmountPaid), f2s(inv.Balance), strconv.FormatFloat(inv.ExchangeRate, 'f', -1, 64), f2s(inv.Total * inv.ExchangeRate), inv.BillingMode, ps, pe, inv.PONumber, paidAt})
		}
	}
}
