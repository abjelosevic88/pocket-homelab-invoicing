package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- Pocket Time: the shared timer ----
//
// Pocket Time is the person's one place for timers (docs/worklog.md part 1 in
// pocket-homelab-time). When Invoicing reaches it, the timer on the Time
// tracking page IS the person's timer there: starting it here starts it in
// Pocket Time (for the client: app_ref "client:<id>"), it shows there while it
// runs, and stopping it in either place stops it in both. One timer at a
// time: starting while another runs (wherever it was started) answers 409
// with the running one, and the page offers "Stop it and start this"
// (switch). A timer started elsewhere is shown here too, with Stop.
//
// The finished time comes back from Pocket Time through the worklog
// (worklog.go, deliver: true), so it is not saved here on stop. What Pocket
// Time does not keep (the free-text project, a rate override) waits in
// shared_timers until it does.
//
// Chosen per person under Settings → Pocket Time, like Pocket Documents:
// core (a Pocket Core app link, time:read + time:write), direct (a URL and
// the person's ptm_ API token) or off. Not connected (off, no Core, the link
// switched off), the timer works here on its own, as before.

var timeScopes = []string{"time:read", "time:write"}

// timeConn is how Invoicing reaches one person's Pocket Time.
type timeConn struct {
	base, token, via string
	http             *http.Client
}

// timeOff says Pocket Time is not connected for the person: the timer here works on its own.
type timeOff struct{ msg string }

func (e *timeOff) Error() string { return e.msg }

func isTimeOff(err error) bool {
	var off *timeOff
	return errors.As(err, &off)
}

// timeMode is the person's Pocket Time connection mode in effect.
func (s *Server) timeMode(st store.Settings) string {
	return connMode(st.TimeMode, st.TimeToken, s.core.Configured())
}

func (s *Server) timeDirectBase(ctx context.Context, st store.Settings) string {
	return strings.TrimRight(firstNonEmpty(st.TimeURL, s.resolveSibling(ctx, "time")), "/")
}

var timeHTTP = &http.Client{Timeout: 10 * time.Second}

// timeFor connects to the person's Pocket Time following their mode. A
// *timeOff error means "not connected" (use the timer here); any other error
// means it should be there but could not be reached.
func (s *Server) timeFor(ctx context.Context, uid int64) (*timeConn, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	switch s.timeMode(st) {
	case modeOff:
		return nil, &timeOff{"Pocket Time is switched off (Settings → Pocket Time)"}
	case modeDirect:
		if st.TimeToken == "" {
			return nil, &timeOff{"Pocket Time is set to a direct connection but has no API token"}
		}
		base := s.timeDirectBase(ctx, st)
		if base == "" {
			return nil, &timeOff{"set the Pocket Time URL in Settings → Pocket Time"}
		}
		return &timeConn{base: base, token: st.TimeToken, via: modeDirect, http: timeHTTP}, nil
	}
	if !s.core.Configured() {
		return nil, &timeOff{"there is no Pocket Core on this box"}
	}
	u, err := s.store.GetUser(ctx, uid)
	if err != nil {
		return nil, &timeOff{"no account to act for"}
	}
	tok, err := s.core.ActAs(ctx, u.Email, "time", timeScopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			return nil, &timeOff{firstNonEmpty(linkReasonFor("Pocket Time", le.Reason), le.Reason)}
		}
		if errors.Is(err, core.ErrNotConfigured) {
			return nil, &timeOff{"there is no Pocket Core on this box"}
		}
		return nil, fmt.Errorf("Pocket Core did not answer, so the timer in Pocket Time can't be reached: %w", err)
	}
	if tok.URL == "" || tok.AccessToken == "" {
		return nil, errors.New("Pocket Core gave no address or token for Pocket Time")
	}
	return &timeConn{base: strings.TrimRight(tok.URL, "/"), token: tok.AccessToken, via: modeCore, http: timeHTTP}, nil
}

// timeStatus says, without issuing a token, how the person reaches Pocket
// Time: the mode in effect, "core" / "direct" / "" (not connected) and why not.
func (s *Server) timeStatus(ctx context.Context, u *store.User, st store.Settings) (mode, via, detail string) {
	mode = s.timeMode(st)
	switch mode {
	case modeOff:
		return mode, "", "switched off"
	case modeDirect:
		if st.TimeToken == "" {
			return mode, "", "add an API token"
		}
		if s.timeDirectBase(ctx, st) == "" {
			return mode, "", "add the URL"
		}
		return mode, modeDirect, ""
	}
	if u == nil {
		return mode, "", "nobody to act for"
	}
	if !s.core.Configured() {
		return mode, "", "there is no Pocket Core on this box"
	}
	on, reason := s.core.LinkOn(ctx, u.Email, "time", timeScopes...)
	if on {
		return mode, modeCore, ""
	}
	return mode, "", firstNonEmpty(linkReasonFor("Pocket Time", reason), reason)
}

