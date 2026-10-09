package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// NextRun computes the next run date after `from` for a frequency/interval.
func NextRun(from string, frequency string, interval int) string {
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		t = time.Now()
	}
	if interval < 1 {
		interval = 1
	}
	switch frequency {
	case "daily":
		t = t.AddDate(0, 0, interval)
	case "weekly":
		t = t.AddDate(0, 0, 7*interval)
	case "biweekly":
		t = t.AddDate(0, 0, 14*interval)
	case "quarterly":
		t = addMonthsClamped(t, 3*interval)
	case "yearly":
		t = t.AddDate(interval, 0, 0)
	default: // monthly
		t = addMonthsClamped(t, interval)
	}
	return t.Format("2006-01-02")
}

// addMonthsClamped adds months keeping the day-of-month when possible (Jan 31 + 1 month = Feb 28/29).
func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, t.Location())
}

// periodFor returns the service period covered by a run on `runDate`.
func periodFor(runDate, frequency string, interval int) (string, string) {
	t, err := time.Parse("2006-01-02", runDate)
	if err != nil {
		return "", ""
	}
	end, _ := time.Parse("2006-01-02", NextRun(runDate, frequency, interval))
	return t.Format("2006-01-02"), end.AddDate(0, 0, -1).Format("2006-01-02")
}

