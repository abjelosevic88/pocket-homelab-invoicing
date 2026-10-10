package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/calendar"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// The month-end wizard proposes one invoice per client for a calendar month,
// creates the drafts in one go and lists everything for that month so it can be
// finished (fiscal PDF uploaded, emailed) from a single screen.

type monthEndRow struct {
	ClientID       int64               `json:"client_id"`
	ClientName     string              `json:"client_name"`
	ClientEmail    string              `json:"client_email"`
	Currency       string              `json:"currency"`
	BillingMode    string              `json:"billing_mode"`
	RecurringID    *int64              `json:"recurring_id"`
	RecurringName  string              `json:"recurring_name"`
	TemplateID     *int64              `json:"template_id"`
	Include        bool                `json:"include"`
	Items          []store.InvoiceItem `json:"items"`
	QuantitySource string              `json:"quantity_source"` // working_days | time_entries | profile | client
	UnbilledMin    int                 `json:"unbilled_minutes"`
	TimeEntryIDs   []int64             `json:"time_entry_ids"`
	Existing       []store.Invoice     `json:"existing"`
	CustomFields   map[string]string   `json:"custom_fields"`      // suggested values
	CustomLast     map[string]string   `json:"custom_fields_last"` // last used values (for hints)
	DueDate        string              `json:"due_date"`
	Notes          string              `json:"notes"`
	Terms          string              `json:"terms"`
	Hint           string              `json:"hint"`
}

var counterRe = regexp.MustCompile(`^(\d+)(?:/(\d+))?(\D*)$`)

// bumpCounter suggests the next value of a simple counter such as "14/15ПП" -> "15/16ПП"
// or "0031" -> "0032". Anything else yields "" (no suggestion).
func bumpCounter(v string) string {
	m := counterRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return ""
	}
	inc := func(d string) string {
		n, _ := strconv.Atoi(d)
		return fmt.Sprintf("%0*d", len(d), n+1)
	}
	out := inc(m[1])
	if m[2] != "" {
		out += "/" + inc(m[2])
	}
	return out + m[3]
}

