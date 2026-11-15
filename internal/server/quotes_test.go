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
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// TestQuoteFlow: a quote is written, shared, accepted by the client from the
// link, and that starts one draft invoice with its lines; nothing about it
// reaches the invoice list before that.
func TestQuoteFlow(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.BaseURL = "http://invoicing.test"
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
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
	tok, _, err := st.CreateAPIToken(ctx, owner.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path string, body any) (int, []byte) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			j, _ := json.Marshal(body)
			rd = bytes.NewReader(j)
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, out
	}
	public := func(method, path string, form url.Values) (int, string, string) {
		t.Helper()
		var res *http.Response
		var err error
		if method == http.MethodPost {
			res, err = noFollow.PostForm(ts.URL+path, form)
		} else {
			res, err = noFollow.Get(ts.URL + path)
		}
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out), res.Header.Get("Location")
	}
	var client store.Client
	code, body := call("POST", "/api/v1/clients", map[string]any{"name": "Acme Corp", "email": "", "currency": "EUR", "payment_terms_days": 10})
	if code != 201 && code != 200 {
		t.Fatalf("client: %d %s", code, body)
	}
	_ = json.Unmarshal(body, &client)

	// Create: own numbering, valid-until from the settings, totals computed.
	code, body = call("POST", "/api/v1/quotes", map[string]any{"client_id": client.ID, "items": []map[string]any{{"description": "Website redesign", "unit": "fixed", "quantity": 1, "unit_price": 2000, "tax_rate": 17}, {"description": "Hosting", "unit": "month", "quantity": 12, "unit_price": 10}}, "discount_type": "percent", "discount_value": 10})
	if code != 201 {
		t.Fatalf("create quote: %d %s", code, body)
	}
	var q store.Quote
	_ = json.Unmarshal(body, &q)
	if !strings.HasPrefix(q.Number, fmt.Sprintf("Q-%d-", time.Now().Year())) || q.Status != "draft" || len(q.Items) != 2 {
		t.Fatalf("quote = %+v", q)
	}
	if want := time.Now().AddDate(0, 0, 30).Format("2006-01-02"); q.ValidUntil != want {
		t.Errorf("valid_until = %s, want %s", q.ValidUntil, want)
	}
	// 2120 - 10% = 1908; tax 17% of 1800 = 306 → 2214
	if q.Subtotal != 2120 || q.DiscountTotal != 212 || q.Total != 2214 {
		t.Errorf("totals = %v %v %v", q.Subtotal, q.DiscountTotal, q.Total)
	}

	// A draft is not shared yet; quotes never show up among invoices.
	if c, _, _ := public("GET", "/q/"+q.PublicToken, nil); c != 404 {
		t.Errorf("draft link = %d, want 404", c)
	}
	_, body = call("GET", "/api/v1/invoices", nil)
	if !strings.Contains(string(body), `"total":0`) {
		t.Errorf("invoices before acceptance: %s", body)
	}

	// The PDF renders as a quote.
	if c, b := call("GET", fmt.Sprintf("/api/v1/quotes/%d/pdf", q.ID), nil); c != 200 || !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("pdf: %d", c)
	}

	// Mark sent; the link opens, shows the accept form and counts as viewed.
	if c, b := call("POST", fmt.Sprintf("/api/v1/quotes/%d/status", q.ID), map[string]string{"status": "sent"}); c != 200 {
		t.Fatalf("mark sent: %d %s", c, b)
	}
	c, page, _ := public("GET", "/q/"+q.PublicToken, nil)
	if c != 200 || !strings.Contains(page, "Accept quote") || !strings.Contains(page, "QUOTE") || !strings.Contains(page, "Valid until") {
		t.Fatalf("public page: %d\n%s", c, page)
	}
	if strings.Contains(page, "Balance due") {
		t.Errorf("a quote shows no balance due")
	}
	_, body = call("GET", fmt.Sprintf("/api/v1/quotes/%d", q.ID), nil)
	_ = json.Unmarshal(body, &q)
	if q.Status != "viewed" || q.ViewedAt == nil {
		t.Errorf("after viewing: %s", q.Status)
	}

	// Editing a sent quote makes a new revision and keeps it open.
	edit := map[string]any{"client_id": client.ID, "valid_until": q.ValidUntil, "items": []map[string]any{{"description": "Website redesign", "unit": "fixed", "quantity": 1, "unit_price": 1800, "tax_rate": 17}}}
	if c, b := call("PUT", fmt.Sprintf("/api/v1/quotes/%d", q.ID), edit); c != 200 {
		t.Fatalf("revise: %d %s", c, b)
	} else {
		_ = json.Unmarshal(b, &q)
	}
	if q.Revision != 2 || q.Status != "viewed" || q.Total != 2106 {
		t.Errorf("revision = %d status = %s total = %v", q.Revision, q.Status, q.Total)
	}

	// Accepting needs a name and the tick.
	if _, _, loc := public("POST", "/q/"+q.PublicToken+"/accept", url.Values{"agree": {"1"}}); !strings.HasSuffix(loc, "?e=name") {
		t.Errorf("no name → %q", loc)
	}
	if _, _, loc := public("POST", "/q/"+q.PublicToken+"/accept", url.Values{"name": {"Jane Client"}}); !strings.HasSuffix(loc, "?e=agree") {
		t.Errorf("no tick → %q", loc)
	}
	// Accept: one draft invoice with the quote's lines, linked both ways.
	if c, _, _ := public("POST", "/q/"+q.PublicToken+"/accept", url.Values{"name": {"Jane Client"}, "po": {"PO-77"}, "agree": {"1"}}); c != http.StatusSeeOther {
		t.Fatalf("accept: %d", c)
	}
	// a second click (or the person's button) never makes a second invoice
	public("POST", "/q/"+q.PublicToken+"/accept", url.Values{"name": {"Jane Client"}, "agree": {"1"}})
	if c, _ := call("POST", fmt.Sprintf("/api/v1/quotes/%d/convert", q.ID), nil); c != http.StatusConflict {
		t.Errorf("convert after invoiced = %d, want 409", c)
	}
	_, body = call("GET", fmt.Sprintf("/api/v1/quotes/%d", q.ID), nil)
	_ = json.Unmarshal(body, &q)
	if q.Status != "invoiced" || q.InvoiceID == nil || q.AcceptedName != "Jane Client" || q.PONumber != "PO-77" || q.InvoiceNumber == "" {
		t.Fatalf("after accept: %+v", q)
	}
	var invs struct {
		Items []store.Invoice `json:"items"`
		Total int             `json:"total"`
	}
	_, body = call("GET", "/api/v1/invoices", nil)
	_ = json.Unmarshal(body, &invs)
	if invs.Total != 1 {
		t.Fatalf("invoices after accept: %s", body)
	}
	_, body = call("GET", fmt.Sprintf("/api/v1/invoices/%d", *q.InvoiceID), nil)
	var inv store.Invoice
	_ = json.Unmarshal(body, &inv)
	if inv.Status != "draft" || inv.Total != q.Total || inv.PONumber != "PO-77" || len(inv.Items) != 1 || inv.FromQuote == nil || inv.FromQuote.ID != q.ID {
		t.Fatalf("draft invoice: %+v", inv)
	}
	if want := time.Now().AddDate(0, 0, 10).Format("2006-01-02"); inv.DueDate != want {
		t.Errorf("invoice due = %s, want %s (client terms)", inv.DueDate, want)
	}
	// The page now says it is accepted; an accepted quote can't be edited.
	if _, page, _ := public("GET", "/q/"+q.PublicToken, nil); !strings.Contains(page, "Accepted by Jane Client") || strings.Contains(page, "Accept quote</button>") {
		t.Errorf("accepted page:\n%s", page)
	}
	if c, _ := call("PUT", fmt.Sprintf("/api/v1/quotes/%d", q.ID), edit); c != http.StatusConflict {
		t.Errorf("edit accepted = %d, want 409", c)
	}

	// Deleting the invoice puts the quote back to accepted; convert makes a new one.
	if c, b := call("DELETE", fmt.Sprintf("/api/v1/invoices/%d", *q.InvoiceID), nil); c != 200 {
		t.Fatalf("delete invoice: %d %s", c, b)
	}
	_, body = call("GET", fmt.Sprintf("/api/v1/quotes/%d", q.ID), nil)
	_ = json.Unmarshal(body, &q)
	if q.Status != "accepted" || q.InvoiceID != nil {
		t.Fatalf("after invoice delete: %s %v", q.Status, q.InvoiceID)
	}
	if c, b := call("POST", fmt.Sprintf("/api/v1/quotes/%d/convert", q.ID), nil); c != 200 {
		t.Fatalf("convert: %d %s", c, b)
	}

	// A second quote: declined from the link; an expired one can't be accepted.
	code, body = call("POST", "/api/v1/quotes", map[string]any{"client_id": client.ID, "items": []map[string]any{{"description": "Audit", "quantity": 2, "unit_price": 500}}})
	var q2 store.Quote
	_ = json.Unmarshal(body, &q2)
	if code != 201 || q2.Number == q.Number {
		t.Fatalf("second quote: %d %s", code, body)
	}
	call("POST", fmt.Sprintf("/api/v1/quotes/%d/status", q2.ID), map[string]string{"status": "sent"})
	public("POST", "/q/"+q2.PublicToken+"/decline", url.Values{"reason": {"Too expensive"}})
	_, body = call("GET", fmt.Sprintf("/api/v1/quotes/%d", q2.ID), nil)
	_ = json.Unmarshal(body, &q2)
	if q2.Status != "declined" || q2.DeclineReason != "Too expensive" {
		t.Errorf("declined: %s %q", q2.Status, q2.DeclineReason)
	}
	// revising a declined quote starts over as a draft
	if c, b := call("PUT", fmt.Sprintf("/api/v1/quotes/%d", q2.ID), map[string]any{"client_id": client.ID, "items": []map[string]any{{"description": "Audit", "quantity": 2, "unit_price": 400}}}); c != 200 {
		t.Fatalf("revise declined: %d %s", c, b)
	} else {
		_ = json.Unmarshal(b, &q2)
	}
	if q2.Status != "draft" || q2.Revision != 2 {
		t.Errorf("revised declined: %s rev %d", q2.Status, q2.Revision)
	}
	past := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
	call("PUT", fmt.Sprintf("/api/v1/quotes/%d", q2.ID), map[string]any{"client_id": client.ID, "issue_date": past, "valid_until": time.Now().AddDate(0, 0, -1).Format("2006-01-02"), "items": []map[string]any{{"description": "Audit", "quantity": 1, "unit_price": 400}}})
	call("POST", fmt.Sprintf("/api/v1/quotes/%d/status", q2.ID), map[string]string{"status": "sent"})
	public("POST", "/q/"+q2.PublicToken+"/accept", url.Values{"name": {"Late"}, "agree": {"1"}})
	_, body = call("GET", fmt.Sprintf("/api/v1/quotes/%d", q2.ID), nil)
	_ = json.Unmarshal(body, &q2)
	if q2.Status != "expired" || q2.InvoiceID != nil {
		t.Errorf("late accept: %s", q2.Status)
	}
	if _, page, _ := public("GET", "/q/"+q2.PublicToken, nil); !strings.Contains(page, "expired") {
		t.Errorf("expired page does not say so")
	}

	// List filters.
	var list struct {
		Total int `json:"total"`
	}
	_, body = call("GET", "/api/v1/quotes?status=expired", nil)
	_ = json.Unmarshal(body, &list)
	if list.Total != 1 {
		t.Errorf("expired list = %s", body)
	}
}
