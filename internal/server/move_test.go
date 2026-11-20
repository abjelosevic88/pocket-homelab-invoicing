package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// Pocket Move copies a person's own business out (move:export) and nothing
// else; Invoicing takes nothing in yet, so move:import opens no route.
func TestMoveExport(t *testing.T) {
	fc := fakeCore(t)
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.CoreURL = fc.URL
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := openTestStore(t, cfg)
	s := New(cfg, st, log, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
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
	ownerTok, _, _ := st.CreateAPIToken(ctx, owner.ID, "owner")
	call := func(tok, method, path string, body any) (int, string) {
		t.Helper()
		j, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(j))
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
	for _, who := range []struct{ tok, name string }{{ownerTok, "Owner"}, {"pcd_member-phone", "Member"}} {
		if code, out := call(who.tok, "POST", "/api/v1/clients", map[string]any{"name": who.name + " Client", "currency": "EUR"}); code >= 300 {
			t.Fatalf("client: %d %s", code, out)
		}
	}
	exp, imp := "pcx_member_export", "pcx_member_import"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/clients", 403},
		{exp, "GET", "/api/v1/invoices", 403},
		{exp, "GET", "/api/v1/reports/export.csv", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{exp, "POST", "/api/v1/clients", 403},
		{imp, "GET", core.PortablePath, 403},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "POST", "/api/v1/clients", 403},
		{imp, "POST", "/api/v1/invoices", 403},
	} {
		if code, body := call(c.tok, c.method, c.path, map[string]any{}); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := call(exp, "GET", core.PortablePath, nil)
	var p core.Portable
	if err := json.Unmarshal([]byte(body), &p); err != nil || len(p.Exports) != 1 || len(p.Imports) != 0 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
	// each person's export is their own business only
	if code, out := call(exp, "GET", "/api/v1/export.json", nil); code != 200 || !strings.Contains(out, "Member Client") || strings.Contains(out, "Owner Client") {
		t.Fatalf("member's export: %d %s", code, out)
	}
	if code, out := call("pcx_owner_export", "GET", "/api/v1/export.json", nil); code != 200 || !strings.Contains(out, "Owner Client") || strings.Contains(out, "Member Client") {
		t.Fatalf("owner's export: %d %s", code, out)
	}
}
