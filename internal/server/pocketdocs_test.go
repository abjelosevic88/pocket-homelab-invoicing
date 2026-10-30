package server

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pocketdocs/pdtest"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

const docsPublic = "https://documents.example.org"

// fakeLinkCore is Pocket Core's app-link API: a pcx_ token per person for
// Pocket Documents (or a refusal reason), the link state without a token,
// app registration, mode reports and resolve.
type fakeLinkCore struct {
	srv     *httptest.Server
	docsURL string
	mu      sync.Mutex
	refuse  map[string]string // email → reason
	asked   []string          // "<auth> <user> <app> <scopes>" per token request
	statusN int
	modes   map[string]string // "<user> <target>" → last reported mode
	reports int
	uses    []map[string]any // manifest Uses seen at registration
	svcs    []any            // manifest Services seen at registration
}

func newFakeLinkCore(t *testing.T, docs *pdtest.Server) *fakeLinkCore {
	f := &fakeLinkCore{docsURL: docs.URL, refuse: map[string]string{"nobody@example.com": "not_in_audience"}}
	for _, who := range []string{"owner", "member", "other"} {
		docs.Token("pcx_"+who, who)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/links/token":
			var in struct {
				User, App string
				Scopes    []string
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.asked = append(f.asked, r.Header.Get("Authorization")+" "+in.User+" "+in.App+" "+strings.Join(in.Scopes, ","))
			if reason, ok := f.refuse[in.User]; ok {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "refused", "reason": reason})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_" + strings.Split(in.User, "@")[0], "expires_in": 300, "scope": strings.Join(in.Scopes, " "), "app": in.App, "url": f.docsURL, "public_url": docsPublic})
		case "/api/v1/links/status":
			f.statusN++
			user := r.URL.Query().Get("user")
			if r.URL.Query().Get("app") != "documents" {
				_ = json.NewEncoder(w).Encode([]any{})
				return
			}
			state, on := "on", true
			if reason, ok := f.refuse[user]; ok {
				state, on = reason, false
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"app": "documents", "scope": "documents:read", "state": state, "on": on}, {"app": "documents", "scope": "documents:write", "state": state, "on": on}})
		case "/api/v1/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "documents", "url": f.docsURL, "public_url": docsPublic})
		case "/api/v1/apps/register":
			var m struct {
				Uses     []map[string]any `json:"uses"`
				Services []any            `json:"services"`
			}
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.uses, f.svcs = m.Uses, m.Services
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
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLinkCore) tokenCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.asked...)
}

func (f *fakeLinkCore) reported(email string) (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modes[email+" documents"], f.reports
}