func (s *Server) handleListRecurring(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRecurring(r.Context())
	if err != nil {
		s.fail(w, err, "list recurring")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetRecurring(w http.ResponseWriter, r *http.Request) {
	rec, err := s.store.GetRecurring(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recurring")
		return
	}
	invoices, _, _ := s.store.ListInvoices(r.Context(), store.InvoiceFilter{Limit: 100})
	var generated []store.Invoice
	for _, inv := range invoices {
		if inv.RecurringID != nil && *inv.RecurringID == rec.ID {
			generated = append(generated, inv)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recurring": rec, "invoices": generated})
}

func (s *Server) handleSaveRecurring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var rec store.RecurringInvoice
	if id := idParam(r, "id"); id > 0 {
		existing, err := s.store.GetRecurring(ctx, id)
		if err != nil {
			s.fail(w, err, "get recurring")
			return
		}
		rec = *existing
	}
	if err := decode(r, &rec); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec.ID = idParam(r, "id")
	client, err := s.store.GetClient(ctx, rec.ClientID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "client not found")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	if strings.TrimSpace(rec.Name) == "" {
		rec.Name = client.Name + " – " + firstNonEmpty(rec.Frequency, "monthly")
	}
	if rec.Frequency == "" {
		rec.Frequency = "monthly"
	}
	if rec.Interval < 1 {
		rec.Interval = 1
	}
	if rec.Status == "" {
		rec.Status = "active"
	}
	if rec.StartDate == "" {
		rec.StartDate = store.Today()
	}
	if rec.NextRun == "" || rec.NextRun < rec.StartDate {
		rec.NextRun = rec.StartDate
	}
	if rec.Currency == "" {
		rec.Currency = firstNonEmpty(client.Currency, st.BaseCurrency)
	}
	rec.Currency = strings.ToUpper(rec.Currency)
	if rec.BillingMode == "" {
		rec.BillingMode = "monthly"
	}
	if rec.DueDays <= 0 {
		rec.DueDays = firstNonZero(client.PaymentTermsDays, st.DefaultDueDays)
	}
	if rec.DiscountType == "" {
		rec.DiscountType = "none"
	}
	if rec.EndDate != nil && *rec.EndDate == "" {
		rec.EndDate = nil
	}
	if len(rec.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one line item is required")
		return
	}
	if err := s.store.SaveRecurring(ctx, &rec); err != nil {
		s.fail(w, err, "save recurring")
		return
	}
	saved, _ := s.store.GetRecurring(ctx, rec.ID)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteRecurring(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRecurring(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete recurring")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRunRecurring(w http.ResponseWriter, r *http.Request) {
	rec, err := s.store.GetRecurring(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get recurring")
		return
	}
	inv, err := s.runRecurring(r.Context(), rec, store.Today(), true)
	if err != nil {
		s.fail(w, err, "run recurring")
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

// runRecurring generates one invoice for the profile dated runDate and advances the schedule.
func (s *Server) runRecurring(ctx context.Context, rec *store.RecurringInvoice, runDate string, manual bool) (*store.Invoice, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	ps, pe := periodFor(runDate, rec.Frequency, rec.Interval)
	in := invoiceInput{
		ClientID: rec.ClientID, Currency: rec.Currency, BillingMode: rec.BillingMode, IssueDate: runDate,
		DiscountType: rec.DiscountType, DiscountValue: rec.DiscountValue, Notes: rec.Notes, Terms: rec.Terms, Footer: st.DefaultFooter, TemplateID: rec.TemplateID,
		PeriodStart: &ps, PeriodEnd: &pe,
	}
	t, _ := time.Parse("2006-01-02", runDate)
	in.DueDate = t.AddDate(0, 0, rec.DueDays).Format("2006-01-02")
	for _, it := range rec.Items {
		desc := strings.ReplaceAll(it.Description, "{period}", fmt.Sprintf("%s – %s", ps, pe))
		desc = strings.ReplaceAll(desc, "{month}", t.Format("January 2006"))
		desc = strings.ReplaceAll(desc, "{year}", t.Format("2006"))
		in.Items = append(in.Items, store.InvoiceItem{Description: desc, Unit: it.Unit, Quantity: it.Quantity, UnitPrice: it.UnitPrice, TaxRate: it.TaxRate, Discount: it.Discount})
	}
	inv := &store.Invoice{Status: store.StatusDraft}
	if err := s.applyInput(ctx, inv, in, st); err != nil {
		return nil, err
	}
	inv.RecurringID = &rec.ID
	inv.Number, err = s.nextNumber(ctx, &st, inv.IssueDate, inv.ClientName)
	if err != nil {
		return nil, err
	}
	if rec.AutoSend {
		s.transition(inv, store.StatusSent)
	}
	if err := s.store.CreateInvoice(ctx, inv); err != nil {
		return nil, err
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "created", "Generated by recurring profile "+rec.Name)
	// advance schedule
	if !manual || rec.NextRun <= runDate {
		rec.NextRun = NextRun(rec.NextRun, rec.Frequency, rec.Interval)
	}
	rec.LastRun = &runDate
	rec.Occurrences++
	if (rec.MaxOccurrences > 0 && rec.Occurrences >= rec.MaxOccurrences) || (rec.EndDate != nil && rec.NextRun > *rec.EndDate) {
		rec.Status = "completed"
	}
	if err := s.store.SaveRecurring(ctx, rec); err != nil {
		return nil, err
	}
	full, _ := s.store.GetInvoice(ctx, inv.ID)
	s.hooks.Emit("invoice.created", full)
	if rec.AutoSend {
		if err := s.sendInvoiceEmail(ctx, full, "", "", ""); err != nil {
			s.log.Warn("recurring: auto-send failed", "invoice", full.Number, "err", err)
		} else {
			s.hooks.Emit("invoice.sent", full)
		}
	}
	return full, nil
}

// RunScheduler executes all periodic jobs once. Safe to call concurrently with requests.
func (s *Server) RunScheduler(ctx context.Context) map[string]any {
	result := map[string]any{}
	// 1. Recurring invoices (catch up if several periods were missed while the container was down)
	due, err := s.store.DueRecurring(ctx)
	if err != nil {
		s.log.Error("scheduler: list due recurring", "err", err)
	}
	generated := 0
	for i := range due {
		rec := &due[i]
		for guard := 0; guard < 36 && rec.Status == "active" && rec.NextRun <= store.Today(); guard++ {
			if _, err := s.runRecurring(ctx, rec, rec.NextRun, false); err != nil {
				s.log.Error("scheduler: recurring failed", "profile", rec.Name, "err", err)
				break
			}
			generated++
		}
	}
	result["recurring_generated"] = generated

	// 2. Overdue marking
	ids, err := s.store.MarkOverdue(ctx)
	if err != nil {
		s.log.Error("scheduler: mark overdue", "err", err)
	}
	for _, id := range ids {
		if inv, err := s.store.GetInvoice(ctx, id); err == nil {
			s.store.LogActivity(ctx, "invoice", id, "status", "Marked overdue")
			s.hooks.Emit("invoice.overdue", inv)
		}
	}
	result["marked_overdue"] = len(ids)

	// 2b. Finish pending Paperless consume tasks (no-op when the integration is off)
	if n := s.resolvePendingPaperless(ctx); n > 0 {
		result["paperless_resolved"] = n
	}

	// 3. Payment reminders
	st, _ := s.store.GetSettings(ctx)
	reminders := 0
	if st.RemindersEnabled && st.SMTPHost != "" {
		var days []int
		for _, d := range strings.Split(st.ReminderDays, ",") {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(d), "%d", &n); err == nil && n > 0 {
				days = append(days, n)
			}
		}
		list, _ := s.store.InvoicesDueForReminder(ctx, days)
		for _, inv := range list {
			full, err := s.store.GetInvoice(ctx, inv.ID)
			if err != nil {
				continue
			}
			subj := "Reminder: invoice {number} is overdue"
			body := "Hi {client},\n\nThis is a friendly reminder that invoice {number} for {balance} was due on {due_date}.\n\nView it online: {link}\n\nThanks,\n{company}"
			if err := s.sendInvoiceEmail(ctx, full, "", subj, body); err != nil {
				s.log.Warn("scheduler: reminder failed", "invoice", full.Number, "err", err)
				continue
			}
			s.store.LogActivity(ctx, "invoice", full.ID, "reminder", "Payment reminder sent")
			reminders++
		}
	}
	result["reminders_sent"] = reminders

	// 4. Exchange rates
	if s.rates.Name() != "none" {
		last, _ := s.store.GetKV(ctx, "rates_last_refresh")
		var lastT time.Time
		if last != "" {
			lastT, _ = time.Parse(time.RFC3339, last)
		}
		if time.Since(lastT) >= s.cfg.ExchangeRateRefresh {
			if n, err := s.refreshRates(ctx); err != nil {
				s.log.Warn("scheduler: rate refresh failed", "err", err)
			} else {
				result["rates_updated"] = n
				_ = s.store.SetKV(ctx, "rates_last_refresh", time.Now().UTC().Format(time.RFC3339))
			}
		}
	}
	_ = s.store.PurgeSessions(ctx)
	_ = s.store.SetKV(ctx, "scheduler_last_run", time.Now().UTC().Format(time.RFC3339))
	return result
}

func (s *Server) handleRunScheduler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.RunScheduler(r.Context()))
}
