package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeSign is Pocket Sign's POST /api/v1/documents: it answers like Sign
// does when a document is created and sent in one call.
type fakeSign struct {
	srv  *httptest.Server
	mu   sync.Mutex
	got  []map[string]any
	auth []string
	fail bool
}

func newFakeSign(t *testing.T) *fakeSign {
	f := &fakeSign{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/auth/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"email": "owner@example.com"})
		case "POST /api/v1/documents":
			if f.fail {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "this PDF can't be read"})
				return
			}
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.got = append(f.got, in)
			rc := in["recipients"].([]any)[0].(map[string]any)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"document": map[string]any{"id": 41, "status": "pending", "recipients": []map[string]any{{"email": rc["email"], "link": "https://sign.example/s/tok", "email_error": ""}}},
				"emails":   []map[string]any{{"recipient_id": 1, "sent": true, "error": ""}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// "Get it signed" sends the invoice PDF to the client through the person's
// own Pocket Sign: the client's contact and email unless others are typed,
// a signature box placed by Sign, sent at once, the invoice remembered.
// Direct uses the saved token; without a connection there is nothing to do.
func TestInvoiceGetItSigned(t *testing.T) {
	sign := newFakeSign(t)
	s, st := serviceTestServer(t, "", nil)
	owner := mkUser(t, st, "owner@example.com", roleAdmin)
	member := mkUser(t, st, "member@example.com", roleMember)
	call := apiCaller(t, s, st, owner.ID)
	mcall := apiCaller(t, s, st, member.ID)
	mustID := func(code int, out map[string]any, raw []byte) int64 {
		t.Helper()
		if code >= 300 {
			t.Fatalf("create: %d %s", code, raw)
		}
		return int64(out["id"].(float64))
	}
	cid := mustID(call("POST", "/clients", map[string]any{"name": "Acme d.o.o.", "contact_name": "Ana Kovač", "email": "Ana@Acme.test", "currency": "EUR"}))
	bare := mustID(call("POST", "/clients", map[string]any{"name": "No Mail Ltd", "currency": "EUR"}))
	inv := mustID(call("POST", "/invoices", map[string]any{"client_id": cid, "issue_date": "2026-11-01", "due_date": "2026-11-15", "items": []map[string]any{{"description": "Work", "quantity": 1, "unit_price": 100}}}))
	inv2 := mustID(call("POST", "/invoices", map[string]any{"client_id": bare, "issue_date": "2026-11-02", "due_date": "2026-11-16", "items": []map[string]any{{"description": "Work", "quantity": 1, "unit_price": 50}}}))

	// not connected: no Core, no token
	if code, out, _ := call("GET", "/pocket-sign/status", nil); code != 200 || out["configured"] != false {
		t.Fatalf("status without a connection: %d %v", code, out)
	}
	if code, _, raw := call("POST", fmt.Sprintf("/invoices/%d/sign", inv), map[string]any{}); code != 400 || !strings.Contains(string(raw), "Pocket Sign") {
		t.Fatalf("sign without a connection: %d %s", code, raw)
	}

	// direct: the saved URL and token
	if code, out, raw := call("PUT", "/settings", map[string]any{"sign_mode": "direct", "sign_url": sign.srv.URL + "/", "sign_token": "psg_secret"}); code != 200 || out["sign_token"] != "" || out["sign_token_set"] != true || out["sign_via"] != "direct" {
		t.Fatalf("settings: %d %s", code, raw)
	}
	if code, out, raw := call("POST", "/settings/test-sign", map[string]any{}); code != 200 || out["ok"] != true {
		t.Fatalf("test: %d %s", code, raw)
	}
	code, out, raw := call("POST", fmt.Sprintf("/invoices/%d/sign", inv), map[string]any{})
	if code != 200 || out["document_id"].(float64) != 41 || out["emailed"] != true || out["email"] != "ana@acme.test" || out["name"] != "Ana Kovač" ||
		out["url"] != sign.srv.URL+"/documents/41" {
		t.Fatalf("sign: %d %s", code, raw)
	}
	if len(sign.got) != 1 || sign.auth[len(sign.auth)-1] != "Bearer psg_secret" {
		t.Fatalf("sign calls: %d %v", len(sign.got), sign.auth)
	}
	body := sign.got[0]
	pdf, _ := base64.StdEncoding.DecodeString(body["pdf_base64"].(string))
	rc := body["recipients"].([]any)[0].(map[string]any)
	if !strings.HasPrefix(string(pdf), "%PDF") || body["auto_fields"] != true || body["send"] != true || !strings.HasPrefix(body["title"].(string), "Invoice ") ||
		!strings.Contains(body["title"].(string), "Acme d.o.o.") || body["source_ref"] == "" || rc["email"] != "ana@acme.test" || rc["role"] != "signer" {
		t.Fatalf("what Sign got: %v", body)
	}
	if _, _, raw := call("GET", fmt.Sprintf("/activity?entity_type=invoice&entity_id=%d", inv), nil); !strings.Contains(string(raw), "Sent for signing in Pocket Sign to Ana Kovač") {
		t.Fatalf("activity line missing: %s", raw)
	}

	// someone else typed in; a client without email needs one
	if code, _, raw := call("POST", fmt.Sprintf("/invoices/%d/sign", inv2), map[string]any{}); code != 400 || !strings.Contains(string(raw), "no email") {
		t.Fatalf("no email: %d %s", code, raw)
	}
	if code, _, raw := call("POST", fmt.Sprintf("/invoices/%d/sign", inv2), map[string]any{"name": "Boss", "email": "boss@nomail.test", "message": "Sign please"}); code != 200 {
		t.Fatalf("typed signer: %d %s", code, raw)
	}
	if b := sign.got[1]; b["message"] != "Sign please" || b["recipients"].([]any)[0].(map[string]any)["name"] != "Boss" {
		t.Fatalf("typed signer body: %v", b)
	}
	// Sign's own refusal comes through in its words
	sign.fail = true
	if code, _, raw := call("POST", fmt.Sprintf("/invoices/%d/sign", inv), map[string]any{}); code != 502 || !strings.Contains(string(raw), "this PDF can't be read") {
		t.Fatalf("sign refusal: %d %s", code, raw)
	}
	sign.fail = false

	// strictly per person: the member can't send the owner's invoice
	if code, _, _ := mcall("POST", fmt.Sprintf("/invoices/%d/sign", inv), map[string]any{}); code != 404 {
		t.Fatalf("member signing the owner's invoice: %d", code)
	}
	// off: no calls
	before := len(sign.got)
	call("PUT", "/settings", map[string]any{"sign_mode": "off"})
	if code, _, _ := call("POST", fmt.Sprintf("/invoices/%d/sign", inv), map[string]any{}); code != 400 || len(sign.got) != before {
		t.Fatalf("off: %d, calls %d → %d", code, before, len(sign.got))
	}
}
