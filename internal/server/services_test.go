package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/config"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// fakePaperless answers the document list (Ping) and records the tokens it saw.
type fakePaperless struct {
	srv  *httptest.Server
	mu   sync.Mutex
	auth []string
}

func newFakePaperless(t *testing.T) *fakePaperless {
	f := &fakePaperless{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.Header().Set("X-Version", "2.15.0")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"count": 3, "results": []}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePaperless) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.auth...)
}

// fakeServiceCore is Pocket Core's service-connection API: a credential per
// person (or a refusal reason), and the link state without a key.
type fakeServiceCore struct {
	srv     *httptest.Server
	mu      sync.Mutex
	creds   map[string]map[string]any // email → credential response
	refuse  map[string]string         // email → reason
	asked   []string                  // "<auth> <user> <type>" per credential request
	statusN int
	modes   map[string]string // "<user> <target>" → last reported mode
	reports int               // PUT /links/modes calls
}

func newFakeServiceCore(t *testing.T, paperlessURL string) *fakeServiceCore {
	f := &fakeServiceCore{
		creds: map[string]map[string]any{
			"owner@example.com":  {"type": "paperless", "url": paperlessURL, "public_url": "https://paperless.example.org", "key": "box-token", "fields": map[string]string{"TOKEN": "box-token"}, "source": "box"},
			"member@example.com": {"type": "paperless", "url": paperlessURL, "public_url": "https://paperless.example.org", "key": "member-token", "fields": map[string]string{"TOKEN": "member-token"}, "source": "personal"},
		},
		refuse: map[string]string{"nobody@example.com": "no_account"},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/services/credential":
			var in struct{ User, Type string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.asked = append(f.asked, r.Header.Get("Authorization")+" "+in.User+" "+in.Type)
			if reason, ok := f.refuse[in.User]; ok {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "paperless: no account", "reason": reason})
				return
			}
			_ = json.NewEncoder(w).Encode(f.creds[in.User])
		case "/api/v1/apps/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"app": map[string]any{"slug": "invoicing"}})
		case "/api/v1/links/modes":
			if r.Method != http.MethodPut || r.Header.Get("Authorization") != "Bearer pca_invoicing" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var in struct {
				Modes []struct{ User, Target, Mode string }
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if f.modes == nil {
				f.modes = map[string]string{}
			}
			for _, m := range in.Modes {
				f.modes[m.User+" "+m.Target] = m.Mode
			}
			f.reports++
			_ = json.NewEncoder(w).Encode(map[string]any{"recorded": len(in.Modes)})
		case "/api/v1/links/status":
			f.statusN++
			if r.URL.Query().Get("app") != "service:paperless" {
				_ = json.NewEncoder(w).Encode([]any{})
				return
			}
			user := r.URL.Query().Get("user")
			if reason, ok := f.refuse[user]; ok {
				_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "service:paperless", "scope": "account", "state": reason, "on": false}})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "service:paperless", "scope": "account", "state": "on", "on": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServiceCore) credentialCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.asked...)
}

// reported is the last mode Core heard for a person's Paperless, and how many reports came in.
func (f *fakeServiceCore) reported(email string) (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modes[email+" service:paperless"], f.reports
}