// monthEndPlan builds the proposal for a month.
func (s *Server) monthEndPlan(ctx context.Context, month string) (map[string]any, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	a, b, err := calendar.MonthRange(month)
	if err != nil {
		return nil, err
	}
	ps, pe := a.Format("2006-01-02"), b.Format("2006-01-02")
	issue := b.AddDate(0, 0, 1).Format("2006-01-02")
	if issue > store.Today() {
		issue = store.Today()
	}
	wd, _ := workingDays(st, ps, pe)
	allHol := calendar.Count(a, b, calendar.ParseWorkWeek("1,2,3,4,5,6,7"), st.Holidays).Holidays

	clients, err := s.store.ListClients(ctx, false, "")
	if err != nil {
		return nil, err
	}
	profiles, _ := s.store.ListRecurring(ctx)
	profileFor := func(cid int64) *store.RecurringInvoice {
		var best *store.RecurringInvoice
		for i := range profiles {
			r := &profiles[i]
			if r.ClientID != cid || r.Status == "completed" || r.Frequency != "monthly" {
				continue
			}
			if best == nil || (best.Status != "active" && r.Status == "active") {
				best = r
			}
		}
		return best
	}
	monthLabel := a.Format("January 2006")
	rows := []monthEndRow{}
	all := []store.Invoice{}
	for _, c := range clients {
		row := monthEndRow{ClientID: c.ID, ClientName: c.Name, ClientEmail: c.Email, Currency: firstNonEmpty(c.Currency, st.BaseCurrency), BillingMode: firstNonEmpty(c.BillingMode, st.DefaultBilling), TemplateID: c.TemplateID, TimeEntryIDs: []int64{}, CustomFields: map[string]string{}, CustomLast: map[string]string{}}
		row.Existing, _ = s.store.InvoicesForPeriod(ctx, c.ID, ps, pe)
		if row.Existing == nil {
			row.Existing = []store.Invoice{}
		}
		all = append(all, row.Existing...)

		rec := profileFor(c.ID)
		unbilled, _ := s.store.ListTimeEntries(ctx, store.TimeFilter{ClientID: c.ID, Unbilled: true, From: ps, To: pe})
		for _, t := range unbilled {
			row.UnbilledMin += t.DurationMinutes
			row.TimeEntryIDs = append(row.TimeEntryIDs, t.ID)
		}
		dueDays := firstNonZero(c.PaymentTermsDays, st.DefaultDueDays)
		row.Notes, row.Terms = st.DefaultNotes, strings.ReplaceAll(st.DefaultTerms, "{due_days}", fmt.Sprint(dueDays))
		if rec != nil {
			row.RecurringID, row.RecurringName = &rec.ID, rec.Name
			row.Currency, row.BillingMode = rec.Currency, rec.BillingMode
			if rec.TemplateID != nil && *rec.TemplateID > 0 {
				row.TemplateID = rec.TemplateID
			}
			if rec.DueDays > 0 {
				dueDays = rec.DueDays
			}
			row.Notes, row.Terms = rec.Notes, rec.Terms
			row.Items, row.QuantitySource = recurringItems(rec, st, ps, pe)
		} else {
			unit := unitForBilling(row.BillingMode)
			qty := 1.0
			row.QuantitySource = "client"
			switch unit {
			case "day":
				qty, row.QuantitySource = float64(wd.WorkingDays), "working_days"
			case "hour":
				qty, row.QuantitySource = float64(wd.WorkingDays)*st.HoursPerDay, "working_days"
			}
			rate := c.DefaultRate
			if rate == 0 {
				switch unit {
				case "day":
					rate = st.DefaultDailyRate
				case "hour":
					rate = st.DefaultHourlyRate
				case "month":
					rate = st.DefaultMonthlyRate
				}
			}
			row.Items = []store.InvoiceItem{{Description: "Services – " + monthLabel, Unit: unit, Quantity: qty, UnitPrice: rate, TaxRate: st.DefaultTaxRate}}
		}
		// Tracked time beats any estimate for hourly lines.
		if row.UnbilledMin > 0 {
			hoursTracked := float64(row.UnbilledMin) / 60
			for i := range row.Items {
				if row.Items[i].Unit == "hour" {
					row.Items[i].Quantity = hoursTracked
					row.QuantitySource = "time_entries"
					break
				}
			}
		}
		if t, err := time.Parse("2006-01-02", issue); err == nil {
			row.DueDate = t.AddDate(0, 0, dueDays).Format("2006-01-02")
		}
		// Fiscal counters: suggest the next value of simple numeric fields.
		if last, err := s.store.LatestCustomFields(ctx, c.ID, true); err == nil {
			for _, f := range st.CustomFields {
				if v, ok := last[f.Key]; ok {
					row.CustomLast[f.Key] = v
					if next := bumpCounter(v); next != "" {
						row.CustomFields[f.Key] = next
					}
				}
			}
		}
		open := 0
		for _, e := range row.Existing {
			if e.Status != store.StatusCancelled {
				open++
			}
		}
		switch {
		case open > 0:
			row.Include = false
			row.Hint = fmt.Sprintf("Already invoiced for %s (%s)", monthLabel, row.Existing[0].Number)
		case rec != nil || row.UnbilledMin > 0:
			row.Include = true
		default:
			row.Include = false
			row.Hint = "No recurring profile or tracked time for this month; tick to invoice anyway"
		}
		switch row.QuantitySource {
		case "working_days":
			row.Hint = firstNonEmpty(row.Hint, fmt.Sprintf("%d working days in %s", wd.WorkingDays, monthLabel))
		case "time_entries":
			row.Hint = firstNonEmpty(row.Hint, fmt.Sprintf("%.2f h tracked and unbilled", float64(row.UnbilledMin)/60))
		case "profile":
			row.Hint = firstNonEmpty(row.Hint, "Quantity from the recurring profile")
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"month": month, "month_label": monthLabel, "period_start": ps, "period_end": pe, "issue_date": issue,
		"calendar": wd, "all_holidays": allHol, "work_week": calendar.NormalizeWorkWeek(st.WorkWeek), "hours_per_day": st.HoursPerDay, "custom_fields": st.CustomFields,
		"rows": rows, "invoices": all, "smtp_configured": st.SMTPHost != "",
	}, nil
}

func unitForBilling(mode string) string {
	switch mode {
	case "hourly":
		return "hour"
	case "daily":
		return "day"
	case "monthly":
		return "month"
	}
	return "unit"
}

// handleMonthEndPlan: GET /month-end?month=YYYY-MM
func (s *Server) handleMonthEndPlan(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().AddDate(0, -1, 0).Format("2006-01")
	}
	plan, err := s.monthEndPlan(r.Context(), month)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

type monthEndCreateRow struct {
	ClientID     int64               `json:"client_id"`
	RecurringID  *int64              `json:"recurring_id"`
	TemplateID   *int64              `json:"template_id"`
	Currency     string              `json:"currency"`
	BillingMode  string              `json:"billing_mode"`
	Items        []store.InvoiceItem `json:"items"`
	CustomFields map[string]string   `json:"custom_fields"`
	TimeEntryIDs []int64             `json:"time_entry_ids"`
	DueDate      string              `json:"due_date"`
	Notes        string              `json:"notes"`
	Terms        string              `json:"terms"`
	WorkedDays   []string            `json:"worked_days"` // dates picked in the calendar (YYYY-MM-DD)
}

// handleMonthEndCreate: POST /month-end  {month, issue_date, rows:[...]} -> creates draft invoices.
func (s *Server) handleMonthEndCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Month     string              `json:"month"`
		IssueDate string              `json:"issue_date"`
		Rows      []monthEndCreateRow `json:"rows"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a, b, err := calendar.MonthRange(in.Month)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ps, pe := a.Format("2006-01-02"), b.Format("2006-01-02")
	if in.IssueDate == "" {
		in.IssueDate = store.Today()
	}
	if _, err := time.Parse("2006-01-02", in.IssueDate); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid issue_date")
		return
	}
	if len(in.Rows) == 0 {
		writeErr(w, http.StatusBadRequest, "no rows selected")
		return
	}
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	created := []store.Invoice{}
	errs := []string{}
	for _, row := range in.Rows {
		items := row.Items[:0]
		for _, it := range row.Items {
			if strings.TrimSpace(it.Description) == "" && it.Quantity == 0 {
				continue
			}
			items = append(items, it)
		}
		if len(items) == 0 {
			errs = append(errs, fmt.Sprintf("client %d: no line items", row.ClientID))
			continue
		}
		inp := invoiceInput{
			ClientID: row.ClientID, Currency: row.Currency, BillingMode: row.BillingMode, IssueDate: in.IssueDate, DueDate: row.DueDate,
			DiscountType: "none", Notes: row.Notes, Terms: row.Terms, Footer: st.DefaultFooter, TemplateID: row.TemplateID,
			PeriodStart: &ps, PeriodEnd: &pe, Items: items, CustomFields: row.CustomFields, WorkedDays: row.WorkedDays,
		}
		inv := &store.Invoice{Status: store.StatusDraft}
		if err := s.applyInput(ctx, inv, inp, st); err != nil {
			errs = append(errs, fmt.Sprintf("client %d: %v", row.ClientID, err))
			continue
		}
		inv.RecurringID = row.RecurringID
		if inv.Number, err = s.nextNumber(ctx, &st, inv.IssueDate, inv.ClientName); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", inv.ClientName, err))
			continue
		}
		if err := s.store.CreateInvoice(ctx, inv); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", inv.ClientName, err))
			continue
		}
		if len(row.TimeEntryIDs) > 0 {
			_ = s.store.LinkTimeEntries(ctx, inv.ID, row.TimeEntryIDs)
		}
		msg := "Created by the month-end wizard for " + a.Format("January 2006")
		if len(row.WorkedDays) > 0 {
			msg += " · days worked: " + describeWorkedDays(row.WorkedDays)
		}
		s.store.LogActivity(ctx, "invoice", inv.ID, "created", msg)
		// Keep the recurring schedule in step so the scheduler does not generate the same period again.
		if row.RecurringID != nil {
			if rec, err := s.store.GetRecurring(ctx, *row.RecurringID); err == nil && rec.ClientID == row.ClientID {
				runDate := runDateFor(ps, pe, rec.PeriodMode)
				advanced := false
				for guard := 0; guard < 24 && rec.NextRun <= runDate; guard++ {
					rec.NextRun = NextRun(rec.NextRun, rec.Frequency, rec.Interval)
					advanced = true
				}
				if advanced {
					rec.LastRun = &in.IssueDate
					rec.Occurrences++
					if (rec.MaxOccurrences > 0 && rec.Occurrences >= rec.MaxOccurrences) || (rec.EndDate != nil && rec.NextRun > *rec.EndDate) {
						rec.Status = "completed"
					}
					_ = s.store.SaveRecurring(ctx, rec)
				}
			}
		}
		full, _ := s.store.GetInvoice(ctx, inv.ID)
		if full != nil {
			created = append(created, *full)
			s.hooks.Emit("invoice.created", full)
		}
	}
	status := http.StatusCreated
	if len(created) == 0 && len(errs) > 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"invoices": created, "errors": errs})
}

// describeWorkedDays compresses ["2026-10-01","2026-10-02","2026-10-05"] into "1, 2, 5 (3 days)".
func describeWorkedDays(days []string) string {
	cp := append([]string{}, days...)
	sort.Strings(cp)
	parts := make([]string, 0, len(cp))
	for _, d := range cp {
		if len(d) >= 10 {
			parts = append(parts, strings.TrimLeft(d[8:10], "0"))
		}
	}
	return fmt.Sprintf("%s (%d days)", strings.Join(parts, ", "), len(parts))
}
