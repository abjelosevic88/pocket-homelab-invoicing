package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// fakeTimeCore is Pocket Core for the Pocket Time tests: link tokens other
// apps act here with (introspection), link tokens Invoicing gets for Pocket
// Time, the link state, registration and mode reports.
type fakeTimeCore struct {
	srv     *httptest.Server
	timeURL string
	mu      sync.Mutex
	refuse  map[string]string // email → reason for tokens to Pocket Time
	asked   []string
}

// link tokens acting HERE: token → acting app, scope
var timeLinkTokens = map[string][2]string{
	"pcx_wl_read":      {"time", "worklog:read"},
	"pcx_wl_write":     {"time", "worklog:write"},
	"pcx_wl_projects":  {"projects", "worklog:write"},
	"pcx_wl_invoices":  {"day", "invoices:read"},
	"pcx_wl_wrong_aud": {"time", "worklog:read"},
}

func newFakeTimeCore(t *testing.T, timeURL string) *fakeTimeCore {
	f := &fakeTimeCore{timeURL: timeURL, refuse: map[string]string{}}
	member := map[string]any{"id": 7, "email": "member@example.com", "name": "Member", "role": "member"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if l, ok := timeLinkTokens[in.Token]; ok {
				aud := "invoicing"
				if in.Token == "pcx_wl_wrong_aud" {
					aud = "projects"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "scope": l[1], "audience": aud,
					"user": member, "app": map[string]any{"id": 9, "slug": l[0], "name": "Pocket " + l[0]}})
				return
			}
			if in.Token == "pcd_member-phone" {
				_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "device", "user": member})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
		case "/api/v1/links/token":
			var in struct {
				User, App string
				Scopes    []string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.asked = append(f.asked, in.User+" "+in.App+" "+strings.Join(in.Scopes, ","))
			if reason, ok := f.refuse[in.User]; ok {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "refused", "reason": reason})
				return
			}
			if in.App != "time" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "refused", "reason": "off_admin"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_to_time_" + strings.Split(in.User, "@")[0], "expires_in": 300, "scope": strings.Join(in.Scopes, " "), "app": in.App, "url": f.timeURL})
		case "/api/v1/links/status":
			state, on := "on", true
			if reason, ok := f.refuse[r.URL.Query().Get("user")]; ok {
				state, on = reason, false
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"app": r.URL.Query().Get("app"), "scope": "time:read", "state": state, "on": on}, {"app": r.URL.Query().Get("app"), "scope": "time:write", "state": state, "on": on}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// fakeTime is Pocket Time's shared timer: one running entry per person.
type fakeTime struct {
	srv     *httptest.Server
	mu      sync.Mutex
	nextID  int64
	running map[string]any // nil = none
	starts  []map[string]any
	stops   []map[string]any
	auth    []string
}

func newFakeTime(t *testing.T) *fakeTime {
	f := &fakeTime{nextID: 100}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/timer":
			_ = json.NewEncoder(w).Encode(map[string]any{"running": f.running, "now": time.Now().Format(time.RFC3339)})
		case "POST /api/v1/timer/start":
			f.starts = append(f.starts, in)
			if f.running != nil && in["switch"] != true {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "a timer is already running", "running": f.running})
				return
			}
			stopped := f.running
			f.nextID++
			title, _ := in["title"].(string)
			if title == "" {
				title, _ = in["app_label"].(string)
			}
			f.running = map[string]any{"id": f.nextID, "title": title, "started_at": time.Now().Format(time.RFC3339), "running": true, "billable": in["billable"],
				"source": "invoicing", "app": "invoicing", "app_ref": in["app_ref"], "app_label": in["app_label"], "app_url": in["app_url"], "deliver": in["deliver"]}
			_ = json.NewEncoder(w).Encode(map[string]any{"running": f.running, "stopped": stopped})
		case "POST /api/v1/timer/stop":
			f.stops = append(f.stops, in)
			if f.running == nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"running": nil, "stopped": nil})
				return
			}
			if id, ok := in["only_if"].(float64); ok && int64(id) != f.running["id"].(int64) {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "a different timer is running now", "running": f.running})
				return
			}
			stopped := f.running
			f.running = nil
			_ = json.NewEncoder(w).Encode(map[string]any{"running": nil, "stopped": stopped})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTime) set(running map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = running
}

