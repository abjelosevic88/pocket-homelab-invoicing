package server

import (
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) handleListTime(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListTimeEntries(r.Context(), store.TimeFilter{ClientID: qInt64(r, "client_id"), Unbilled: q.Get("unbilled") == "1", From: q.Get("from"), To: q.Get("to"), Limit: qInt(r, "limit", 500)})
	if err != nil {
		s.fail(w, err, "list time")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleRunningTimer(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.RunningTimer(r.Context())
	if err != nil {
		s.fail(w, err, "running timer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": t})
}

func (s *Server) handleStartTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		ClientID    int64    `json:"client_id"`
		Project     string   `json:"project"`
		Description string   `json:"description"`
		Rate        *float64 `json:"rate"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := s.store.GetClient(ctx, in.ClientID); err != nil {
		writeErr(w, http.StatusBadRequest, "client not found")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	_, _ = s.store.StopRunningTimers(ctx, st.TimeRoundingMin)
	t := &store.TimeEntry{ClientID: in.ClientID, Project: in.Project, Description: in.Description, StartedAt: time.Now().UTC().Format(time.RFC3339), Billable: true, Rate: in.Rate}
	if err := s.store.SaveTimeEntry(ctx, t); err != nil {
		s.fail(w, err, "start timer")
		return
	}
	full, _ := s.store.GetTimeEntry(ctx, t.ID)
	writeJSON(w, http.StatusCreated, full)
}

func (s *Server) handleStopTimer(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context())
	stopped, err := s.store.StopRunningTimers(r.Context(), st.TimeRoundingMin)
	if err != nil {
		s.fail(w, err, "stop timer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": stopped})
}

func (s *Server) handleSaveTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var t store.TimeEntry
	if id := idParam(r, "id"); id > 0 {
		existing, err := s.store.GetTimeEntry(ctx, id)
		if err != nil {
			s.fail(w, err, "get time entry")
			return
		}
		t = *existing
	} else {
		t.Billable = true
	}
	if err := decode(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t.ID = idParam(r, "id")
	if _, err := s.store.GetClient(ctx, t.ClientID); err != nil {
		writeErr(w, http.StatusBadRequest, "client not found")
		return
	}
	if t.StartedAt == "" {
		t.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if len(t.StartedAt) == 10 { // date only
		t.StartedAt += "T09:00:00Z"
	}
	start, err := time.Parse(time.RFC3339, t.StartedAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "started_at must be RFC3339 or YYYY-MM-DD")
		return
	}
	// Either ended_at or duration_minutes drives the other.
	if t.EndedAt != nil && *t.EndedAt != "" {
		end, err := time.Parse(time.RFC3339, *t.EndedAt)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "ended_at must be RFC3339")
			return
		}
		if t.DurationMinutes <= 0 {
			t.DurationMinutes = int(end.Sub(start).Minutes() + 0.5)
		}
	} else if t.DurationMinutes > 0 {
		end := start.Add(time.Duration(t.DurationMinutes) * time.Minute).UTC().Format(time.RFC3339)
		t.EndedAt = &end
	}
	if t.DurationMinutes < 0 {
		t.DurationMinutes = 0
	}
	if err := s.store.SaveTimeEntry(ctx, &t); err != nil {
		s.fail(w, err, "save time entry")
		return
	}
	full, _ := s.store.GetTimeEntry(ctx, t.ID)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteTime(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTimeEntry(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete time entry")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- expenses ----

func (s *Server) handleListExpenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListExpenses(r.Context(), store.ExpenseFilter{ClientID: qInt64(r, "client_id"), Unbilled: q.Get("unbilled") == "1", From: q.Get("from"), To: q.Get("to")})
	if err != nil {
		s.fail(w, err, "list expenses")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveExpense(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var e store.Expense
	if id := idParam(r, "id"); id > 0 {
		existing, err := s.store.GetExpense(ctx, id)
		if err != nil {
			s.fail(w, err, "get expense")
			return
		}
		e = *existing
	}
	if err := decode(r, &e); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	e.ID = idParam(r, "id")
	st, _ := s.store.GetSettings(ctx)
	if e.Date == "" {
		e.Date = store.Today()
	}
	if e.Currency == "" {
		e.Currency = st.BaseCurrency
	}
	if e.ClientID != nil && *e.ClientID == 0 {
		e.ClientID = nil
	}
	if e.ExchangeRate <= 0 {
		if r, ok := s.store.GetRate(ctx, e.Currency, st.BaseCurrency); ok {
			e.ExchangeRate = r
		} else {
			e.ExchangeRate = 1
		}
	}
	if err := s.store.SaveExpense(ctx, &e); err != nil {
		s.fail(w, err, "save expense")
		return
	}
	full, _ := s.store.GetExpense(ctx, e.ID)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteExpense(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete expense")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
