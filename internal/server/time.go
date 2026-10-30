package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) handleListTime(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListTimeEntries(r.Context(), uidOf(r), store.TimeFilter{ClientID: qInt64(r, "client_id"), Unbilled: q.Get("unbilled") == "1", From: q.Get("from"), To: q.Get("to"), Limit: qInt(r, "limit", 500)})
	if err != nil {
		s.fail(w, err, "list time")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleRunningTimer is the person's running timer: here, or in Pocket Time
// when connected (wherever it was started). shared = the timer here is the
// one in Pocket Time; time_error = it could not be asked just now.
func (s *Server) handleRunningTimer(w http.ResponseWriter, r *http.Request) {
	v, shared, timeErr := s.currentTimer(r.Context(), uidOf(r))
	out := map[string]any{"running": v, "shared": shared}
	if timeErr != "" {
		out["time_error"] = timeErr
	}
	writeJSON(w, http.StatusOK, out)
}

// handleStartTimer starts a timer for a client. Connected to Pocket Time it
// starts the person's one timer there (409 with the running one, unless
// switch); otherwise here, stopping any timer running here (as always).
func (s *Server) handleStartTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	var in struct {
		ClientID    int64    `json:"client_id"`
		Project     string   `json:"project"`
		Description string   `json:"description"`
		Rate        *float64 `json:"rate"`
		Switch      bool     `json:"switch"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	client, err := s.store.GetClient(ctx, uid, in.ClientID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "client not found")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	tc, err := s.timeFor(ctx, uid)
	if err != nil && !isTimeOff(err) {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if tc != nil {
		// app + source: a direct (ptm_ token) connection can't tell Pocket Time who calls; a Core link sets them anyway
		body := map[string]any{"app": "invoicing", "source": "invoicing", "app_ref": clientRef(client.ID), "app_label": client.Name, "app_url": clientPath(client.ID),
			"title": strings.TrimSpace(in.Description), "billable": true, "deliver": true, "switch": in.Switch}
		if p := strings.TrimSpace(in.Project); p != "" {
			body["project"] = p // a Pocket Time project of that name, if there is one; kept here in any case
		}
		var res struct {
			Running *timeEntry `json:"running"`
		}
		code, err := tc.call(ctx, http.MethodPost, "/api/v1/timer/start", body, &res)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		if code == http.StatusConflict {
			v := s.fromTime(ctx, uid, res.Running)
			msg := "A timer is already running: " + v.name()
			if v != nil && !v.Ours {
				msg += " (started in " + v.StartedIn + ")"
			}
			writeJSON(w, http.StatusConflict, map[string]any{"error": msg + ".", "running": v})
			return
		}
		if res.Running == nil {
			writeErr(w, http.StatusBadGateway, "Pocket Time did not say which timer it started")
			return
		}
		if err := s.store.SaveSharedTimer(ctx, uid, store.SharedTimer{TimeEntryID: res.Running.ID, ClientID: client.ID, Project: strings.TrimSpace(in.Project), Rate: in.Rate}); err != nil {
			s.log.Warn("pocket time: keeping the timer's project failed", "err", err)
		}
		_, _ = s.store.StopRunningTimers(ctx, uid, st.TimeRoundingMin) // one from before Pocket Time was connected
		writeJSON(w, http.StatusCreated, s.fromTime(ctx, uid, res.Running))
		return
	}
	_, _ = s.store.StopRunningTimers(ctx, uid, st.TimeRoundingMin)
	t := &store.TimeEntry{ClientID: in.ClientID, Project: in.Project, Description: in.Description, StartedAt: time.Now().UTC().Format(time.RFC3339), Billable: true, Rate: in.Rate}
	if err := s.store.SaveTimeEntry(ctx, uid, t); err != nil {
		s.fail(w, err, "start timer")
		return
	}
	full, _ := s.store.GetTimeEntry(ctx, uid, t.ID)
	writeJSON(w, http.StatusCreated, full)
}

// handleStopTimer stops the running timer: one running here, else the one in
// Pocket Time. time_id = the Pocket Time timer the page shows (a different
// one running there now → 409, nothing stopped). Pocket Time sends the
// finished time back through the worklog.
func (s *Server) handleStopTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	var in struct {
		TimeID int64 `json:"time_id"`
	}
	_ = decode(r, &in)
	st, _ := s.store.GetSettings(ctx, uid)
	stopped, err := s.store.StopRunningTimers(ctx, uid, st.TimeRoundingMin)
	if err != nil {
		s.fail(w, err, "stop timer")
		return
	}
	out := map[string]any{"stopped": stopped}
	if len(stopped) > 0 && in.TimeID == 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	tc, err := s.timeFor(ctx, uid)
	if err != nil {
		if isTimeOff(err) || len(stopped) > 0 {
			writeJSON(w, http.StatusOK, out)
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	body := map[string]any{}
	if in.TimeID > 0 {
		body["only_if"] = in.TimeID
	}
	var res struct {
		Running *timeEntry `json:"running"`
		Stopped *timeEntry `json:"stopped"`
	}
	code, err := tc.call(ctx, http.MethodPost, "/api/v1/timer/stop", body, &res)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if code == http.StatusConflict {
		v := s.fromTime(ctx, uid, res.Running)
		writeJSON(w, http.StatusConflict, map[string]any{"error": "A different timer is running now: " + v.name() + ". Nothing was stopped.", "running": v})
		return
	}
	out["stopped_shared"] = s.fromTime(ctx, uid, res.Stopped)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var t store.TimeEntry
	if id := idParam(r, "id"); id > 0 {
		existing, err := s.store.GetTimeEntry(ctx, uidOf(r), id)
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
	if t.ID == 0 {
		t.Source, t.ExternalID = "app", "" // only the worklog writes these
	}
	t.InvoiceID = s.ownInvoiceID(ctx, uidOf(r), t.InvoiceID)
	if _, err := s.store.GetClient(ctx, uidOf(r), t.ClientID); err != nil {
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
	if err := s.store.SaveTimeEntry(ctx, uidOf(r), &t); err != nil {
		s.fail(w, err, "save time entry")
		return
	}
	full, _ := s.store.GetTimeEntry(ctx, uidOf(r), t.ID)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteTime(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTimeEntry(r.Context(), uidOf(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete time entry")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- expenses ----

func (s *Server) handleListExpenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.store.ListExpenses(r.Context(), uidOf(r), store.ExpenseFilter{ClientID: qInt64(r, "client_id"), Unbilled: q.Get("unbilled") == "1", From: q.Get("from"), To: q.Get("to")})
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
		existing, err := s.store.GetExpense(ctx, uidOf(r), id)
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
	st, _ := s.store.GetSettings(ctx, uidOf(r))
	if e.Date == "" {
		e.Date = store.Today()
	}
	if e.Currency == "" {
		e.Currency = st.BaseCurrency
	}
	e.ClientID = s.ownClientID(ctx, uidOf(r), e.ClientID)
	e.InvoiceID = s.ownInvoiceID(ctx, uidOf(r), e.InvoiceID)
	if e.ExchangeRate <= 0 {
		if r, ok := s.store.GetRate(ctx, e.Currency, st.BaseCurrency); ok {
			e.ExchangeRate = r
		} else {
			e.ExchangeRate = 1
		}
	}
	if err := s.store.SaveExpense(ctx, uidOf(r), &e); err != nil {
		s.fail(w, err, "save expense")
		return
	}
	full, _ := s.store.GetExpense(ctx, uidOf(r), e.ID)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteExpense(r.Context(), uidOf(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete expense")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