func (f *fakeTime) runningID() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running == nil {
		return 0
	}
	return f.running["id"].(int64)
}

func (f *fakeTime) lastStart() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts[len(f.starts)-1]
}

// timeTestServer: Invoicing with a fake Core and Pocket Time, the owner (an
// admin with their own business) and the member (who uses Pocket Time).
func timeTestServer(t *testing.T) (*Server, *store.Store, *fakeTimeCore, *fakeTime, *store.User, *store.User) {
	t.Helper()
	ft := newFakeTime(t)
	fc := newFakeTimeCore(t, ft.srv.URL)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	return s, st, fc, ft, owner, member
}

// tokenCaller calls the API with any bearer token.
func tokenCaller(t *testing.T, s *Server) func(tok, method, path string, body any) (int, map[string]any) {
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return func(tok, method, path string, body any) (int, map[string]any) {
		t.Helper()
		var rd *strings.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req, _ := http.NewRequest(method, ts.URL+"/api/v1"+path, rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
}

func mkClient(t *testing.T, st *store.Store, uid int64, name string) *store.Client {
	t.Helper()
	c := &store.Client{Name: name, Currency: "EUR"}
	if err := st.CreateClient(context.Background(), uid, c); err != nil {
		t.Fatal(err)
	}
	return c
}

func mkTime(t *testing.T, st *store.Store, uid, cid int64, desc, start string, minutes int) *store.TimeEntry {
	t.Helper()
	s, _ := time.Parse(time.RFC3339, start)
	end := s.Add(time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339)
	e := &store.TimeEntry{ClientID: cid, Description: desc, StartedAt: s.UTC().Format(time.RFC3339), EndedAt: &end, DurationMinutes: minutes, Billable: true}
	if err := st.SaveTimeEntry(context.Background(), uid, e); err != nil {
		t.Fatal(err)
	}
	return e
}

func entriesOf(out map[string]any) []map[string]any {
	var list []map[string]any
	raw, _ := json.Marshal(out["entries"])
	_ = json.Unmarshal(raw, &list)
	return list
}

// The worklog: Pocket Time, acting for the member, sees exactly the member's
// time and clients with worklog:read, writes time with worklog:write
// (idempotent on external_id), and can never touch invoiced time, the
// member's own entries, another app's entries or anyone else's.
func TestWorklogLink(t *testing.T) {
	s, st, _, _, owner, member := timeTestServer(t)
	ctx := context.Background()
	call := tokenCaller(t, s)
	oc := mkClient(t, st, owner.ID, "Owner Client")
	ownerEntry := mkTime(t, st, owner.ID, oc.ID, "Owner work", "2026-10-20T08:00:00Z", 60)
	mc := mkClient(t, st, member.ID, "Harbor Ltd")
	own := mkTime(t, st, member.ID, mc.ID, "Design review", "2026-10-20T09:00:00Z", 90)
	// make sure the member's account is the one Core's identity maps to
	if code, out := call("pcd_member-phone", "GET", "/auth/me", nil); code != 200 || !strings.Contains(fmt.Sprint(out), "member@example.com") {
		t.Fatalf("member: %d %v", code, out)
	}

	// read: the member's entries and clients only
	code, out := call("pcx_wl_read", "GET", "/worklog?from=2026-10-01&to=2026-10-31", nil)
	list := entriesOf(out)
	if code != 200 || len(list) != 1 || list[0]["id"] != fmt.Sprint(own.ID) || list[0]["ref"] != fmt.Sprintf("client:%d", mc.ID) ||
		list[0]["label"] != "Harbor Ltd" || list[0]["title"] != "Design review" || list[0]["duration_s"] != float64(5400) || list[0]["source"] != "app" ||
		list[0]["url"] != fmt.Sprintf("/clients/%d", mc.ID) || list[0]["date"] != "2026-10-20" {
		t.Fatalf("worklog: %d %v", code, out)
	}
	if strings.Contains(fmt.Sprint(out), "Owner") {
		t.Fatal("the owner's time leaked through the member's link")
	}
	if code, out := call("pcx_wl_read", "GET", "/worklog?from=2026-11-01&to=2026-11-30", nil); code != 200 || len(entriesOf(out)) != 0 {
		t.Fatalf("other period: %d %v", code, out)
	}
	if code, _ := call("pcx_wl_read", "GET", "/worklog?from=2026-10-31&to=2026-10-01", nil); code != 400 {
		t.Fatalf("backwards period: %d", code)
	}
	code, out = call("pcx_wl_read", "GET", "/worklog/items", nil)
	if code != 200 || !strings.Contains(fmt.Sprint(out), fmt.Sprintf("client:%d", mc.ID)) || strings.Contains(fmt.Sprint(out), "Owner") || !strings.Contains(fmt.Sprint(out), "1 h 30 min not invoiced yet") {
		t.Fatalf("items: %d %v", code, out)
	}
	if code, out := call("pcx_wl_read", "GET", "/worklog/items?q=zzz", nil); code != 200 || len(out["items"].([]any)) != 0 {
		t.Fatalf("items search: %d %v", code, out)
	}

	// scopes: read cannot write, write cannot read, other links cannot do either
	body := map[string]any{"ref": fmt.Sprintf("client:%d", mc.ID), "title": "Fix the login redirect", "started_at": "2026-10-28T09:10:00+01:00",
		"ended_at": "2026-10-28T10:25:00+01:00", "date": "2026-10-28", "duration_s": 4500, "billable": true, "project": "Mobile app", "external_id": "time:812"}
	for _, c := range []struct{ tok, m, p string }{
		{"pcx_wl_read", "POST", "/worklog"}, {"pcx_wl_read", "DELETE", "/worklog/1"},
		{"pcx_wl_write", "GET", "/worklog"}, {"pcx_wl_write", "GET", "/worklog/items"},
		{"pcx_wl_invoices", "GET", "/worklog"}, {"pcx_wl_invoices", "POST", "/worklog"},
		{"pcx_wl_write", "GET", "/time"}, {"pcx_wl_write", "POST", "/time"}, {"pcx_wl_write", "PUT", fmt.Sprintf("/time/%d", own.ID)},
		{"pcx_wl_write", "POST", "/time/start"}, {"pcx_wl_read", "GET", "/clients"},
	} {
		if code, _ := call(c.tok, c.m, c.p, body); code != 403 {
			t.Fatalf("%s %s with %s: %d, want 403", c.m, c.p, c.tok, code)
		}
	}
	if code, _ := call("pcx_wl_wrong_aud", "GET", "/worklog", nil); code != 401 {
		t.Fatalf("a token for another app: %d, want 401", code)
	}

	// write: unknown or someone else's client → 400 in plain words
	for _, ref := range []string{"issue:ATH-1", "", fmt.Sprintf("client:%d", oc.ID), "client:99999"} {
		b := map[string]any{"ref": ref, "duration_s": 600, "started_at": "2026-10-28T09:00:00Z", "external_id": "time:1"}
		if code, out := call("pcx_wl_write", "POST", "/worklog", b); code != 400 || out["error"] == "" {
			t.Fatalf("ref %q: %d %v", ref, code, out)
		}
	}
	if code, _ := call("pcx_wl_write", "POST", "/worklog", map[string]any{"ref": fmt.Sprintf("client:%d", mc.ID), "duration_s": 0, "date": "2026-10-28"}); code != 400 {
		t.Fatalf("no length: %d", code)
	}

	// write, then the same external_id again: one entry, updated
	code, out = call("pcx_wl_write", "POST", "/worklog", body)
	if code != 200 || out["id"] == "" || out["url"] != fmt.Sprintf("/clients/%d", mc.ID) {
		t.Fatalf("log: %d %v", code, out)
	}
	id := out["id"].(string)
	body["duration_s"], body["title"] = 3600, "Fix the login redirect (2)"
	if code, out := call("pcx_wl_write", "POST", "/worklog", body); code != 200 || out["id"] != id {
		t.Fatalf("again: %d %v, want the same id %s", code, out, id)
	}
	all, _ := st.ListTimeEntries(ctx, member.ID, store.TimeFilter{})
	if len(all) != 2 {
		t.Fatalf("member has %d entries, want 2 (own + one from Pocket Time)", len(all))
	}
	var got *store.TimeEntry
	for i := range all {
		if fmt.Sprint(all[i].ID) == id {
			got = &all[i]
		}
	}
	if got == nil || got.Source != "time" || got.ExternalID != "time:812" || got.DurationMinutes != 60 || got.Description != "Fix the login redirect (2)" ||
		got.Project != "Mobile app" || !got.Billable || got.InvoiceID != nil || got.StartedAt != "2026-10-28T08:10:00Z" || got.ClientID != mc.ID {
		t.Fatalf("logged entry: %+v", got)
	}
	// Pocket Time sees it marked as its own (so it skips it)
	_, out = call("pcx_wl_read", "GET", "/worklog?from=2026-10-28&to=2026-10-28", nil)
	if l := entriesOf(out); len(l) != 1 || l[0]["source"] != "time" || l[0]["external_id"] != "time:812" || l[0]["label"] != "Harbor Ltd · Mobile app" {
		t.Fatalf("worklog after writing: %v", out)
	}
	// rounding like the timer: 20 minutes → 30 with 15-minute blocks
	b2 := map[string]any{"ref": fmt.Sprintf("client:%d", mc.ID), "date": "2026-10-29", "duration_s": 1200, "external_id": "time:813", "billable": false}
	code, out = call("pcx_wl_write", "POST", "/worklog", b2)
	if code != 200 {
		t.Fatalf("second: %d %v", code, out)
	}
	id2 := out["id"].(string)
	var n2 int64
	fmt.Sscan(id2, &n2)
	e2, _ := st.GetTimeEntry(ctx, member.ID, n2)
	if e2.DurationMinutes != 30 || e2.Billable || !strings.HasPrefix(e2.StartedAt, "2026-10-29") {
		t.Fatalf("second entry: %+v", e2)
	}

	// another app's link can't change or remove what Pocket Time wrote; nobody can remove the member's own entries
	if code, _ := call("pcx_wl_projects", "POST", "/worklog", body); code != 409 {
		t.Fatalf("other app rewriting Time's entry: %d, want 409", code)
	}
	for _, c := range []struct{ tok, id string }{{"pcx_wl_projects", id}, {"pcx_wl_write", fmt.Sprint(own.ID)}, {"pcx_wl_write", fmt.Sprint(ownerEntry.ID)}, {"pcx_wl_write", "abc"}} {
		if code, _ := call(c.tok, "DELETE", "/worklog/"+c.id, nil); code != 404 {
			t.Fatalf("DELETE %s with %s: %d, want 404", c.id, c.tok, code)
		}
	}

	// invoiced: never changed or removed from there
	var nid int64
	fmt.Sscan(id, &nid)
	inv := &store.Invoice{ClientID: mc.ID, Number: "INV-T-1", Status: "draft", IssueDate: "2026-10-30", DueDate: "2026-11-13", Currency: "EUR"}
	if err := st.CreateInvoice(ctx, member.ID, inv); err != nil {
		t.Fatal(err)
	}
	if err := st.LinkTimeEntries(ctx, member.ID, inv.ID, []int64{nid}); err != nil {
		t.Fatal(err)
	}
	if code, out := call("pcx_wl_write", "POST", "/worklog", body); code != 409 || !strings.Contains(out["error"].(string), "INV-T-1") {
		t.Fatalf("rewriting invoiced time: %d %v", code, out)
	}
	if code, _ := call("pcx_wl_write", "DELETE", "/worklog/"+id, nil); code != 409 {
		t.Fatalf("removing invoiced time: %d, want 409", code)
	}
	// not invoiced: removed
	if code, _ := call("pcx_wl_write", "DELETE", "/worklog/"+id2, nil); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if _, err := st.GetTimeEntry(ctx, member.ID, n2); err == nil {
		t.Fatal("the entry is still there")
	}
	// the owner's time is untouched
	if e, err := st.GetTimeEntry(ctx, owner.ID, ownerEntry.ID); err != nil || e.DurationMinutes != 60 {
		t.Fatalf("owner's entry: %+v %v", e, err)
	}
}

// Through the person's own API token (Pocket Time connected directly) the
// worklog works the same; /time never lets anyone set source or external_id.
func TestWorklogDirectAndReadOnlyFields(t *testing.T) {
	s, st, _, _, _, member := timeTestServer(t)
	ctx := context.Background()
	mc := mkClient(t, st, member.ID, "Harbor Ltd")
	call := apiCaller(t, s, st, member.ID)
	code, out, raw := call("POST", "/worklog", map[string]any{"ref": fmt.Sprintf("client:%d", mc.ID), "date": "2026-10-28", "duration_s": 3600, "external_id": "time:5"})
	if code != 200 {
		t.Fatalf("direct log: %d %s", code, raw)
	}
	var id int64
	fmt.Sscan(out["id"].(string), &id)
	if e, _ := st.GetTimeEntry(ctx, member.ID, id); e.Source != "time" || e.ExternalID != "time:5" {
		t.Fatalf("direct entry: %+v", e)
	}
	if code, _, _ := call("DELETE", fmt.Sprintf("/worklog/%d", id), nil); code != 200 {
		t.Fatalf("direct delete: %d", code)
	}
	code, out, _ = call("POST", "/time", map[string]any{"client_id": mc.ID, "started_at": "2026-10-28", "duration_minutes": 30, "source": "time", "external_id": "time:9"})
	if code != 200 || out["source"] != "app" || out["external_id"] != "" {
		t.Fatalf("manual entry took source/external_id: %d %v", code, out)
	}
	// and a manual entry can't be removed through the worklog
	if code, _, _ := call("DELETE", fmt.Sprintf("/worklog/%v", out["id"]), nil); code != 404 {
		t.Fatalf("worklog delete of a manual entry: %d", code)
	}
}

// The shared timer: with Pocket Time reachable through Core, the timer here is
// the one there — started for the client, shown wherever it was started, one
// at a time (409 + switch), stopped with only_if; nothing saved here until
// Pocket Time sends the finished time back, which keeps the project and rate.
func TestSharedTimer(t *testing.T) {
	s, st, fc, ft, _, member := timeTestServer(t)
	ctx := context.Background()
	mc := mkClient(t, st, member.ID, "Harbor Ltd")
	call := apiCaller(t, s, st, member.ID)

	code, out, raw := call("POST", "/time/start", map[string]any{"client_id": mc.ID, "project": "Mobile app", "description": "Login bug", "rate": 80})
	if code != 201 || out["shared"] != true || out["ours"] != true || out["client_name"] != "Harbor Ltd" || out["project"] != "Mobile app" || out["description"] != "Login bug" {
		t.Fatalf("start: %d %s", code, raw)
	}
	tid := int64(out["time_id"].(float64))
	start := ft.lastStart()
	if start["app_ref"] != fmt.Sprintf("client:%d", mc.ID) || start["app_label"] != "Harbor Ltd" || start["app_url"] != fmt.Sprintf("/clients/%d", mc.ID) ||
		start["deliver"] != true || start["switch"] != false || start["title"] != "Login bug" || start["billable"] != true || start["app"] != "invoicing" || start["source"] != "invoicing" {
		t.Fatalf("Pocket Time got %v", start)
	}
	if got := ft.auth[len(ft.auth)-1]; got != "Bearer pcx_to_time_member" {
		t.Fatalf("Pocket Time saw %q", got)
	}
	if got := fc.asked[len(fc.asked)-1]; got != "member@example.com time time:read,time:write" {
		t.Fatalf("Core asked %q", got)
	}
	if all, _ := st.ListTimeEntries(ctx, member.ID, store.TimeFilter{}); len(all) != 0 {
		t.Fatalf("a shared timer saved %d entries here", len(all))
	}
	code, out, raw = call("GET", "/time/running", nil)
	run, _ := out["running"].(map[string]any)
	if code != 200 || out["shared"] != true || run == nil || run["ours"] != true || run["project"] != "Mobile app" || int64(run["time_id"].(float64)) != tid {
		t.Fatalf("running: %d %s", code, raw)
	}
	if code, out, _ := call("GET", "/dashboard", nil); code != 200 || out["running_timer"] == nil {
		t.Fatalf("dashboard timer: %d %v", code, out["running_timer"])
	}

	// started somewhere else: shown here, and starting offers to switch
	ft.set(map[string]any{"id": int64(500), "title": "Wonderwall", "started_at": time.Now().Format(time.RFC3339), "source": "guitar", "app": "guitar", "app_ref": "tab:42", "app_label": "Wonderwall"})
	_, out, _ = call("GET", "/time/running", nil)
	run, _ = out["running"].(map[string]any)
	if run == nil || run["ours"] != false || run["started_in"] != "Pocket Guitar" || run["description"] != "Wonderwall" || run["client_name"] != "" {
		t.Fatalf("running elsewhere: %v", out)
	}
	code, out, _ = call("POST", "/time/start", map[string]any{"client_id": mc.ID})
	if code != 409 || !strings.Contains(out["error"].(string), "Wonderwall") || !strings.Contains(out["error"].(string), "Pocket Guitar") || out["running"] == nil {
		t.Fatalf("start while another runs: %d %v", code, out)
	}
	if ft.runningID() != 500 {
		t.Fatal("the other timer was stopped without asking")
	}
	code, out, _ = call("POST", "/time/start", map[string]any{"client_id": mc.ID, "switch": true})
	if code != 201 || ft.runningID() == 500 {
		t.Fatalf("switch: %d %v", code, out)
	}
	tid = int64(out["time_id"].(float64))
	if out["description"] != "" {
		t.Fatalf("an untitled timer is described as %v", out["description"])
	}

	// stop: only the one shown
	if code, out, _ := call("POST", "/time/stop", map[string]any{"time_id": 12345}); code != 409 || ft.runningID() != tid || out["running"] == nil {
		t.Fatalf("stop of a timer no longer running: %d %v", code, out)
	}
	code, out, _ = call("POST", "/time/stop", map[string]any{"time_id": tid})
	if code != 200 || ft.runningID() != 0 || out["stopped_shared"] == nil {
		t.Fatalf("stop: %d %v", code, out)
	}
	if all, _ := st.ListTimeEntries(ctx, member.ID, store.TimeFilter{}); len(all) != 0 {
		t.Fatalf("stopping saved %d entries here (Pocket Time sends them)", len(all))
	}

	// Pocket Time sends the first timer back: the project and rate kept here are used
	first := int64(101)
	call2 := tokenCaller(t, s)
	code, res := call2("pcx_wl_write", "POST", "/worklog", map[string]any{"ref": fmt.Sprintf("client:%d", mc.ID), "title": "Login bug", "started_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"duration_s": 2700, "billable": true, "project": "", "external_id": fmt.Sprintf("time:%d", first)})
	if code != 200 {
		t.Fatalf("delivery: %d %v", code, res)
	}
	var eid int64
	fmt.Sscan(res["id"].(string), &eid)
	e, _ := st.GetTimeEntry(ctx, member.ID, eid)
	if e.Project != "Mobile app" || e.Rate == nil || *e.Rate != 80 || e.DurationMinutes != 45 || e.Description != "Login bug" {
		t.Fatalf("delivered entry: %+v", e)
	}
}

// Not connected (off, the link switched off in Core), the timer works here on
// its own, as before; a timer from before stays visible and stoppable.
func TestTimerWithoutPocketTime(t *testing.T) {
	s, st, fc, ft, _, member := timeTestServer(t)
	ctx := context.Background()
	mc := mkClient(t, st, member.ID, "Harbor Ltd")
	call := apiCaller(t, s, st, member.ID)

	fc.mu.Lock()
	fc.refuse["member@example.com"] = "off_admin"
	fc.mu.Unlock()
	code, out, raw := call("POST", "/time/start", map[string]any{"client_id": mc.ID, "description": "Local"})
	if code != 201 || out["id"] == float64(0) || out["shared"] != nil {
		t.Fatalf("local start with the link off: %d %s", code, raw)
	}
	ft.mu.Lock()
	n := len(ft.starts)
	ft.mu.Unlock()
	if n != 0 {
		t.Fatal("Pocket Time was called with the link off")
	}
	_, out, _ = call("GET", "/time/running", nil)
	if run, _ := out["running"].(map[string]any); run == nil || run["shared"] != false || run["description"] != "Local" || out["shared"] != false {
		t.Fatalf("local running: %v", out)
	}
	mst, _ := st.GetSettings(ctx, member.ID)
	if m := s.publicSettings(ctx, member, mst); m["time_mode"] != modeCore || m["time_via"] != "" || !strings.Contains(m["time_detail"].(string), "switched off in Pocket Core") {
		t.Fatalf("status with the link off: %v %v %v", m["time_mode"], m["time_via"], m["time_detail"])
	}

	// the link comes on: the old local timer is still shown and stops here
	fc.mu.Lock()
	delete(fc.refuse, "member@example.com")
	fc.mu.Unlock()
	_, out, _ = call("GET", "/time/running", nil)
	if run, _ := out["running"].(map[string]any); run == nil || run["shared"] != false || out["shared"] != true {
		t.Fatalf("local leftover once connected: %v", out)
	}
	code, out, _ = call("POST", "/time/stop", nil)
	if code != 200 || len(out["stopped"].([]any)) != 1 {
		t.Fatalf("stop local: %d %v", code, out)
	}
	ft.mu.Lock()
	n = len(ft.stops)
	ft.mu.Unlock()
	if n != 0 {
		t.Fatal("stopping the local timer also stopped Pocket Time's")
	}

	// off: local, and Core is not even asked
	code, _, _ = call("PUT", "/settings", map[string]any{"time_mode": "off"})
	if code != 200 {
		t.Fatalf("switch off: %d", code)
	}
	fc.mu.Lock()
	asked := len(fc.asked)
	fc.mu.Unlock()
	if code, out, _ := call("POST", "/time/start", map[string]any{"client_id": mc.ID}); code != 201 || out["shared"] != nil {
		t.Fatalf("start with Pocket Time off: %d %v", code, out)
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.asked) != asked {
		t.Fatal("Core was asked for a Pocket Time token while it is off")
	}
}

// Direct: the person's own URL and ptm_ token, never shown back.
func TestTimerDirect(t *testing.T) {
	s, st, fc, ft, _, member := timeTestServer(t)
	mc := mkClient(t, st, member.ID, "Harbor Ltd")
	call := apiCaller(t, s, st, member.ID)
	code, out, _ := call("PUT", "/settings", map[string]any{"time_mode": "direct", "time_url": ft.srv.URL + "/", "time_token": "ptm_secret"})
	if code != 200 || out["time_token"] != "" || out["time_token_set"] != true || out["time_via"] != modeDirect || out["time_url"] != ft.srv.URL {
		t.Fatalf("direct settings: %d %v", code, out)
	}
	fc.mu.Lock()
	asked := len(fc.asked)
	fc.mu.Unlock()
	if code, out, _ := call("POST", "/time/start", map[string]any{"client_id": mc.ID}); code != 201 || out["shared"] != true {
		t.Fatalf("direct start: %d %v", code, out)
	}
	if st := ft.lastStart(); st["app"] != "invoicing" || st["source"] != "invoicing" {
		t.Fatalf("direct start without app/source: %v", st)
	}
	if got := ft.auth[len(ft.auth)-1]; got != "Bearer ptm_secret" {
		t.Fatalf("Pocket Time saw %q", got)
	}
	if code, out, _ := call("POST", "/settings/test-time", map[string]any{}); code != 200 || out["via"] != modeDirect || out["running"] != "Harbor Ltd" {
		t.Fatalf("test: %d %v", code, out)
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.asked) != asked {
		t.Fatal("direct asked Core for a token")
	}
}