// waitReported waits for Core to hear want for the person (reports are sent in the background).
func waitReported(t *testing.T, f *fakeLinkCore, email, want string) {
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

// apiCaller calls the API with a person's token and decodes a JSON answer.
func apiCaller(t *testing.T, s *Server, st *store.Store, uid int64) func(method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	tok, _, err := st.CreateAPIToken(context.Background(), uid, "t")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return func(method, path string, body any) (int, map[string]any, []byte) {
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
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out, raw
	}
}

// Pocket Documents through a Core link: each person gets a token of their
// own from Core on every use, for documents:read + documents:write; direct
// uses the saved token without asking Core (the address from Core when none
// is saved); off makes no calls at all; a refusal reads as "not connected".
func TestDocumentsConnectionModes(t *testing.T) {
	docs := pdtest.New(t)
	fc := newFakeLinkCore(t, docs)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx := context.Background()
	mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	nobody := mkUser(t, st, "nobody@example.com", roleMember)

	// --- core (the default without a saved token, Core being there) ---
	dc, err := s.docsFor(ctx, member.ID)
	if err != nil {
		t.Fatalf("member through core: %v", err)
	}
	if ps := dc.client.Ping(ctx); !ps.OK {
		t.Fatalf("ping through core: %+v", ps)
	}
	if got := docs.Calls(); len(got) != 1 || got[0] != "Bearer pcx_member" {
		t.Fatalf("documents saw %v, want the member's own link token", got)
	}
	if got := fc.tokenCalls(); len(got) != 1 || got[0] != "Bearer pca_invoicing member@example.com documents documents:read,documents:write" {
		t.Fatalf("core asked %v", got)
	}
	if u := dc.docURL(12); u != docsPublic+"/documents/12" {
		t.Fatalf("browser link through core = %q, want Core's public URL", u)
	}
	// every use asks Core again (a switch-off there applies at once)
	if _, err := s.docsFor(ctx, member.ID); err != nil || len(fc.tokenCalls()) != 2 {
		t.Fatalf("second use: %v, core asked %d times", err, len(fc.tokenCalls()))
	}
	mst, _ := st.GetSettings(ctx, member.ID)
	if m := s.publicSettings(ctx, member, mst); m["documents_mode"] != modeCore || m["documents_via"] != modeCore || m["core_configured"] != true {
		t.Fatalf("member status: %v %v", m["documents_mode"], m["documents_via"])
	}

	// --- refusal: not connected, with Core's reason ---
	if _, err := s.docsFor(ctx, nobody.ID); err == nil || !strings.Contains(err.Error(), "you do not have access to Pocket Documents") {
		t.Fatalf("refused person: err = %v", err)
	}
	nst, _ := st.GetSettings(ctx, nobody.ID)
	if m := s.publicSettings(ctx, nobody, nst); m["documents_via"] != "" || !strings.Contains(m["documents_detail"].(string), "do not have access") {
		t.Fatalf("refused status: via=%v detail=%v", m["documents_via"], m["documents_detail"])
	}

	// --- direct: a saved token wins by default, Core is not asked for a token ---
	docs.Token("pd_member", "member")
	mst.DocumentsURL, mst.DocumentsToken = docs.URL, "pd_member"
	if err := st.SaveSettings(ctx, member.ID, mst); err != nil {
		t.Fatal(err)
	}
	before := len(fc.tokenCalls())
	dc, err = s.docsFor(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	dc.client.Ping(ctx)
	if got := docs.Calls(); got[len(got)-1] != "Bearer pd_member" || dc.via != modeDirect || dc.public != docs.URL {
		t.Fatalf("direct used %q via %s, public %s", got[len(got)-1], dc.via, dc.public)
	}
	if len(fc.tokenCalls()) != before {
		t.Fatal("direct mode asked Core for a token")
	}
	// direct without a URL: Core says where Documents lives; the browser URL may differ
	mst.DocumentsURL, mst.DocumentsPublicURL = "", "https://docs.home.example"
	_ = st.SaveSettings(ctx, member.ID, mst)
	if dc, err = s.docsFor(ctx, member.ID); err != nil || dc.client.BaseURL != docs.URL || dc.docURL(3) != "https://docs.home.example/documents/3" {
		t.Fatalf("direct via resolve: %v %+v", err, dc)
	}
	if m := s.publicSettings(ctx, member, mst); m["documents_mode"] != modeDirect || m["documents_via"] != modeDirect || m["documents_token"] != "" || m["documents_token_set"] != true {
		t.Fatalf("direct status: %v", m)
	}

	// --- core chosen explicitly beats the saved token ---
	mst.DocumentsMode = modeCore
	_ = st.SaveSettings(ctx, member.ID, mst)
	dc, _ = s.docsFor(ctx, member.ID)
	dc.client.Ping(ctx)
	if got := docs.Calls(); got[len(got)-1] != "Bearer pcx_member" {
		t.Fatalf("explicit core used %q", got[len(got)-1])
	}

	// --- off: no calls anywhere ---
	mst.DocumentsMode = modeOff
	mst.TimeMode = modeOff // Pocket Time's status is in the settings too; off asks Core nothing either
	_ = st.SaveSettings(ctx, member.ID, mst)
	coreBefore, docsBefore := len(fc.tokenCalls()), len(docs.Calls())
	fc.mu.Lock()
	statusBefore := fc.statusN
	fc.mu.Unlock()
	if dc, err := s.docsFor(ctx, member.ID); dc != nil || err == nil {
		t.Fatal("off must give no connection")
	}
	if m := s.publicSettings(ctx, member, mst); m["documents_mode"] != modeOff || m["documents_via"] != "" {
		t.Fatalf("off status: %v %v", m["documents_mode"], m["documents_via"])
	}
	fc.mu.Lock()
	statusAfter := fc.statusN
	fc.mu.Unlock()
	if len(fc.tokenCalls()) != coreBefore || len(docs.Calls()) != docsBefore || statusAfter != statusBefore {
		t.Fatal("off mode made calls")
	}
}

// Without Pocket Core the default stays direct; DOCUMENTS_URL is the address
// when the person saved none.
func TestDocumentsWithoutCore(t *testing.T) {
	docs := pdtest.New(t)
	docs.Token("pd_owner", "owner")
	s, st := serviceTestServer(t, "", func(c *config.Config) { c.DocumentsURL = docs.URL })
	ctx := context.Background()
	u := mkUser(t, st, "owner@example.com", roleAdmin)
	set, _ := st.GetSettings(ctx, u.ID)
	if m := s.publicSettings(ctx, u, set); m["documents_mode"] != modeDirect || m["core_configured"] != false || m["core_registered"] != false || m["documents_detail"] != "add an API token" {
		t.Fatalf("no core: %v", m)
	}
	set.DocumentsToken = "pd_owner"
	_ = st.SaveSettings(ctx, u.ID, set)
	dc, err := s.docsFor(ctx, u.ID)
	if err != nil || dc.client.BaseURL != docs.URL {
		t.Fatalf("DOCUMENTS_URL not used: %v", err)
	}
	if ps := dc.client.Ping(ctx); !ps.OK {
		t.Fatalf("ping: %+v", ps)
	}
	s.reportModes(ctx, 0) // no Core: a no-op
	if modes, err := s.connModes(ctx, 0); err != nil || len(modes) != 2 || modes[0].Mode != modeDirect || modes[0].Target != "documents" || modes[1].Target != "time" || modes[1].Mode != modeDirect {
		t.Fatalf("modes without core: %v %v", modes, err)
	}
}

// The settings API: the switch is stored, the status comes back, a token
// typed into the test form is tried as is, Disconnect clears URL and token,
// and off closes every Pocket Documents route without calling anything.
func TestDocumentsSettingsAPI(t *testing.T) {
	docs := pdtest.New(t)
	fc := newFakeLinkCore(t, docs)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	member := mkUser(t, st, "member@example.com", roleMember)
	call := apiCaller(t, s, st, member.ID)

	code, got, _ := call("GET", "/settings", nil)
	if code != 200 || got["documents_mode"] != "core" || got["documents_via"] != "core" || got["documents_tags"] != "pocket-invoicing" {
		t.Fatalf("GET settings: %d %v", code, got)
	}
	if _, has := got["paperless_url"]; has {
		t.Fatal("Paperless settings still exposed")
	}
	code, res, _ := call("POST", "/settings/test-documents", map[string]any{})
	if code != 200 || res["via"] != "core" || res["url"] != docsPublic {
		t.Fatalf("test through core: %d %v", code, res)
	}
	code, res, _ = call("GET", "/pocket-documents/status", nil)
	if code != 200 || res["configured"] != true || res["ok"] != true || res["url"] != docsPublic {
		t.Fatalf("status: %d %v", code, res)
	}
	// switch to direct and save a URL + token
	docs.Token("pd_direct", "member")
	got["documents_mode"], got["documents_url"], got["documents_token"] = "direct", docs.URL, "pd_direct"
	code, got, _ = call("PUT", "/settings", got)
	if code != 200 || got["documents_mode"] != "direct" || got["documents_via"] != "direct" || got["documents_token"] != "" || got["documents_token_set"] != true {
		t.Fatalf("PUT direct: %d %v", code, got)
	}
	before := len(fc.tokenCalls())
	if code, res, _ = call("POST", "/settings/test-documents", map[string]any{}); code != 200 || res["via"] != "direct" {
		t.Fatalf("test direct: %d %v", code, res)
	}
	if calls := docs.Calls(); calls[len(calls)-1] != "Bearer pd_direct" || len(fc.tokenCalls()) != before {
		t.Fatalf("test direct used %v / asked core", calls[len(calls)-1])
	}
	// a typed token is tried as is (and a wrong one says so)
	docs.Token("pd_typed", "member")
	if code, _, _ = call("POST", "/settings/test-documents", map[string]any{"url": docs.URL, "token": "pd_typed"}); code != 200 {
		t.Fatalf("typed token test: %d", code)
	}
	if calls := docs.Calls(); calls[len(calls)-1] != "Bearer pd_typed" {
		t.Fatalf("typed test used %v", calls[len(calls)-1])
	}
	if code, res, _ = call("POST", "/settings/test-documents", map[string]any{"url": docs.URL, "token": "pd_wrong"}); code != http.StatusBadGateway || !strings.Contains(fmt.Sprint(res["error"]), "authentication failed") {
		t.Fatalf("wrong token test: %d %v", code, res)
	}
	// Disconnect
	got["documents_url"], got["documents_public_url"], got["documents_token"], got["documents_clear_token"] = "", "", "", true
	code, got, _ = call("PUT", "/settings", got)
	if code != 200 || got["documents_token_set"] != false || got["documents_url"] != "" || got["documents_via"] != "" || got["documents_mode"] != "direct" {
		t.Fatalf("disconnect: %d %v", code, got)
	}
	// off: every route refuses without calling anything
	got["documents_mode"] = "off"
	if code, got, _ = call("PUT", "/settings", got); got["documents_mode"] != "off" {
		t.Fatalf("PUT off: %d %v", code, got["documents_mode"])
	}
	n, c := len(docs.Calls()), len(fc.tokenCalls())
	if code, _, _ = call("POST", "/settings/test-documents", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("test when off: %d", code)
	}
	for _, p := range []string{"/pocket-documents/documents", "/pocket-documents/correspondents", "/pocket-documents/documents/1/thumb"} {
		if code, _, _ = call("GET", p, nil); code != http.StatusConflict {
			t.Fatalf("GET %s when off: %d", p, code)
		}
	}
	if code, res, _ = call("GET", "/pocket-documents/status", nil); code != 200 || res["configured"] != false || res["mode"] != "off" {
		t.Fatalf("status when off: %d %v", code, res)
	}
	if len(docs.Calls()) != n || len(fc.tokenCalls()) != c {
		t.Fatal("off made calls")
	}
	// an unknown mode is ignored
	got["documents_mode"] = "sideways"
	if _, got, _ = call("PUT", "/settings", got); got["documents_mode"] != "off" {
		t.Fatalf("bad mode stored: %v", got["documents_mode"])
	}
}

// Core hears every person's mode in effect (defaults included) after
// registering, and a person's new mode right after they switch or disconnect.
// The manifest asks for the documents link and no third-party service.
func TestDocumentsModesReported(t *testing.T) {
	docs := pdtest.New(t)
	fc := newFakeLinkCore(t, docs)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	other := mkUser(t, st, "other@example.com", roleMember)
	mst, _ := st.GetSettings(ctx, member.ID)
	mst.DocumentsURL, mst.DocumentsToken = docs.URL, "pd_member" // no mode stored: direct by default
	_ = st.SaveSettings(ctx, member.ID, mst)
	ost, _ := st.GetSettings(ctx, other.ID)
	ost.DocumentsMode = modeOff
	_ = st.SaveSettings(ctx, other.ID, ost)

	if err := s.Bootstrap(ctx); err != nil { // starts the Core loop: register, then report
		t.Fatal(err)
	}
	waitReported(t, fc, "owner@example.com", modeCore)
	waitReported(t, fc, "member@example.com", modeDirect)
	waitReported(t, fc, "other@example.com", modeOff)
	fc.mu.Lock()
	uses, svcs := fc.uses, fc.svcs
	fc.mu.Unlock()
	if len(svcs) != 0 || len(uses) != 2 || uses[0]["app"] != "documents" || fmt.Sprint(uses[0]["scopes"]) != "[documents:read documents:write]" ||
		uses[1]["app"] != "time" || fmt.Sprint(uses[1]["scopes"]) != "[time:read time:write]" {
		t.Fatalf("manifest uses=%v services=%v", uses, svcs)
	}

	call := apiCaller(t, s, st, member.ID)
	put := func(body map[string]any) map[string]any {
		t.Helper()
		code, out, raw := call("PUT", "/settings", body)
		if code != 200 {
			t.Fatalf("PUT settings: %d %s", code, raw)
		}
		return out
	}
	got := put(map[string]any{"documents_mode": "off", "documents_url": docs.URL})
	waitReported(t, fc, "member@example.com", modeOff)
	got["documents_mode"] = "core"
	got = put(got)
	waitReported(t, fc, "member@example.com", modeCore)
	// an unrelated save does not report again
	_, n := fc.reported("member@example.com")
	got["documents_tags"] = "x"
	put(got)
	time.Sleep(100 * time.Millisecond)
	if _, n2 := fc.reported("member@example.com"); n2 != n {
		t.Fatalf("an unrelated save reported modes (%d → %d)", n, n2)
	}
	if m, _ := fc.reported("other@example.com"); m != modeOff {
		t.Fatalf("other's mode changed to %q", m)
	}
}

// Filing: an invoice PDF goes into the person's own Pocket Documents under
// the client's correspondent (by name), with the invoice type and tags, and
// an external id that makes filing again idempotent; the client page lists
// that correspondent's documents and thumbnails come through Invoicing.
// Another person sees and touches none of it.
func TestFileInvoiceInDocuments(t *testing.T) {
	docs := pdtest.New(t)
	fc := newFakeLinkCore(t, docs)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx := context.Background()
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	athena := docs.AddNamed("owner", "correspondents", "Athena Studio") // recreated by the Paperless import, new id
	docs.AddNamed("member", "correspondents", "Athena Studio")          // the member's own, same name
	call := apiCaller(t, s, st, owner.ID)
	mcall := apiCaller(t, s, st, member.ID)

	mustID := func(code int, out map[string]any, raw []byte) int64 {
		t.Helper()
		if code >= 300 {
			t.Fatalf("create: %d %s", code, raw)
		}
		return int64(out["id"].(float64))
	}
	cid := mustID(call("POST", "/clients", map[string]any{"name": "Athena Studio S.à r.l.", "currency": "EUR", "documents_correspondent": " Athena Studio "}))
	plain := mustID(call("POST", "/clients", map[string]any{"name": "Acme", "currency": "EUR"}))
	inv := mustID(call("POST", "/invoices", map[string]any{"client_id": cid, "issue_date": "2026-10-01", "due_date": "2026-10-15", "items": []map[string]any{{"description": "Work", "quantity": 1, "unit_price": 100}}}))
	inv2 := mustID(call("POST", "/invoices", map[string]any{"client_id": plain, "issue_date": "2026-10-02", "due_date": "2026-10-16", "items": []map[string]any{{"description": "Work", "quantity": 1, "unit_price": 50}}}))
	if code, c, _ := call("GET", fmt.Sprintf("/clients/%d", cid), nil); code != 200 || c["client"].(map[string]any)["documents_correspondent"] != "Athena Studio" {
		t.Fatalf("client: %d %v", code, c)
	}

	// file the invoice
	code, out, raw := call("POST", fmt.Sprintf("/invoices/%d/pocket-documents", inv), map[string]any{"source": "generated"})
	if code != 200 {
		t.Fatalf("file: %d %s", code, raw)
	}
	link, _ := out["pocket_documents"].(map[string]any)
	if link == nil || link["documents_id"].(float64) == 0 || link["url"] != fmt.Sprintf("%s/documents/%d", docsPublic, int64(link["documents_id"].(float64))) {
		t.Fatalf("link: %s", raw)
	}
	docID := int64(link["documents_id"].(float64))
	filed := docs.Docs("owner")
	if len(filed) != 1 {
		t.Fatalf("owner's documents: %+v", filed)
	}
	d := filed[0]
	if d.ExternalID != fmt.Sprintf("invoicing:invoice:%d", inv) || d.Correspondent == nil || *d.Correspondent != athena || docs.NameOf("owner", *d.DocumentType) != "Invoice" || len(d.Tags) != 1 || docs.NameOf("owner", d.Tags[0]) != "pocket-invoicing" || d.Created != "2026-10-01" || !strings.HasPrefix(string(d.Data), "%PDF") {
		t.Fatalf("filed document: %+v", d)
	}
	if len(docs.Docs("member")) != 0 {
		t.Fatal("filed in the member's documents")
	}
	// the invoice page shows the link (without asking for a token)
	asked := len(fc.tokenCalls())
	if _, got, _ := call("GET", fmt.Sprintf("/invoices/%d", inv), nil); got["pocket_documents"].(map[string]any)["url"] != link["url"] || len(fc.tokenCalls()) != asked {
		t.Fatalf("invoice page: %v (asked core %d times)", got["pocket_documents"], len(fc.tokenCalls())-asked)
	}
	// filing again: already filed, no upload
	call("POST", fmt.Sprintf("/invoices/%d/pocket-documents", inv), map[string]any{})
	if len(docs.Docs("owner")) != 1 {
		t.Fatal("filed twice")
	}
	// forget the link, file again: Documents answers 409 for the external id and the link comes back
	if code, _, _ := call("DELETE", fmt.Sprintf("/pocket-documents/links/%d?kind=invoice", inv), nil); code != 200 {
		t.Fatalf("unlink: %d", code)
	}
	_, out, _ = call("POST", fmt.Sprintf("/invoices/%d/pocket-documents", inv), map[string]any{})
	if l, _ := out["pocket_documents"].(map[string]any); l == nil || int64(l["documents_id"].(float64)) != docID || len(docs.Docs("owner")) != 1 {
		t.Fatalf("re-filed: %v, %d documents", out["pocket_documents"], len(docs.Docs("owner")))
	}
	// a client without a correspondent is filed under its own name (created)
	call("POST", fmt.Sprintf("/invoices/%d/pocket-documents", inv2), map[string]any{})
	for _, x := range docs.Docs("owner") {
		if x.ExternalID == fmt.Sprintf("invoicing:invoice:%d", inv2) && (x.Correspondent == nil || docs.NameOf("owner", *x.Correspondent) != "Acme") {
			t.Fatalf("plain client filed as %+v", x)
		}
	}

	// the client's documents, newest first, and the thumbnail through Invoicing
	code, list, raw := call("GET", fmt.Sprintf("/pocket-documents/documents?client_id=%d&page_size=15", cid), nil)
	if code != 200 || list["count"].(float64) != 1 || list["correspondent"] != "Athena Studio" || list["url"] != fmt.Sprintf("%s/documents?correspondent=%d", docsPublic, athena) {
		t.Fatalf("client documents: %d %s", code, raw)
	}
	first := list["results"].([]any)[0].(map[string]any)
	if int64(first["id"].(float64)) != docID || first["correspondent"] != "Athena Studio" || list["linked"].(map[string]any)[fmt.Sprint(docID)] == nil {
		t.Fatalf("client document: %s", raw)
	}
	if code, _, raw := call("GET", fmt.Sprintf("/pocket-documents/documents/%d/thumb", docID), nil); code != 200 || string(raw) != fmt.Sprintf("thumb-of-%d", docID) || strings.Contains(string(raw), "pcx_") {
		t.Fatalf("thumb: %d %s", code, raw)
	}
	// a client whose correspondent is not in Documents: an empty list, nothing created
	plainCorr := mustID(call("POST", "/clients", map[string]any{"name": "Nowhere Ltd", "currency": "EUR", "documents_correspondent": "Nowhere"}))
	if code, l, raw := call("GET", fmt.Sprintf("/pocket-documents/documents?client_id=%d", plainCorr), nil); code != 200 || l["count"].(float64) != 0 || l["correspondent_id"].(float64) != 0 {
		t.Fatalf("missing correspondent: %d %s", code, raw)
	}

	// ---- the member: none of the owner's ----
	if code, _, _ := mcall("POST", fmt.Sprintf("/invoices/%d/pocket-documents", inv), map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("member files the owner's invoice: %d", code)
	}
	if code, _, _ := mcall("GET", fmt.Sprintf("/pocket-documents/documents?client_id=%d", cid), nil); code != http.StatusNotFound {
		t.Fatalf("member lists the owner's client: %d", code)
	}
	if code, l, raw := mcall("GET", "/pocket-documents/documents", nil); code != 200 || l["count"].(float64) != 0 || strings.Contains(string(raw), "Athena Studio S") {
		t.Fatalf("member's own list: %d %s", code, raw)
	}
	if code, _, _ := mcall("GET", fmt.Sprintf("/pocket-documents/documents/%d/thumb", docID), nil); code != http.StatusNotFound {
		t.Fatalf("member reads the owner's thumbnail: %d", code)
	}
	if code, _, _ := mcall("DELETE", fmt.Sprintf("/pocket-documents/links/%d?kind=invoice", inv), nil); code != 200 {
		t.Fatalf("unlink: %d", code)
	}
	if l, _ := st.GetDocsLink(ctx, owner.ID, "invoice", inv); l == nil {
		t.Fatal("the member's unlink removed the owner's link")
	}
	if code, _, raw := mcall("POST", "/pocket-documents/correspondents", map[string]any{"name": "Member Corp", "client_id": cid}); code != 200 {
		t.Fatalf("member creates a correspondent: %d %s", code, raw)
	}
	if c, _ := st.GetClient(ctx, owner.ID, cid); c.DocumentsCorrespondent != "Athena Studio" {
		t.Fatalf("the member relinked the owner's client: %q", c.DocumentsCorrespondent)
	}
}

// Old Paperless links are carried over by the scheduler: a document imported
// from Paperless carries the external id paperless:<id>; one that is missing
// is recorded as not filed so the button comes back.
func TestAdoptPaperlessLinks(t *testing.T) {
	docs := pdtest.New(t)
	fc := newFakeLinkCore(t, docs)
	s, st := serviceTestServer(t, fc.srv.URL, nil)
	ctx := context.Background()
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	call := apiCaller(t, s, st, owner.ID)
	var ids []int64
	for i := 0; i < 2; i++ {
		code, c, raw := call("POST", "/clients", map[string]any{"name": fmt.Sprintf("Client %d", i), "currency": "EUR"})
		if code != 201 {
			t.Fatalf("client: %d %s", code, raw)
		}
		code, inv, raw := call("POST", "/invoices", map[string]any{"client_id": c["id"], "issue_date": "2026-10-01", "due_date": "2026-10-15", "items": []map[string]any{{"description": "Work", "quantity": 1, "unit_price": 10}}})
		if code != 201 {
			t.Fatalf("invoice: %d %s", code, raw)
		}
		ids = append(ids, int64(inv["id"].(float64)))
	}
	imported := docs.AddDoc("owner", pdtest.Doc{Title: "Invoice 1", Created: "2026-10-01", ExternalID: "paperless:77"})
	if _, err := st.DB.Exec(`INSERT INTO paperless_links (user_id, kind, ref_id, paperless_id) VALUES (?, 'invoice', ?, 77), (?, 'invoice', ?, 78)`, owner.ID, ids[0], owner.ID, ids[1]); err != nil {
		t.Fatal(err)
	}
	res := s.RunScheduler(ctx)
	if res["documents_links_adopted"] != 1 {
		t.Fatalf("scheduler: %v", res)
	}
	if l, _ := st.GetDocsLink(ctx, owner.ID, "invoice", ids[0]); l == nil || l.DocumentsID != imported {
		t.Fatalf("adopted: %+v", l)
	}
	if l, _ := st.GetDocsLink(ctx, owner.ID, "invoice", ids[1]); l == nil || l.DocumentsID != 0 || !strings.Contains(l.Error, "Paperless #78") {
		t.Fatalf("missing one: %+v", l)
	}
	// nothing left: the next run asks nobody
	n := len(fc.tokenCalls())
	s.RunScheduler(ctx)
	if len(fc.tokenCalls()) != n {
		t.Fatal("scheduler asked Core with nothing to adopt")
	}
	// the failed one can be filed again
	code, out, raw := call("POST", fmt.Sprintf("/invoices/%d/pocket-documents", ids[1]), map[string]any{})
	if l, _ := out["pocket_documents"].(map[string]any); code != 200 || l == nil || l["documents_id"].(float64) == 0 || l["error"] != nil {
		t.Fatalf("file again: %d %s", code, raw)
	}
}
