package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// fakeCore answers token introspection like Pocket Core: pcd_/pcu_ tokens map to people.
// The member's Core role is "admin" on purpose: the app must not trust it.
func fakeCore(t *testing.T) *httptest.Server {
	t.Helper()
	people := map[string]map[string]any{
		"pcd_member-phone": {"id": 7, "email": "member@example.com", "name": "Member", "role": "admin"},
		"pcu_owner":        {"id": 1, "email": "owner@example.com", "name": "Owner", "role": "admin"},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/introspect" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		if scope, ok := map[string]string{"pcx_member_read": "invoices:read", "pcx_member_other": "something:else"}[in.Token]; ok { // a linked app acting for the member
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "scope": scope, "audience": "invoicing",
				"user": people["pcd_member-phone"], "app": map[string]any{"id": 4, "slug": "day", "name": "Pocket Day"}})
			return
		}
		if u, ok := people[in.Token]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "device", "user": u})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
	}))
	t.Cleanup(ts.Close)
	return ts
}

type hookSink struct {
	mu     sync.Mutex
	events []string
	srv    *httptest.Server
}

func newHookSink(t *testing.T) *hookSink {
	h := &hookSink{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.events = append(h.events, r.Header.Get("X-Webhook-Event"))
		h.mu.Unlock()
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *hookSink) got() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.events...)
}

