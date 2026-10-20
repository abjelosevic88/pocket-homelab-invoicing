package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// A linked app (Pocket Day) acting for the member through Pocket Core reads
// only the member's invoices and payments, and nothing else in the app.
func TestCoreLinkScopes(t *testing.T) {
	core := fakeCore(t)
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.CoreURL = core.URL
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
	call := func(tok, method, path string, body any) (int, string) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			j, _ := json.Marshal(body)
			rd = bytes.NewReader(j)
		}
		req, _ := http.NewRequest(method, ts.URL+"/api/v1"+path, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out)
	}
	create := func(tok, path string, body any) int64 {
		t.Helper()
		code, out := call(tok, "POST", path, body)
		var v struct {
			ID      int64 `json:"id"`
			Payment struct {
				ID int64 `json:"id"`
			} `json:"payment"`
		}
		_ = json.Unmarshal([]byte(out), &v)
		if v.ID == 0 {
			v.ID = v.Payment.ID
		}
		if code >= 300 || v.ID == 0 {
			t.Fatalf("POST %s: %d %s", path, code, out)
		}
		return v.ID
	}
	business := func(tok, who string) (inv int64) {
		cl := create(tok, "/clients", map[string]any{"name": who + " Client", "currency": "EUR"})
		inv = create(tok, "/invoices", map[string]any{"client_id": cl, "status": "sent", "issue_date": "2026-01-05", "due_date": "2026-01-20",
			"items": []map[string]any{{"description": who + " work", "quantity": 1, "unit_price": 100}}})
		create(tok, "/payments", map[string]any{"invoice_id": inv, "amount": 40, "date": "2026-01-10", "notes": who + " payment"})
		return inv
	}
	ownerInv := business(ownerTok, "Owner")
	memberInv := business("pcd_member-phone", "Member")
	link, other := "pcx_member_read", "pcx_member_other"

	// auth/me works for any linked app and is the member.
	for _, tok := range []string{link, other} {
		if code, out := call(tok, "GET", "/auth/me", nil); code != 200 || !strings.Contains(out, "member@example.com") {
			t.Fatalf("auth/me with %s: %d %s", tok, code, out)
		}
	}
	// invoices:read sees the member's invoices and payments only.
	for _, p := range []string{"/invoices", "/invoices?from=2026-01-01&to=2026-12-31&limit=100", fmt.Sprintf("/invoices/%d", memberInv), "/payments", "/payments?from=2026-01-10&to=2026-01-10"} {
		code, out := call(link, "GET", p, nil)
		if code != 200 || !strings.Contains(out, "Member") || strings.Contains(out, "Owner") {
			t.Fatalf("GET %s through the link: %d %s", p, code, out)
		}
	}
	if code, _ := call(link, "GET", fmt.Sprintf("/invoices/%d", ownerInv), nil); code != 404 {
		t.Fatalf("owner's invoice through the member's link: %d, want 404", code)
	}
	// A token without invoices:read cannot read.
	for _, p := range []string{"/invoices", fmt.Sprintf("/invoices/%d", memberInv), "/payments"} {
		if code, _ := call(other, "GET", p, nil); code != 403 {
			t.Fatalf("GET %s without invoices:read: %d, want 403", p, code)
		}
	}
	// Everything else stays closed: writes, files, reports, exports, settings, tokens, admin.
	for _, c := range []struct{ m, p string }{
		{"PUT", "/auth/me"}, {"POST", "/auth/logout"}, {"GET", "/dashboard"}, {"GET", "/activity"},
		{"POST", "/invoices"}, {"PUT", fmt.Sprintf("/invoices/%d", memberInv)}, {"DELETE", fmt.Sprintf("/invoices/%d", memberInv)},
		{"POST", fmt.Sprintf("/invoices/%d/status", memberInv)}, {"POST", fmt.Sprintf("/invoices/%d/send", memberInv)},
		{"GET", fmt.Sprintf("/invoices/%d/pdf", memberInv)}, {"GET", fmt.Sprintf("/invoices/%d/html", memberInv)}, {"GET", fmt.Sprintf("/invoices/%d/attachments", memberInv)},
		{"GET", "/invoices/next-number"}, {"GET", "/invoices/check-number"},
		{"POST", "/payments"}, {"DELETE", "/payments/1"}, {"GET", "/clients"}, {"GET", "/expenses"}, {"GET", "/time"}, {"GET", "/documents"},
		{"GET", "/reports/revenue"}, {"GET", "/reports/export.csv"}, {"GET", "/reports/accountant-package.zip"}, {"GET", "/export.json"},
		{"GET", "/settings"}, {"PUT", "/settings"}, {"GET", "/settings/system"}, {"GET", "/tokens"}, {"POST", "/tokens"}, {"GET", "/webhooks"},
		{"GET", "/users"}, {"POST", "/users"}, {"GET", "/backup"}, {"POST", "/scheduler/run"}, {"PUT", "/rates"},
	} {
		if code, _ := call(link, c.m, c.p, map[string]any{}); code != 403 {
			t.Fatalf("%s %s with a link token: %d, want 403", c.m, c.p, code)
		}
	}
	// And a link token never counts as a signed-in session.
	if code, out := call(link, "GET", "/auth/status", nil); code != 200 || !strings.Contains(out, `"authenticated":false`) {
		t.Fatalf("auth/status with a link token: %d %s", code, out)
	}
}