// call sends one request to Pocket Time and decodes the answer into out (also
// for 409, which carries the running timer). Other refusals are errors in
// plain words.
func (tc *timeConn) call(ctx context.Context, method, path string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, tc.base+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tc.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := tc.http.Do(req)
	if err != nil {
		return 0, errors.New("Pocket Time did not answer")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return res.StatusCode, errors.New("Pocket Time did not accept the token (check Settings → Pocket Time)")
	case res.StatusCode == http.StatusForbidden:
		return res.StatusCode, errors.New("Pocket Time did not allow it: switch the Invoicing → Pocket Time link on in Pocket Core → Links")
	case res.StatusCode == http.StatusNotFound:
		return res.StatusCode, errors.New("this Pocket Time version has no shared timer yet (update it)")
	case res.StatusCode >= 300 && res.StatusCode != http.StatusConflict:
		var e apiError
		_ = json.Unmarshal(raw, &e)
		if e.Error != "" {
			return res.StatusCode, errors.New("Pocket Time: " + e.Error)
		}
		return res.StatusCode, fmt.Errorf("Pocket Time answered HTTP %d", res.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return res.StatusCode, fmt.Errorf("Pocket Time sent an answer this version does not understand: %w", err)
		}
	}
	return res.StatusCode, nil
}

// timeEntry is the part of Pocket Time's entry Invoicing uses.
type timeEntry struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	StartedAt   string `json:"started_at"`
	Billable    bool   `json:"billable"`
	Source      string `json:"source"`
	App         string `json:"app"`
	AppRef      string `json:"app_ref"`
	AppLabel    string `json:"app_label"`
	ProjectName string `json:"project_name"`
}

// timerView is the running timer as the pages show it: a time entry, plus
// where it runs. Shared = it runs in Pocket Time (TimeID is its id there);
// Ours = it is for a client here; StartedIn = the app it was started in.
type timerView struct {
	*store.TimeEntry
	Shared    bool   `json:"shared"`
	TimeID    int64  `json:"time_id,omitempty"`
	Ours      bool   `json:"ours"`
	StartedIn string `json:"started_in,omitempty"`
}

// appTitle names the app a timer was started in.
func appTitle(slug string) string {
	switch slug {
	case "", "time", "manual", "import":
		return "Pocket Time"
	case "invoicing":
		return "Pocket Invoicing"
	}
	return "Pocket " + strings.ToUpper(slug[:1]) + slug[1:]
}

// fromTime shows a timer of Pocket Time here.
func (s *Server) fromTime(ctx context.Context, uid int64, e *timeEntry) *timerView {
	if e == nil || e.ID == 0 {
		return nil
	}
	v := &timerView{TimeEntry: &store.TimeEntry{StartedAt: e.StartedAt, Billable: e.Billable, Source: "time"}, Shared: true, TimeID: e.ID,
		StartedIn: appTitle(firstNonEmpty(e.Source, e.App))}
	if e.App == "invoicing" {
		if cid := parseClientRef(e.AppRef); cid > 0 {
			if c, err := s.store.GetClient(ctx, uid, cid); err == nil {
				v.ClientID, v.ClientName, v.Ours = cid, c.Name, true
			}
		}
	}
	if !v.Ours {
		v.Description = firstNonEmpty(e.Title, e.ProjectName, e.AppLabel, "a timer")
		return v
	}
	if e.Title != e.AppLabel && e.Title != v.ClientName {
		v.Description = e.Title
	}
	v.Project = e.ProjectName
	if k := s.store.GetSharedTimer(ctx, uid, e.ID); k != nil && k.Project != "" {
		v.Project = k.Project
	}
	return v
}

func (v *timerView) name() string {
	if v == nil {
		return "a timer"
	}
	return firstNonEmpty(strings.Join(nonEmpty(v.ClientName, firstNonEmpty(v.Description, v.Project)), " · "), "a timer")
}

// currentTimer is the person's running timer: one running here (from before
// Pocket Time was connected), else Pocket Time's. timeErr says why Pocket Time
// could not be asked; shared says the timer here is the one in Pocket Time.
func (s *Server) currentTimer(ctx context.Context, uid int64) (v *timerView, shared bool, timeErr string) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if local, err := s.store.RunningTimer(ctx, uid); err == nil && local != nil {
		v = &timerView{TimeEntry: local, Ours: true}
	}
	tc, err := s.timeFor(ctx, uid)
	if err != nil {
		if isTimeOff(err) {
			return v, false, ""
		}
		return v, false, err.Error()
	}
	if v != nil {
		return v, true, ""
	}
	var res struct {
		Running *timeEntry `json:"running"`
	}
	if _, err := tc.call(ctx, http.MethodGet, "/api/v1/timer", nil, &res); err != nil {
		return nil, true, err.Error()
	}
	return s.fromTime(ctx, uid, res.Running), true, ""
}

// handleTestTime checks the Pocket Time connection (the saved one, or a URL + token being typed).
func (s *Server) handleTestTime(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	_ = decode(r, &in)
	uid := uidOf(r)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var tc *timeConn
	if strings.TrimSpace(in.Token) != "" {
		st, _ := s.store.GetSettings(ctx, uid)
		st.TimeURL = firstNonEmpty(strings.TrimRight(strings.TrimSpace(in.URL), "/"), st.TimeURL)
		base := s.timeDirectBase(ctx, st)
		if base == "" {
			writeErr(w, http.StatusBadRequest, "the URL is required")
			return
		}
		tc = &timeConn{base: base, token: strings.TrimSpace(in.Token), via: modeDirect, http: timeHTTP}
	} else {
		var err error
		if tc, err = s.timeFor(ctx, uid); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var res struct {
		Running *timeEntry `json:"running"`
	}
	if _, err := tc.call(ctx, http.MethodGet, "/api/v1/timer", nil, &res); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	running := ""
	if v := s.fromTime(ctx, uid, res.Running); v != nil {
		running = v.name()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "via": tc.via, "running": running})
}