// TestPerUserIsolation: the owner fills the app through an API token; a household member
// signs in through a Core device token (and then their own API token) and must not be able
// to list, read, change, delete, search or export any of it.
func TestPerUserIsolation(t *testing.T) {
	core := fakeCore(t)
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.CoreURL = core.URL
	cfg.BaseURL = "http://invoicing.test"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := openTestStore(t, cfg)
	s := New(cfg, st, log, nil)
	ctx := context.Background()
	owner := &store.User{Email: "owner@example.com", Name: "Owner", Role: roleAdmin, PasswordHash: "x"}
	if err := st.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	ownerTok, _, err := st.CreateAPIToken(ctx, owner.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}

	call := func(tok, method, path string, body any) (int, []byte) {
		t.Helper()
		var rd io.Reader
		ct := ""
		switch b := body.(type) {
		case nil:
		case *multipartBody:
			rd, ct = &b.Buffer, b.ct
		default:
			j, _ := json.Marshal(b)
			rd, ct = bytes.NewReader(j), "application/json"
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, out
	}
	mustID := func(status int, body []byte) int64 {
		t.Helper()
		if status >= 300 {
			t.Fatalf("create: %d %s", status, body)
		}
		var v struct {
			ID      int64 `json:"id"`
			Payment struct {
				ID int64 `json:"id"`
			} `json:"payment"`
		}
		_ = json.Unmarshal(body, &v)
		if v.ID == 0 {
			v.ID = v.Payment.ID
		}
		if v.ID == 0 {
			var arr []struct {
				ID int64 `json:"id"`
			}
			_ = json.Unmarshal(body, &arr)
			if len(arr) > 0 {
				v.ID = arr[0].ID
			}
		}
		if v.ID == 0 {
			t.Fatalf("create: no id in %s", body)
		}
		return v.ID
	}

	// ---- the owner's business ----
	ownerHooks := newHookSink(t)
	memberHooks := newHookSink(t)
	if code, b := call(ownerTok, "PUT", "/api/v1/settings", map[string]any{"company_name": "Owner Co", "smtp_password": "owner-secret", "smtp_host": "smtp.owner", "base_currency": "BAM", "invoice_number_format": "OWN-{SEQ:3}"}); code != 200 {
		t.Fatalf("owner settings: %d %s", code, b)
	}
	webhookID := mustID(call(ownerTok, "POST", "/api/v1/webhooks", map[string]any{"url": ownerHooks.srv.URL, "events": "*"}))
	clientID := mustID(call(ownerTok, "POST", "/api/v1/clients", map[string]any{"name": "Secret Client GmbH", "email": "ap@secret.example", "currency": "EUR"}))
	tplID := mustID(call(ownerTok, "POST", "/api/v1/templates", map[string]any{"name": "Owner design", "layout": "modern"}))
	invID := mustID(call(ownerTok, "POST", "/api/v1/invoices", map[string]any{"client_id": clientID, "status": "sent", "issue_date": "2026-01-05", "due_date": "2026-01-20", "template_id": tplID, "items": []map[string]any{{"description": "Secret work", "quantity": 10, "unit_price": 100}}}))
	code, b := call(ownerTok, "GET", fmt.Sprintf("/api/v1/invoices/%d", invID), nil)
	var inv store.Invoice
	_ = json.Unmarshal(b, &inv)
	if code != 200 || !strings.HasPrefix(inv.Number, "OWN-") {
		t.Fatalf("owner invoice: %d %s", code, b)
	}
	paymentID := mustID(call(ownerTok, "POST", "/api/v1/payments", map[string]any{"invoice_id": invID, "amount": 100, "date": "2026-01-10"}))
	expenseID := mustID(call(ownerTok, "POST", "/api/v1/expenses", map[string]any{"client_id": clientID, "date": "2026-01-03", "amount": 42, "currency": "EUR", "description": "Secret expense"}))
	timeID := mustID(call(ownerTok, "POST", "/api/v1/time", map[string]any{"client_id": clientID, "started_at": "2026-01-03", "duration_minutes": 90, "description": "Secret hours"}))
	productID := mustID(call(ownerTok, "POST", "/api/v1/products", map[string]any{"name": "Secret product", "unit_price": 10}))
	taxID := mustID(call(ownerTok, "POST", "/api/v1/tax-rates", map[string]any{"name": "Owner VAT", "rate": 17}))
	recID := mustID(call(ownerTok, "POST", "/api/v1/recurring", map[string]any{"client_id": clientID, "name": "Secret retainer", "start_date": "2099-01-01", "items": []map[string]any{{"description": "Retainer", "quantity": 1, "unit_price": 500}}}))
	mp := newMultipart(t, map[string]string{"title": "Secret contract"}, "file", "contract.pdf", "%PDF-1.4 secret")
	docID := mustID(call(ownerTok, "POST", "/api/v1/documents", mp))
	mp = newMultipart(t, nil, "file", "receipt.pdf", "%PDF-1.4 receipt")
	attID := mustID(call(ownerTok, "POST", fmt.Sprintf("/api/v1/invoices/%d/attachments", invID), mp))

	// ---- the member: first contact through a Core device token ----
	member := "pcd_member-phone"
	code, b = call(member, "GET", "/api/v1/auth/me", nil)
	var me store.User
	_ = json.Unmarshal(b, &me)
	if code != 200 || me.Email != "member@example.com" || me.Role != roleMember {
		t.Fatalf("member auto-provisioned as %d %s (must be role member even though Core says admin)", code, b)
	}
	code, b = call(member, "POST", "/api/v1/tokens", map[string]any{"name": "member script"})
	var tokRes struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(b, &tokRes)
	if code != 201 || tokRes.Token == "" {
		t.Fatalf("member token: %d %s", code, b)
	}
	if code, b := call(member, "POST", "/api/v1/webhooks", map[string]any{"url": memberHooks.srv.URL, "events": "*"}); code != 200 {
		t.Fatalf("member webhook: %d %s", code, b)
	}

	secrets := []string{"Secret Client", "Secret work", "Secret expense", "Secret hours", "Secret product", "Secret retainer", "Secret contract", "Owner Co", "owner-secret", "Owner VAT", "Owner design", inv.Number, "receipt.pdf"}
	noSecrets := func(path string, body []byte) {
		t.Helper()
		for _, sec := range secrets {
			if strings.Contains(string(body), sec) {
				t.Errorf("%s leaks %q: %s", path, sec, body)
			}
		}
	}

	for _, tok := range []string{member, tokRes.Token} {
		via := "core device token"
		if tok == tokRes.Token {
			via = "member API token"
		}
		t.Run(via, func(t *testing.T) {
			// lists, search, reports, exports: nothing of the owner's
			for _, p := range []string{
				"/api/v1/clients?archived=1", "/api/v1/clients?q=Secret", "/api/v1/invoices", "/api/v1/invoices?q=" + inv.Number, "/api/v1/invoices?status=all",
				"/api/v1/payments", "/api/v1/expenses", "/api/v1/time", "/api/v1/products?archived=1", "/api/v1/tax-rates", "/api/v1/templates",
				"/api/v1/recurring", "/api/v1/documents", "/api/v1/documents?q=secret", "/api/v1/documents/categories", "/api/v1/webhooks", "/api/v1/activity",
				"/api/v1/dashboard", "/api/v1/settings", "/api/v1/reports/revenue?from=2025-01-01&to=2027-01-01", "/api/v1/reports/clients?from=2025-01-01&to=2027-01-01",
				"/api/v1/reports/aging", "/api/v1/reports/tax?from=2025-01-01&to=2027-01-01", "/api/v1/reports/currencies?from=2025-01-01&to=2027-01-01",
				"/api/v1/reports/time?from=2025-01-01&to=2027-01-01", "/api/v1/reports/years", "/api/v1/reports/income-tax?year=2026",
				"/api/v1/reports/export.csv?from=2025-01-01&to=2027-01-01", "/api/v1/reports/export.csv?type=payments&from=2025-01-01&to=2027-01-01",
				"/api/v1/reports/export.csv?type=time&from=2025-01-01&to=2027-01-01", "/api/v1/reports/export.csv?type=expenses&from=2025-01-01&to=2027-01-01",
				"/api/v1/reports/export.csv?type=items&from=2025-01-01&to=2027-01-01", "/api/v1/export.json", "/api/v1/month-end?month=2026-01",
				"/api/v1/time/running", "/api/v1/invoices/check-number?number=" + inv.Number,
			} {
				code, body := call(tok, "GET", p, nil)
				if code != 200 {
					t.Errorf("GET %s: %d %s", p, code, body)
				}
				noSecrets(p, body)
			}
			code, body := call(tok, "GET", "/api/v1/invoices/check-number?number="+inv.Number, nil)
			if !strings.Contains(string(body), `"other_clients":[]`) {
				t.Errorf("check-number reveals other people's clients: %d %s", code, body)
			}

			// direct ids: 404, never 403
			for _, p := range []string{
				fmt.Sprintf("/api/v1/clients/%d", clientID), fmt.Sprintf("/api/v1/invoices/%d", invID), fmt.Sprintf("/api/v1/invoices/%d/pdf", invID),
				fmt.Sprintf("/api/v1/invoices/%d/html", invID), fmt.Sprintf("/api/v1/invoices/%d/docx?template_id=%d", invID, tplID), fmt.Sprintf("/api/v1/invoices/%d/emails", invID),
				fmt.Sprintf("/api/v1/invoices/%d/attachments", invID), fmt.Sprintf("/api/v1/attachments/%d", attID), fmt.Sprintf("/api/v1/recurring/%d", recID),
				fmt.Sprintf("/api/v1/documents/%d", docID), fmt.Sprintf("/api/v1/documents/%d/file", docID), fmt.Sprintf("/api/v1/templates/%d/preview", tplID),
				fmt.Sprintf("/api/v1/templates/%d/docx", tplID), fmt.Sprintf("/api/v1/templates/%d/preview?invoice_id=%d", tplID, invID),
			} {
				code, body := call(tok, "GET", p, nil)
				if code != 404 {
					t.Errorf("GET %s: want 404, got %d %s", p, code, body)
				}
				noSecrets(p, body)
			}
			writes := []struct {
				method, path string
				body         any
			}{
				{"PUT", fmt.Sprintf("/api/v1/clients/%d", clientID), map[string]any{"name": "pwned"}},
				{"DELETE", fmt.Sprintf("/api/v1/clients/%d", clientID), nil},
				{"PUT", fmt.Sprintf("/api/v1/invoices/%d", invID), map[string]any{"client_id": clientID}},
				{"POST", fmt.Sprintf("/api/v1/invoices/%d/status", invID), map[string]any{"status": "cancelled"}},
				{"POST", fmt.Sprintf("/api/v1/invoices/%d/send", invID), map[string]any{}},
				{"POST", fmt.Sprintf("/api/v1/invoices/%d/duplicate", invID), nil},
				{"DELETE", fmt.Sprintf("/api/v1/invoices/%d", invID), nil},
				{"DELETE", fmt.Sprintf("/api/v1/attachments/%d", attID), nil},
				{"POST", "/api/v1/payments", map[string]any{"invoice_id": invID, "amount": 1}},
				{"DELETE", fmt.Sprintf("/api/v1/payments/%d", paymentID), nil},
				{"PUT", fmt.Sprintf("/api/v1/expenses/%d", expenseID), map[string]any{"amount": 1}},
				{"DELETE", fmt.Sprintf("/api/v1/expenses/%d", expenseID), nil},
				{"PUT", fmt.Sprintf("/api/v1/time/%d", timeID), map[string]any{"client_id": clientID}},
				{"DELETE", fmt.Sprintf("/api/v1/time/%d", timeID), nil},
				{"PUT", fmt.Sprintf("/api/v1/products/%d", productID), map[string]any{"name": "pwned"}},
				{"DELETE", fmt.Sprintf("/api/v1/products/%d", productID), nil},
				{"PUT", fmt.Sprintf("/api/v1/tax-rates/%d", taxID), map[string]any{"name": "pwned", "rate": 1}},
				{"DELETE", fmt.Sprintf("/api/v1/tax-rates/%d", taxID), nil},
				{"PUT", fmt.Sprintf("/api/v1/templates/%d", tplID), map[string]any{"name": "pwned"}},
				{"DELETE", fmt.Sprintf("/api/v1/templates/%d", tplID), nil},
				{"PUT", fmt.Sprintf("/api/v1/recurring/%d", recID), map[string]any{"client_id": clientID}},
				{"POST", fmt.Sprintf("/api/v1/recurring/%d/run", recID), nil},
				{"DELETE", fmt.Sprintf("/api/v1/recurring/%d", recID), nil},
				{"PUT", fmt.Sprintf("/api/v1/documents/%d", docID), map[string]any{"title": "pwned"}},
				{"DELETE", fmt.Sprintf("/api/v1/documents/%d", docID), nil},
				{"PUT", fmt.Sprintf("/api/v1/webhooks/%d", webhookID), map[string]any{"url": "http://pwned.invalid"}},
				{"DELETE", fmt.Sprintf("/api/v1/webhooks/%d", webhookID), nil},
			}
			for _, w := range writes {
				code, body := call(tok, w.method, w.path, w.body)
				if code != 404 {
					t.Errorf("%s %s: want 404, got %d %s", w.method, w.path, code, body)
				}
			}
			// references to the owner's records inside a body are refused or dropped
			if code, body := call(tok, "POST", "/api/v1/invoices", map[string]any{"client_id": clientID, "items": []map[string]any{{"description": "x", "quantity": 1, "unit_price": 1}}}); code != 400 {
				t.Errorf("invoice for the owner's client: %d %s", code, body)
			}
			if code, body := call(tok, "POST", "/api/v1/time", map[string]any{"client_id": clientID, "duration_minutes": 10}); code != 400 {
				t.Errorf("time entry on the owner's client: %d %s", code, body)
			}
			if code, body := call(tok, "POST", "/api/v1/invoices/from-time", map[string]any{"client_id": clientID}); code != 400 {
				t.Errorf("invoice from the owner's time: %d %s", code, body)
			}
			// box-wide administration is admin-only
			for _, a := range []struct{ method, path string }{{"GET", "/api/v1/users"}, {"POST", "/api/v1/users"}, {"GET", "/api/v1/backup"}, {"POST", "/api/v1/scheduler/run"}, {"PUT", "/api/v1/currencies/USD"}, {"POST", "/api/v1/rates/refresh"}, {"PUT", "/api/v1/rates"}, {"GET", "/api/v1/settings/system"}} {
				if code, _ := call(tok, a.method, a.path, map[string]any{}); code != 403 {
					t.Errorf("%s %s as member: want 403, got %d", a.method, a.path, code)
				}
			}
		})
	}

	// The member's own business works, with their own numbering and settings; the
	// base currency stays the admin's choice.
	if code, b := call(member, "PUT", "/api/v1/settings", map[string]any{"company_name": "Member Studio", "base_currency": "USD", "invoice_number_format": "M-{SEQ:2}"}); code != 200 {
		t.Fatalf("member settings: %d %s", code, b)
	}
	mClient := mustID(call(member, "POST", "/api/v1/clients", map[string]any{"name": "Member's client", "currency": "EUR"}))
	mInv := mustID(call(member, "POST", "/api/v1/invoices", map[string]any{"client_id": mClient, "status": "sent", "items": []map[string]any{{"description": "Member work", "quantity": 1, "unit_price": 50}}}))
	_, b = call(member, "GET", fmt.Sprintf("/api/v1/invoices/%d", mInv), nil)
	var minv store.Invoice
	_ = json.Unmarshal(b, &minv)
	if minv.Number != "M-01" {
		t.Fatalf("member's first invoice number: %q (want their own sequence)", minv.Number)
	}
	if _, b := call(member, "POST", "/api/v1/payments", map[string]any{"invoice_id": mInv, "amount": 50}); !strings.Contains(string(b), `"status":"paid"`) {
		t.Fatalf("member payment: %s", b)
	}
	_, b = call(ownerTok, "GET", "/api/v1/settings", nil)
	var ownerSettings map[string]any
	_ = json.Unmarshal(b, &ownerSettings)
	if ownerSettings["company_name"] != "Owner Co" || ownerSettings["base_currency"] != "BAM" || ownerSettings["invoice_number_format"] != "OWN-{SEQ:3}" || ownerSettings["smtp_password_set"] != true {
		t.Fatalf("owner settings changed by the member: %s", b)
	}
	// and the owner sees nothing of the member's
	for _, p := range []string{"/api/v1/clients", "/api/v1/invoices", "/api/v1/payments", "/api/v1/export.json", "/api/v1/dashboard"} {
		_, body := call(ownerTok, "GET", p, nil)
		if strings.Contains(string(body), "Member") {
			t.Errorf("owner's %s shows the member's data: %s", p, body)
		}
	}
	if code, _ := call(ownerTok, "GET", fmt.Sprintf("/api/v1/invoices/%d", mInv), nil); code != 404 {
		t.Errorf("admin reads a member's invoice: %d (admins get no special read access)", code)
	}

	// The owner's data is intact after all the member's attempts.
	_, b = call(ownerTok, "GET", fmt.Sprintf("/api/v1/invoices/%d", invID), nil)
	var after store.Invoice
	_ = json.Unmarshal(b, &after)
	if after.Number != inv.Number || after.Status != store.StatusPartial && after.Status != store.StatusOverdue || len(after.Attachments) != 1 || len(after.Payments) != 1 {
		t.Fatalf("owner's invoice after the member's attempts: %s", b)
	}

	// Webhooks fire for the record owner only.
	time.Sleep(300 * time.Millisecond)
	for _, e := range memberHooks.got() {
		if e != "client.created" && e != "invoice.created" && e != "payment.created" && e != "invoice.paid" {
			t.Errorf("member webhook got unexpected event %q", e)
		}
	}
	if n := len(memberHooks.got()); n != 4 {
		t.Errorf("member webhook events: %v (want only the member's own 4)", memberHooks.got())
	}
	for _, e := range ownerHooks.got() {
		if e == "" {
			t.Errorf("owner webhook got an empty event")
		}
	}
	if len(ownerHooks.got()) < 3 {
		t.Errorf("owner webhook events: %v", ownerHooks.got())
	}

	// The scheduler works per user: overdue marking and recurring runs never cross over.
	res := s.RunScheduler(ctx)
	if res["recurring_generated"] != 0 {
		t.Errorf("scheduler result: %v", res)
	}
	// Deleting a member who owns records is refused; the owner can't be probed via users either.
	code, b = call(ownerTok, "DELETE", fmt.Sprintf("/api/v1/users/%d", me.ID), nil)
	if code != 409 {
		t.Errorf("deleting a member with records: %d %s", code, b)
	}
}

type multipartBody struct {
	bytes.Buffer
	ct string
}

func newMultipart(t *testing.T, fields map[string]string, fileField, filename, content string) *multipartBody {
	t.Helper()
	mb := &multipartBody{}
	w := multipart.NewWriter(&mb.Buffer)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	fw, err := w.CreateFormFile(fileField, filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte(content))
	_ = w.Close()
	mb.ct = w.FormDataContentType()
	return mb
}
