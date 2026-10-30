package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- the worklog (Pocket Time's contract, docs/worklog.md in pocket-homelab-time) ----
//
// Pocket Time shows the time entries kept here (worklog:read) and writes time
// tracked there for a client here as an unbilled entry, ready to invoice
// (worklog:write). What can be logged to: the person's active clients, ref
// "client:<id>". Writing the same external_id again ("time:<entry id>")
// updates that entry; time already on an invoice is never changed or removed.
// A Core link may only change entries its own app wrote (source = its slug).
// The person themselves (an API token of their own: Pocket Time connected
// directly) may change any entry written through the worklog.

type worklogEntry struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	StartedAt  string `json:"started_at,omitempty"`
	EndedAt    string `json:"ended_at,omitempty"`
	Date       string `json:"date"`
	DurationS  int    `json:"duration_s"`
	Ref        string `json:"ref"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

type worklogItem struct {
	Ref    string `json:"ref"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url"`
}

func clientRef(id int64) string  { return "client:" + strconv.FormatInt(id, 10) }
func clientPath(id int64) string { return "/clients/" + strconv.FormatInt(id, 10) }

// parseClientRef reads "client:<id>".
func parseClientRef(ref string) int64 {
	rest, ok := strings.CutPrefix(strings.TrimSpace(ref), "client:")
	if !ok {
		return 0
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// parseStamp reads the times stored here (RFC3339; older imports may lack the zone).
func parseStamp(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func validDay(d string) bool {
	_, err := time.Parse("2006-01-02", d)
	return err == nil
}

// worklogSource is the source written on an entry: the acting app's slug for
// a Core link, else what the external id names ("time:812" → "time"), else "api".
func worklogSource(ctx context.Context, externalID string) string {
	if app := linkApp(ctx); app != "" {
		return app
	}
	if i := strings.IndexByte(externalID, ':'); i > 0 {
		return strings.ToLower(externalID[:i])
	}
	return "api"
}

// mayChange reports whether the caller may change or remove an entry through the worklog.
func mayChange(ctx context.Context, t *store.TimeEntry) bool {
	if app := linkApp(ctx); app != "" {
		return t.Source == app
	}
	return t.ExternalID != "" || (t.Source != "" && t.Source != "app")
}

func (s *Server) invoicedMsg(ctx context.Context, uid int64, t *store.TimeEntry) string {
	what := "an invoice"
	if t.InvoiceID != nil {
		if n := s.store.InvoiceNumber(ctx, uid, *t.InvoiceID); n != "" {
			what = "invoice " + n
		}
	}
	return "this time is already on " + what + " in Pocket Invoicing, so it can't be changed from here any more"
}

// handleWorklog lists the person's finished time entries by local date (?from=&to=, inclusive).
func (s *Server) handleWorklog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	q := r.URL.Query()
	to := strings.TrimSpace(q.Get("to"))
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	from := strings.TrimSpace(q.Get("from"))
	if from == "" {
		from = to
	}
	if !validDay(from) || !validDay(to) || from > to {
		writeErr(w, http.StatusBadRequest, "from and to are days (YYYY-MM-DD), from not after to")
		return
	}
	list, err := s.store.FinishedTimeAround(ctx, uid, from, to)
	if err != nil {
		s.fail(w, err, "worklog")
		return
	}
	out := make([]worklogEntry, 0, len(list))
	for _, t := range list {
		start, ok := parseStamp(t.StartedAt)
		if !ok {
			continue
		}
		start = start.In(time.Local)
		day := start.Format("2006-01-02")
		if day < from || day > to {
			continue
		}
		e := worklogEntry{ID: strconv.FormatInt(t.ID, 10), StartedAt: start.Format(time.RFC3339), Date: day, DurationS: t.DurationMinutes * 60,
			Ref: clientRef(t.ClientID), Label: t.ClientName, URL: clientPath(t.ClientID), Source: t.Source, ExternalID: t.ExternalID}
		if t.EndedAt != nil {
			if end, ok := parseStamp(*t.EndedAt); ok {
				e.EndedAt = end.In(time.Local).Format(time.RFC3339)
			}
		}
		e.Title = firstNonEmpty(strings.TrimSpace(t.Description), strings.TrimSpace(t.Project), t.ClientName)
		if t.Project != "" && e.Title != t.Project {
			e.Label = t.ClientName + " · " + t.Project
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// handleWorklogItems lists what time can be for here: the person's active
// clients, the ones with recent time first (?q= searches the name).
func (s *Server) handleWorklogItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.TimeClients(ctx, uidOf(r), strings.TrimSpace(r.URL.Query().Get("q")), 50)
	if err != nil {
		s.fail(w, err, "worklog items")
		return
	}
	out := make([]worklogItem, 0, len(list))
	for _, c := range list {
		it := worklogItem{Ref: clientRef(c.ID), Label: c.Name, URL: clientPath(c.ID)}
		if c.UnbilledMinutes > 0 {
			it.Detail = "Client · " + spokenHours(c.UnbilledMinutes) + " not invoiced yet"
		} else {
			it.Detail = "Client"
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func spokenHours(minutes int) string {
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

type worklogInput struct {
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	Note       string `json:"note"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	Date       string `json:"date"`
	DurationS  int    `json:"duration_s"`
	Billable   *bool  `json:"billable"`
	Project    string `json:"project"`
	ExternalID string `json:"external_id"`
}

// handleWorklogWrite logs time tracked elsewhere as an unbilled entry for a
// client; the same external_id again updates it (never once it is invoiced).
func (s *Server) handleWorklogWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	var in worklogInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.ExternalID = strings.TrimSpace(in.ExternalID)
	if len(in.ExternalID) > 200 {
		writeErr(w, http.StatusBadRequest, "external_id is too long")
		return
	}
	cid := parseClientRef(in.Ref)
	if cid == 0 {
		writeErr(w, http.StatusBadRequest, "choose a client: time in Pocket Invoicing is always for one of your clients")
		return
	}
	client, err := s.store.GetClient(ctx, uid, cid)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that client is not one of yours in Pocket Invoicing (it may have been deleted)")
		return
	}
	if in.DurationS <= 0 {
		writeErr(w, http.StatusBadRequest, "the time has no length (duration_s)")
		return
	}
	var start time.Time
	if v := strings.TrimSpace(in.StartedAt); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "started_at is a time (RFC3339)")
			return
		}
		start = t
	} else if validDay(in.Date) {
		start, _ = time.ParseInLocation("2006-01-02 15:04", in.Date+" 09:00", time.Local)
	} else {
		writeErr(w, http.StatusBadRequest, "say when it was: started_at or date")
		return
	}
	end := start.Add(time.Duration(in.DurationS) * time.Second)
	if v := strings.TrimSpace(in.EndedAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil && t.After(start) {
			end = t
		}
	}

	var existing *store.TimeEntry
	if in.ExternalID != "" {
		existing, err = s.store.TimeByExternalID(ctx, uid, in.ExternalID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.fail(w, err, "worklog")
			return
		}
	}
	t := &store.TimeEntry{Billable: true, Source: worklogSource(ctx, in.ExternalID), ExternalID: in.ExternalID}
	if existing != nil {
		if !mayChange(ctx, existing) {
			writeErr(w, http.StatusConflict, "another app already logged time here with this id, so it can't be changed from here")
			return
		}
		if existing.InvoiceID != nil {
			writeErr(w, http.StatusConflict, s.invoicedMsg(ctx, uid, existing))
			return
		}
		t = existing
	}
	// What the timer started here kept for itself (a project, a rate override).
	var kept *store.SharedTimer
	if id, ok := strings.CutPrefix(in.ExternalID, "time:"); ok {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			if k := s.store.GetSharedTimer(ctx, uid, n); k != nil && k.ClientID == cid {
				kept = k
			}
		}
	}
	st, _ := s.store.GetSettings(ctx, uid)
	t.ClientID = cid
	desc := strings.TrimSpace(in.Title)
	if desc == client.Name || desc == "Pocket Invoicing" { // Pocket Time names an untitled timer after what it is for
		desc = ""
	}
	t.Description = clipText(desc, 500)
	switch {
	case strings.TrimSpace(in.Project) != "":
		t.Project = clipText(in.Project, 200)
	case kept != nil && kept.Project != "":
		t.Project = kept.Project
	}
	if kept != nil && kept.Rate != nil && existing == nil {
		t.Rate = kept.Rate
	}
	if in.Billable != nil {
		t.Billable = *in.Billable
	}
	t.StartedAt = start.UTC().Format(time.RFC3339)
	es := end.UTC().Format(time.RFC3339)
	t.EndedAt = &es
	// rounded like the timer here when it stops (Settings → Invoicing)
	t.DurationMinutes = store.RoundMinutes(int(float64(in.DurationS)/60+0.5), st.TimeRoundingMin)
	if err := s.store.SaveTimeEntry(ctx, uid, t); err != nil {
		s.fail(w, err, "worklog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": strconv.FormatInt(t.ID, 10), "url": clientPath(cid)})
}

// handleWorklogDelete removes an entry written through the worklog, unless it is invoiced.
func (s *Server) handleWorklogDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusNotFound, "no such time entry")
		return
	}
	t, err := s.store.GetTimeEntry(ctx, uid, id)
	if err != nil || !mayChange(ctx, t) {
		writeErr(w, http.StatusNotFound, "no such time entry written from there")
		return
	}
	if t.InvoiceID != nil {
		writeErr(w, http.StatusConflict, s.invoicedMsg(ctx, uid, t))
		return
	}
	if err := s.store.DeleteTimeEntry(ctx, uid, id); err != nil {
		s.fail(w, err, "worklog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func clipText(v string, n int) string {
	v = strings.TrimSpace(v)
	if r := []rune(v); len(r) > n {
		return string(r[:n])
	}
	return v
}