// waitReported waits for Core to hear want for the person (reports are sent in the background).
func waitReported(t *testing.T, f *fakeServiceCore, email, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := f.reported(email)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Core heard %q for %s, want %q", got, email, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func serviceTestServer(t *testing.T, coreURL string, mod func(*config.Config)) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.CoreURL = coreURL
	if mod != nil {
		mod(cfg)
	}
	if err := os.WriteFile(dir+"/core.token", []byte("pca_invoicing"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := openTestStore(t, cfg)
	return New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil), st
}

func mkUser(t *testing.T, st *store.Store, email, role string) *store.User {
	t.Helper()
	u := &store.User{Email: email, Name: email, Role: role, PasswordHash: "x"}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

// Paperless through Pocket Core: each person gets the address and their own
// key from Core on every use (the box's key only for the admin), with the
// app's Core token; direct uses the saved URL and token without asking Core;
// off makes no calls at all; a refusal reads as "not connected".
func TestPaperlessConnectionModes(t *testing.T) {
	pl := newFakePaperless(t)
	fc := newFakeServiceCore(t, pl.srv.URL)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx := context.Background()
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	nobody := mkUser(t, st, "nobody@example.com", roleMember)

	// --- core (the default without a saved token, Core being there) ---
	cl, mst, err := s.paperlessConn(ctx, member.ID)
	if err != nil {
		t.Fatalf("member through core: %v", err)
	}
	if ps := cl.Ping(ctx); !ps.OK || ps.DocumentCount != 3 {
		t.Fatalf("ping through core: %+v", ps)
	}
	if got := pl.calls(); len(got) != 1 || got[0] != "Token member-token" {
		t.Fatalf("paperless saw %v, want the member's own key", got)
	}
	if got := fc.credentialCalls(); len(got) != 1 || got[0] != "Bearer pca_invoicing member@example.com paperless" {
		t.Fatalf("core asked %v", got)
	}
	if u := s.paperlessDocURL(mst, 12); u != "https://paperless.example.org/documents/12/details" {
		t.Fatalf("browser link through core = %q, want Core's public URL", u)
	}
	// the next use within the minute reuses the SDK's cached credential
	if _, _, err := s.paperlessConn(ctx, member.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(fc.credentialCalls()); n != 1 {
		t.Fatalf("core asked %d times, want once (cached)", n)
	}
	// a connection test always asks afresh
	if _, _, err := s.paperlessConnect(ctx, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if n := len(fc.credentialCalls()); n != 2 {
		t.Fatalf("core asked %d times, want a fresh ask for the test", n)
	}
	// the admin gets the box's key, and the status line says so
	if _, _, err := s.paperlessConn(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	ost, _ := st.GetSettings(ctx, owner.ID)
	m := s.publicSettings(ctx, owner, ost)
	if m["paperless_mode"] != modeCore || m["paperless_via"] != modeCore || m["paperless_source"] != "box" {
		t.Fatalf("owner status: mode=%v via=%v source=%v", m["paperless_mode"], m["paperless_via"], m["paperless_source"])
	}

	// --- refusal: not connected, with Core's reason ---
	if _, _, err := s.paperlessConn(ctx, nobody.ID); err == nil || !strings.Contains(err.Error(), "no Paperless-ngx account of your own") {
		t.Fatalf("refused person: err = %v", err)
	}
	nst, _ := st.GetSettings(ctx, nobody.ID)
	m = s.publicSettings(ctx, nobody, nst)
	if m["paperless_via"] != "" || !strings.Contains(m["paperless_detail"].(string), "no Paperless-ngx account") {
		t.Fatalf("refused status: via=%v detail=%v", m["paperless_via"], m["paperless_detail"])
	}
	if cl, _ := s.paperlessClient(ctx, nobody.ID); cl != nil {
		t.Fatal("a refused person must have no client")
	}

	// --- direct: a saved token wins by default, Core is not asked ---
	mst, _ = st.GetSettings(ctx, member.ID)
	mst.PaperlessURL, mst.PaperlessToken = pl.srv.URL, "direct-token"
	if err := st.SaveSettings(ctx, member.ID, mst); err != nil {
		t.Fatal(err)
	}
	before := len(fc.credentialCalls())
	cl, _, err = s.paperlessConn(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	cl.Ping(ctx)
	if got := pl.calls(); got[len(got)-1] != "Token direct-token" {
		t.Fatalf("direct used %q", got[len(got)-1])
	}
	if len(fc.credentialCalls()) != before {
		t.Fatal("direct mode asked Core")
	}
	if m := s.publicSettings(ctx, member, mst); m["paperless_mode"] != modeDirect || m["paperless_via"] != modeDirect {
		t.Fatalf("direct status: %v %v", m["paperless_mode"], m["paperless_via"])
	}

	// --- core chosen explicitly beats the saved token ---
	mst.PaperlessMode = modeCore
	_ = st.SaveSettings(ctx, member.ID, mst)
	cl, _, _ = s.paperlessConn(ctx, member.ID)
	cl.Ping(ctx)
	if got := pl.calls(); got[len(got)-1] != "Token member-token" {
		t.Fatalf("explicit core used %q", got[len(got)-1])
	}

	// --- off: no calls anywhere ---
	mst.PaperlessMode = modeOff
	_ = st.SaveSettings(ctx, member.ID, mst)
	coreBefore, plBefore := len(fc.credentialCalls()), len(pl.calls())
	fc.mu.Lock()
	statusBefore := fc.statusN
	fc.mu.Unlock()
	if cl, _, err := s.paperlessConn(ctx, member.ID); cl != nil || err == nil {
		t.Fatal("off must give no client")
	}
	if m := s.publicSettings(ctx, member, mst); m["paperless_mode"] != modeOff || m["paperless_via"] != "" {
		t.Fatalf("off status: %v %v", m["paperless_mode"], m["paperless_via"])
	}
	fc.mu.Lock()
	statusAfter := fc.statusN
	fc.mu.Unlock()
	if len(fc.credentialCalls()) != coreBefore || len(pl.calls()) != plBefore || statusAfter != statusBefore {
		t.Fatal("off mode made calls")
	}
}

// Without Pocket Core the default stays direct, as before.
func TestPaperlessDefaultWithoutCore(t *testing.T) {
	s, st := serviceTestServer(t, "", nil)
	u := mkUser(t, st, "owner@example.com", roleAdmin)
	set, _ := st.GetSettings(context.Background(), u.ID)
	if m := s.publicSettings(context.Background(), u, set); m["paperless_mode"] != modeDirect || m["core_configured"] != false {
		t.Fatalf("no core: mode=%v core=%v", m["paperless_mode"], m["core_configured"])
	}
}

// PAPERLESS_URL / PAPERLESS_TOKEN become the owner's saved connection once;
// after a Disconnect they do not come back on the next start.
func TestPaperlessEnvSeededOnce(t *testing.T) {
	pl := newFakePaperless(t)
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.PaperlessURL, cfg.PaperlessToken = pl.srv.URL, "env-token"
	st := openTestStore(t, cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)

	s := New(cfg, st, log, nil)
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ost, _ := st.GetSettings(ctx, owner.ID)
	if ost.PaperlessURL != pl.srv.URL || ost.PaperlessToken != "env-token" {
		t.Fatalf("owner not seeded: %q %q", ost.PaperlessURL, ost.PaperlessToken)
	}
	if mst, _ := st.GetSettings(ctx, member.ID); mst.PaperlessToken != "" {
		t.Fatal("the environment credential is the owner's only")
	}
	// Disconnect, then a restart
	ost.PaperlessURL, ost.PaperlessToken = "", ""
	_ = st.SaveSettings(ctx, owner.ID, ost)
	s2 := New(cfg, st, log, nil)
	if err := s2.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if cl, _ := s2.paperlessClient(ctx, owner.ID); cl != nil {
		t.Fatal("cleared connection came back from the environment")
	}
	if ost, _ = st.GetSettings(ctx, owner.ID); ost.PaperlessToken != "" {
		t.Fatal("token re-seeded")
	}
}

// The settings API: the switch is stored, the status comes back, a token
// typed into the test form is tried as is, Disconnect clears URL and token.
func TestPaperlessSettingsAPI(t *testing.T) {
	pl := newFakePaperless(t)
	fc := newFakeServiceCore(t, pl.srv.URL)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx := context.Background()
	member := mkUser(t, st, "member@example.com", roleMember)
	tok, _, err := st.CreateAPIToken(ctx, member.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	call := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
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
	code, got := call("GET", "/settings", nil)
	if code != 200 || got["paperless_mode"] != "core" || got["paperless_via"] != "core" || got["paperless_source"] != "personal" {
		t.Fatalf("GET settings: %d mode=%v via=%v source=%v", code, got["paperless_mode"], got["paperless_via"], got["paperless_source"])
	}
	// test through core, asking Core afresh every time (no cache)
	asked := len(fc.credentialCalls())
	code, res := call("POST", "/settings/test-paperless", map[string]any{})
	if code != 200 || res["via"] != "core" || res["source"] != "personal" {
		t.Fatalf("test through core: %d %v", code, res)
	}
	if code, _ = call("POST", "/settings/test-paperless", map[string]any{}); code != 200 || len(fc.credentialCalls()) != asked+2 {
		t.Fatalf("tests asked Core %d times, want 2", len(fc.credentialCalls())-asked)
	}
	// switch to direct and save a URL + token
	got["paperless_mode"], got["paperless_url"], got["paperless_token"] = "direct", pl.srv.URL, "direct-token"
	code, got = call("PUT", "/settings", got)
	if code != 200 || got["paperless_mode"] != "direct" || got["paperless_via"] != "direct" || got["paperless_token"] != "" || got["paperless_token_set"] != true {
		t.Fatalf("PUT direct: %d %v", code, got)
	}
	before := len(fc.credentialCalls())
	if code, res = call("POST", "/settings/test-paperless", map[string]any{}); code != 200 || res["via"] != "direct" {
		t.Fatalf("test direct: %d %v", code, res)
	}
	if calls := pl.calls(); calls[len(calls)-1] != "Token direct-token" || len(fc.credentialCalls()) != before {
		t.Fatalf("test direct used %v / asked core", calls[len(calls)-1])
	}
	// a typed token is tried as is
	if code, _ = call("POST", "/settings/test-paperless", map[string]any{"url": pl.srv.URL, "token": "typed-token"}); code != 200 {
		t.Fatalf("typed token test: %d", code)
	}
	if calls := pl.calls(); calls[len(calls)-1] != "Token typed-token" {
		t.Fatalf("typed test used %v", calls[len(calls)-1])
	}
	// Disconnect
	got["paperless_url"], got["paperless_external_url"], got["paperless_token"], got["paperless_clear_token"] = "", "", "", true
	code, got = call("PUT", "/settings", got)
	if code != 200 || got["paperless_token_set"] != false || got["paperless_url"] != "" || got["paperless_via"] != "" || got["paperless_mode"] != "direct" {
		t.Fatalf("disconnect: %d %v", code, got)
	}
	// off: test refuses without calling anything
	got["paperless_mode"] = "off"
	if code, got = call("PUT", "/settings", got); got["paperless_mode"] != "off" {
		t.Fatalf("PUT off: %d %v", code, got["paperless_mode"])
	}
	n, c := len(pl.calls()), len(fc.credentialCalls())
	if code, _ = call("POST", "/settings/test-paperless", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("test when off: %d", code)
	}
	if code, _ = call("GET", "/paperless/documents", nil); code != http.StatusConflict {
		t.Fatalf("search when off: %d", code)
	}
	if len(pl.calls()) != n || len(fc.credentialCalls()) != c {
		t.Fatal("off made calls")
	}
	// an unknown mode is ignored
	got["paperless_mode"] = "sideways"
	if _, got = call("PUT", "/settings", got); got["paperless_mode"] != "off" {
		t.Fatalf("bad mode stored: %v", got["paperless_mode"])
	}
}

// Core hears every person's Paperless mode in effect (defaults included) after
// registering, and a person's new mode right after they switch or disconnect.
func TestPaperlessModesReported(t *testing.T) {
	pl := newFakePaperless(t)
	fc := newFakeServiceCore(t, pl.srv.URL)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	other := mkUser(t, st, "other@example.com", roleMember)
	mst, _ := st.GetSettings(ctx, member.ID)
	mst.PaperlessURL, mst.PaperlessToken = pl.srv.URL, "direct-token" // no mode stored: direct by default
	_ = st.SaveSettings(ctx, member.ID, mst)
	ost, _ := st.GetSettings(ctx, other.ID)
	ost.PaperlessMode = modeOff
	_ = st.SaveSettings(ctx, other.ID, ost)

	if err := s.Bootstrap(ctx); err != nil { // starts the Core loop: register, then report
		t.Fatal(err)
	}
	waitReported(t, fc, "owner@example.com", modeCore)
	waitReported(t, fc, "member@example.com", modeDirect)
	waitReported(t, fc, "other@example.com", modeOff)
	_ = owner

	tok, _, err := st.CreateAPIToken(ctx, member.ID, "t")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	put := func(body map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/settings", strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("PUT settings: %d %v", res.StatusCode, out)
		}
		return out
	}
	got := put(map[string]any{"paperless_mode": "off", "paperless_url": pl.srv.URL})
	waitReported(t, fc, "member@example.com", modeOff)
	got["paperless_mode"] = "core"
	got = put(got)
	waitReported(t, fc, "member@example.com", modeCore)
	// an unrelated save does not report again
	_, n := fc.reported("member@example.com")
	got["paperless_tags"] = "x"
	put(got)
	time.Sleep(100 * time.Millisecond)
	if _, n2 := fc.reported("member@example.com"); n2 != n {
		t.Fatalf("an unrelated save reported modes (%d → %d)", n, n2)
	}
	if m, _ := fc.reported("other@example.com"); m != modeOff {
		t.Fatalf("other's mode changed to %q", m)
	}
}

// Without Pocket Core nothing is reported (and nothing fails).
func TestPaperlessModesNotReportedWithoutCore(t *testing.T) {
	s, st := serviceTestServer(t, "", nil)
	mkUser(t, st, "owner@example.com", roleAdmin)
	s.reportModes(context.Background(), 0) // no Core: a no-op
	if modes, err := s.paperlessModes(context.Background(), 0); err != nil || len(modes) != 1 || modes[0].Mode != modeDirect || modes[0].Target != "service:paperless" {
		t.Fatalf("modes without core: %v %v", modes, err)
	}
}
