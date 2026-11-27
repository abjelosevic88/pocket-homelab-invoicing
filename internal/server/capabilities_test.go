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
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// The three routes Pocket Search, Pocket Day and Pocket Hub call
// (capabilities.go): the shape of each answer, that it is the app's own data
// and numbers, that nobody sees another person's business, and that a link
// token opens exactly its one route.

// capCore is a Pocket Core that vouches for link tokens
// pcx_<owner|other>_<search|day|widget|read|write|crm> from Pocket Hub and
// remembers the manifest Invoicing registers with.
type capCore struct {
	mu       sync.Mutex
	manifest map[string]any
}

func (f *capCore) handler() http.Handler {
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget,
		"read": "invoices:read", "write": "invoices:write", "worklog": "worklog:read worklog:write"}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.mu.Lock()
			f.manifest = m
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"app":{},"token":"pca_invoicing"}`))
		case "/api/v1/auth/introspect":
			var in struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || (parts[1] != "owner" && parts[1] != "other") || scopes[parts[2]] == "" {
				_, _ = w.Write([]byte(`{"active":false}`))
				return
			}
			id := map[string]int{"owner": 1, "other": 2}[parts[1]]
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "scope": scopes[parts[2]], "audience": "invoicing",
				"user": map[string]any{"id": id, "email": parts[1] + "@example.com", "name": parts[1]},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			http.NotFound(w, r)
		}
	})
}

// capEnv is a server with Core, two people and an API token each.
type capEnv struct {
	t          *testing.T
	s          *Server
	st         *store.Store
	fc         *capCore
	url        string
	owner      string // API tokens
	other      string
	ownerID    int64
	otherID    int64
	clientIDs  map[string]int64
	invoiceIDs map[string]int64
}

func newCapEnv(t *testing.T) *capEnv {
	t.Helper()
	fc := &capCore{}
	coreSrv := httptest.NewServer(fc.handler())
	t.Cleanup(coreSrv.Close)
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.CoreURL = coreSrv.URL
	cfg.CoreRegistrationKey = "pck_test"
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e := &capEnv{t: t, s: s, st: st, fc: fc, clientIDs: map[string]int64{}, invoiceIDs: map[string]int64{}}
	for _, who := range []string{"owner", "other"} {
		u := &store.User{Email: who + "@example.com", Name: who, Role: roleAdmin, PasswordHash: "x"}
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, err := st.CreateAPIToken(ctx, u.ID, who)
		if err != nil {
			t.Fatal(err)
		}
		if who == "owner" {
			e.owner, e.ownerID = tok, u.ID
		} else {
			e.other, e.otherID = tok, u.ID
		}
	}
	if err := st.SeedCurrencies(ctx, "EUR"); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	e.url = ts.URL
	return e
}

// call makes a request with a token and decodes a JSON object answer.
func (e *capEnv) call(tok, method, path string, body any) (int, map[string]any, string) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, e.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m, string(raw)
}

// get reads a route and wants a 200.
func (e *capEnv) get(tok, path string) map[string]any {
	e.t.Helper()
	code, m, raw := e.call(tok, "GET", path, nil)
	if code != 200 {
		e.t.Fatalf("GET %s: %d %s", path, code, raw)
	}
	return m
}

// make posts to the app's own API and returns the new id.
func (e *capEnv) make(tok, path string, body map[string]any) int64 {
	e.t.Helper()
	code, m, raw := e.call(tok, "POST", path, body)
	id, _ := m["id"].(float64)
	if p, ok := m["payment"].(map[string]any); ok && id == 0 {
		id, _ = p["id"].(float64)
	}
	if code >= 300 || id == 0 {
		e.t.Fatalf("POST %s: %d %s", path, code, raw)
	}
	return int64(id)
}

func (e *capEnv) client(tok, name, currency, contact string) int64 {
	id := e.make(tok, "/api/v1/clients", map[string]any{"name": name, "currency": currency, "contact_name": contact})
	e.clientIDs[name] = id
	return id
}

func (e *capEnv) invoice(tok, key string, body map[string]any, line string, price float64) int64 {
	body["items"] = []map[string]any{{"description": line, "quantity": 1, "unit_price": price}}
	id := e.make(tok, "/api/v1/invoices", body)
	e.invoiceIDs[key] = id
	return id
}

func capItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

func capMetrics(m map[string]any) map[string]string {
	out := map[string]string{}
	l, _ := m["metrics"].([]any)
	for _, v := range l {
		row := v.(map[string]any)
		out[row["label"].(string)] = row["value"].(string)
	}
	return out
}

func i64(n int64) string { return fmt.Sprint(n) }

// seedBusiness is the owner's business around 2026-06-02 (and a little of
// the other person's, which must never show).
func seedBusiness(e *capEnv) (quoteID int64) {
	t := e.t
	acme := e.client(e.owner, "Acme d.o.o.", "EUR", "Ana Anić")
	globex := e.client(e.owner, "Globex", "USD", "")
	web := e.invoice(e.owner, "web", map[string]any{"client_id": acme, "status": "sent", "issue_date": "2026-06-02", "due_date": "2026-06-16", "notes": "Spring work",
		"number": "INV-2026-0007"}, "Website redesign", 1200)
	host := e.invoice(e.owner, "host", map[string]any{"client_id": globex, "status": "sent", "issue_date": "2026-05-01", "due_date": "2026-06-02", "exchange_rate": 0.9,
		"number": "INV-2026-0005"}, "Hosting for May", 300)
	e.invoice(e.owner, "draft", map[string]any{"client_id": acme, "status": "draft", "issue_date": "2026-06-02", "due_date": "2026-06-20", "number": "INV-2026-0008"}, "Draft idea", 50)
	e.make(e.owner, "/api/v1/payments", map[string]any{"invoice_id": web, "amount": 1200, "date": "2026-06-02", "reference": "RF-11", "method": "bank_transfer"})
	e.make(e.owner, "/api/v1/payments", map[string]any{"invoice_id": host, "amount": 100.5, "date": "2026-06-02", "method": "card"})
	quoteID = e.make(e.owner, "/api/v1/quotes", map[string]any{"client_id": acme, "issue_date": "2026-06-01", "valid_until": "2026-07-01", "number": "Q-2026-0003",
		"items": []map[string]any{{"description": "Mobile app", "quantity": 1, "unit_price": 5000}}})
	// sent in the morning, accepted late in the evening (UTC): the next day in Sarajevo
	if _, err := e.st.DB.Exec(`UPDATE quotes SET status='accepted', sent_at='2026-06-02T07:30:00.123456Z', accepted_at='2026-06-02T22:30:00Z', accepted_name='Ana' WHERE id=?`, quoteID); err != nil {
		t.Fatal(err)
	}

	// the other person's things, with the same words
	oacme := e.client(e.other, "Acme Other", "EUR", "")
	e.invoice(e.other, "other", map[string]any{"client_id": oacme, "status": "sent", "issue_date": "2026-06-02", "due_date": "2026-06-02", "notes": "Website"}, "Website redesign", 999)
	e.make(e.other, "/api/v1/payments", map[string]any{"invoice_id": e.invoiceIDs["other"], "amount": 999, "date": "2026-06-02"})
	return quoteID
}

func TestPocketSearch(t *testing.T) {
	e := newCapEnv(t)
	quoteID := seedBusiness(e)
	web, acme := e.invoiceIDs["web"], e.clientIDs["Acme d.o.o."]

	// an invoice by number, line, notes and client; paid in full
	for _, q := range []string{"0007", "redesign", "spring", "website+spring"} {
		got := capItems(t, e.get(e.owner, "/api/v1/pocket/search?q="+q))
		if len(got) != 1 || got[0]["id"] != "invoice:"+i64(web) || got[0]["kind"] != "invoice" || got[0]["title"] != "Invoice INV-2026-0007" ||
			got[0]["url"] != "/invoices/"+i64(web) || got[0]["date"] != "2026-06-02" || got[0]["snippet"] != "Acme d.o.o. · 1,200.00 EUR · paid · 2026-06-02" {
			t.Fatalf("%q: %v", q, got)
		}
	}
	// an unpaid invoice past its due date says how late it is, in its own currency
	got := capItems(t, e.get(e.owner, "/api/v1/pocket/search?q=hosting"))
	late := daysApart("2026-06-02", store.Today())
	if len(got) != 1 || got[0]["snippet"] != fmt.Sprintf("Globex · 300.00 USD · overdue %d days · 2026-05-01", late) {
		t.Fatalf("hosting: %v", got)
	}
	// a quote by its line
	got = capItems(t, e.get(e.owner, "/api/v1/pocket/search?q=mobile"))
	if len(got) != 1 || got[0]["id"] != "quote:"+i64(quoteID) || got[0]["kind"] != "quote" || got[0]["title"] != "Quote Q-2026-0003" ||
		got[0]["url"] != "/quotes/"+i64(quoteID) || got[0]["snippet"] != "Acme d.o.o. · 5,000.00 EUR · accepted · 2026-06-01" {
		t.Fatalf("mobile: %v", got)
	}
	// the client by name first, then its invoices and quotes, the newest first
	got = capItems(t, e.get(e.owner, "/api/v1/pocket/search?q=acme"))
	if len(got) != 4 || got[0]["id"] != "client:"+i64(acme) || got[0]["kind"] != "client" || got[0]["url"] != "/clients/"+i64(acme) ||
		got[0]["title"] != "Acme d.o.o." || got[0]["snippet"] != "Ana Anić · 2 invoices" ||
		got[1]["kind"] != "invoice" || got[2]["kind"] != "invoice" || got[3]["kind"] != "quote" {
		t.Fatalf("acme: %v", got)
	}
	if got := capItems(t, e.get(e.owner, "/api/v1/pocket/search?q=globex")); len(got) != 2 || got[0]["snippet"] != "1 invoice · 199.50 USD outstanding" {
		t.Fatalf("globex: %v", got)
	}
	// any case for plain letters; letters like Č as written
	celik := e.client(e.owner, "Čelik d.o.o.", "EUR", "")
	for _, q := range []string{"ACME", "Čelik", "čelik+D.O.O."} {
		got := capItems(t, e.get(e.owner, "/api/v1/pocket/search?q="+strings.ReplaceAll(q, "Č", "%C4%8C")))
		if len(got) == 0 || (strings.Contains(strings.ToLower(q), "elik") && got[0]["id"] != "client:"+i64(celik)) {
			t.Fatalf("%q: %v", q, got)
		}
	}
	// the limit holds
	if got := capItems(t, e.get(e.owner, "/api/v1/pocket/search?q=acme&limit=2")); len(got) != 2 {
		t.Fatalf("limit: %v", got)
	}
	// no letter or digit, or a LIKE wildcard: nothing
	for _, q := range []string{"%3F%3F", "*", "%25", "_", "-"} {
		if got := capItems(t, e.get(e.owner, "/api/v1/pocket/search?q="+q)); len(got) != 0 {
			t.Fatalf("%q finds %v", q, got)
		}
	}
	// the other person finds only their own
	got = capItems(t, e.get(e.other, "/api/v1/pocket/search?q=website"))
	if len(got) != 1 || !strings.Contains(got[0]["snippet"].(string), "Acme Other") {
		t.Fatalf("other's search: %v", got)
	}
	for _, q := range []string{"globex", "mobile", "0007", "spring"} {
		if got := capItems(t, e.get(e.other, "/api/v1/pocket/search?q="+q)); len(got) != 0 {
			t.Fatalf("other finds %q: %v", q, got)
		}
	}
}

func TestPocketDay(t *testing.T) {
	e := newCapEnv(t)
	quoteID := seedBusiness(e)
	web, host := e.invoiceIDs["web"], e.invoiceIDs["host"]

	d := e.get(e.owner, "/api/v1/pocket/day/2026-06-02?tz=UTC")
	if d["date"] != "2026-06-02" || d["summary"] != "Received 1,200.00 EUR and 100.50 USD from 2 clients; 1 invoice sent; 1 invoice due; 1 quote sent; 1 quote accepted" {
		t.Fatalf("summary: %v", d["summary"])
	}
	if m := capMetrics(d); m["Received"] != "1,200.00 EUR + 100.50 USD" || m["Invoiced"] != "1,200.00 EUR" || len(m) != 2 {
		t.Fatalf("metrics: %v", m)
	}
	items := capItems(t, d)
	byID := map[string]map[string]any{}
	var total float64
	for _, it := range items {
		byID[it["id"].(string)] = it
		if a, ok := it["amount"].(float64); ok {
			total += a
		}
	}
	if len(items) != 6 {
		t.Fatalf("items: %v", items)
	}
	// only payments carry money, in minor units of their own currency
	if total != 120000+10050 {
		t.Fatalf("the day's amounts add up to %v: %v", total, items)
	}
	pay := items[0]
	if pay["title"] != "Payment from Globex" && pay["title"] != "Payment from Acme d.o.o." {
		t.Fatalf("payments come first: %v", items)
	}
	for _, it := range items {
		switch it["kind"] {
		case "received":
			if it["title"] == "Payment from Acme d.o.o." && (it["amount"] != float64(120000) || it["currency"] != "EUR" || it["sub"] != "Invoice INV-2026-0007 · bank transfer · RF-11" ||
				it["url"] != "/invoices/"+i64(web) || it["done"] != true) {
				t.Fatalf("acme payment: %v", it)
			}
			if it["title"] == "Payment from Globex" && (it["amount"] != float64(10050) || it["currency"] != "USD" || it["sub"] != "Invoice INV-2026-0005 · card") {
				t.Fatalf("globex payment: %v", it)
			}
		case "issued":
			if it["id"] != "invoice:"+i64(web) || it["title"] != "Invoice INV-2026-0007 to Acme d.o.o." || it["sub"] != "1,200.00 EUR · due 2026-06-16" || it["amount"] != nil {
				t.Fatalf("issued: %v", it)
			}
		case "due":
			if it["id"] != "due:"+i64(host) || it["title"] != "Invoice INV-2026-0005 due" || it["sub"] != "Globex · 199.50 USD to pay" || it["done"] != nil || it["amount"] != nil {
				t.Fatalf("due: %v", it)
			}
		case "quote sent":
			if it["title"] != "Quote Q-2026-0003 sent to Acme d.o.o." || it["time"] != "07:30" || it["sub"] != "5,000.00 EUR · valid until 2026-07-01" || it["url"] != "/quotes/"+i64(quoteID) || it["amount"] != nil {
				t.Fatalf("quote sent: %v", it)
			}
		case "quote accepted":
			if it["title"] != "Quote Q-2026-0003 accepted by Acme d.o.o." || it["time"] != "22:30" || it["sub"] != "5,000.00 EUR · by Ana" || it["done"] != true {
				t.Fatalf("quote accepted: %v", it)
			}
		default:
			t.Fatalf("unexpected item: %v", it)
		}
	}
	if byID["invoice:"+i64(e.invoiceIDs["draft"])] != nil {
		t.Fatalf("a draft counted as issued: %v", items)
	}

	// in Sarajevo the acceptance falls on the next day, at half past midnight
	d = e.get(e.owner, "/api/v1/pocket/day/2026-06-02?tz=Europe/Sarajevo")
	if !strings.HasSuffix(d["summary"].(string), "1 quote sent") || len(capItems(t, d)) != 5 {
		t.Fatalf("Sarajevo 06-02: %v", d)
	}
	d = e.get(e.owner, "/api/v1/pocket/day/2026-06-03?tz=Europe/Sarajevo")
	if d["summary"] != "1 quote accepted" || len(capItems(t, d)) != 1 || capItems(t, d)[0]["time"] != "00:30" || capMetrics(d)["Received"] != "" {
		t.Fatalf("Sarajevo 06-03: %v", d)
	}
	// without a time zone the person's own (Settings: UTC by default)
	if d := e.get(e.owner, "/api/v1/pocket/day/2026-06-02"); len(capItems(t, d)) != 6 {
		t.Fatalf("no tz: %v", d)
	}
	// once the invoice due that day is paid, it says so
	e.make(e.owner, "/api/v1/payments", map[string]any{"invoice_id": host, "amount": 199.5, "date": "2026-06-10"})
	for _, it := range capItems(t, e.get(e.owner, "/api/v1/pocket/day/2026-06-02?tz=UTC")) {
		if it["kind"] == "due" && (it["sub"] != "Globex · paid" || it["done"] != true) {
			t.Fatalf("paid due: %v", it)
		}
	}
	// a day with nothing
	if d := e.get(e.owner, "/api/v1/pocket/day/2026-06-04"); d["summary"] != nil || len(capItems(t, d)) != 0 || d["date"] != "2026-06-04" {
		t.Fatalf("empty day: %v", d)
	}
	// a bad date
	if code, _, _ := e.call(e.owner, "GET", "/api/v1/pocket/day/2026-13-40", nil); code != 400 {
		t.Fatalf("bad date: %d", code)
	}
	// the other person's day is their own
	d = e.get(e.other, "/api/v1/pocket/day/2026-06-02")
	if d["summary"] != "Received 999.00 EUR from Acme Other; 1 invoice sent; 1 invoice due" || len(capItems(t, d)) != 3 {
		t.Fatalf("other's day: %v", d)
	}
}

func TestPocketWidget(t *testing.T) {
	e := newCapEnv(t)
	// nothing yet: nothing to show
	if w := e.get(e.owner, "/api/v1/pocket/widget"); len(capItems(t, w)) != 0 || w["line"] != nil || w["metrics"] != nil {
		t.Fatalf("empty widget: %v", w)
	}
	today := store.Today()
	day := func(n int) string {
		d, _ := time.Parse("2006-01-02", today)
		return d.AddDate(0, 0, n).Format("2006-01-02")
	}
	acme := e.client(e.owner, "Acme", "EUR", "")
	long := day(-60)
	e.invoice(e.owner, "late12", map[string]any{"client_id": acme, "status": "sent", "issue_date": long, "due_date": day(-12), "number": "A-1"}, "Old work", 400)
	late3 := e.invoice(e.owner, "late3", map[string]any{"client_id": acme, "status": "sent", "issue_date": long, "due_date": day(-3), "number": "A-2"}, "Newer work", 300)
	e.make(e.owner, "/api/v1/payments", map[string]any{"invoice_id": late3, "amount": 50, "date": day(-40)}) // partly paid, before this month
	e.invoice(e.owner, "soon", map[string]any{"client_id": acme, "status": "sent", "issue_date": long, "due_date": day(3), "number": "A-3"}, "Soon", 100)
	e.invoice(e.owner, "far", map[string]any{"client_id": acme, "status": "sent", "issue_date": long, "due_date": day(40), "number": "A-4"}, "Far", 50)
	e.invoice(e.owner, "draft", map[string]any{"client_id": acme, "status": "draft", "issue_date": today, "due_date": day(-1), "number": "A-5"}, "Draft", 70)
	paid := e.invoice(e.owner, "paid", map[string]any{"client_id": acme, "status": "sent", "issue_date": today, "due_date": day(10), "number": "A-6"}, "Paid", 80)
	e.make(e.owner, "/api/v1/payments", map[string]any{"invoice_id": paid, "amount": 80, "date": today})

	w := e.get(e.owner, "/api/v1/pocket/widget")
	if w["line"] != "2 overdue, 800.00 EUR outstanding" || w["url"] != "/invoices?status=overdue" {
		t.Fatalf("line: %v", w)
	}
	if m := capMetrics(w); m["Invoiced"] != "80.00 EUR" || m["Received"] != "80.00 EUR" || m["Outstanding"] != "800.00 EUR" || len(m) != 3 {
		t.Fatalf("metrics: %v", m)
	}
	items := capItems(t, w)
	want := []struct{ key, title, detail string }{
		{"late12", "Invoice A-1", "Acme · 12 days late · 400.00 EUR"},
		{"late3", "Invoice A-2", "Acme · 3 days late · 250.00 EUR"},
		{"soon", "Invoice A-3", "Acme · due in 3 days · 100.00 EUR"},
	}
	if len(items) != len(want) {
		t.Fatalf("items: %v", items)
	}
	for i, x := range want {
		if items[i]["title"] != x.title || items[i]["detail"] != x.detail || items[i]["url"] != "/invoices/"+i64(e.invoiceIDs[x.key]) || items[i]["id"] != i64(e.invoiceIDs[x.key]) {
			t.Fatalf("item %d: %v", i, items[i])
		}
	}
	// the numbers are the first page's
	_, dash, _ := e.call(e.owner, "GET", "/api/v1/dashboard", nil)
	if st := dash["stats"].(map[string]any); st["outstanding"] != float64(800) || st["overdue_count"] != float64(2) || st["paid_this_month"] != float64(80) {
		t.Fatalf("dashboard: %v", st)
	}
	// nobody else sees any of it
	if w := e.get(e.other, "/api/v1/pocket/widget"); len(capItems(t, w)) != 0 || w["line"] != nil || w["metrics"] != nil {
		t.Fatalf("other's widget: %v", w)
	}
	if w := e.get("pcx_other_widget", "/api/v1/pocket/widget"); len(capItems(t, w)) != 0 || w["line"] != nil {
		t.Fatalf("other's widget through the link: %v", w)
	}
	if w := e.get("pcx_owner_widget", "/api/v1/pocket/widget"); len(capItems(t, w)) != 3 {
		t.Fatalf("owner's widget through the link: %v", w)
	}
}

// A link token opens its one route and /auth/me, nothing else; the app's
// older scopes do not open the new routes.
func TestPocketLinkTokens(t *testing.T) {
	e := newCapEnv(t)
	seedBusiness(e)
	web := "/api/v1/invoices/" + i64(e.invoiceIDs["web"])
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{"pcx_owner_search", "GET", "/api/v1/pocket/search?q=acme", 200},
		{"pcx_owner_day", "GET", "/api/v1/pocket/day/2026-06-02", 200},
		{"pcx_owner_widget", "GET", "/api/v1/pocket/widget", 200},
		{"pcx_owner_search", "GET", "/api/v1/auth/me", 200},
		{"pcx_owner_day", "GET", "/api/v1/auth/me", 200},
		// another capability's route
		{"pcx_owner_search", "GET", "/api/v1/pocket/day/2026-06-02", 403},
		{"pcx_owner_search", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_owner_day", "GET", "/api/v1/pocket/search?q=acme", 403},
		{"pcx_owner_day", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_owner_widget", "GET", "/api/v1/pocket/search?q=acme", 403},
		{"pcx_owner_widget", "GET", "/api/v1/pocket/day/2026-06-02", 403},
		{"pcx_owner_day", "GET", "/api/v1/pocket/day/today", 403},
		// the rest of the app
		{"pcx_owner_search", "GET", "/api/v1/invoices?q=acme", 403},
		{"pcx_owner_search", "GET", web, 403},
		{"pcx_owner_search", "GET", "/api/v1/clients", 403},
		{"pcx_owner_search", "GET", "/api/v1/quotes", 403},
		{"pcx_owner_day", "GET", "/api/v1/payments?from=2026-06-02&to=2026-06-02", 403},
		{"pcx_owner_day", "GET", "/api/v1/invoices?from=2026-06-02&to=2026-06-02", 403},
		{"pcx_owner_widget", "GET", "/api/v1/dashboard", 403},
		{"pcx_owner_widget", "GET", "/api/v1/reports/aging", 403},
		{"pcx_owner_widget", "POST", web + "/send", 403},
		{"pcx_owner_widget", "POST", web + "/status", 403},
		{"pcx_owner_widget", "POST", "/api/v1/payments", 403},
		{"pcx_owner_widget", "POST", "/api/v1/invoices", 403},
		{"pcx_owner_widget", "DELETE", web, 403},
		{"pcx_owner_search", "GET", "/api/v1/export.json", 403},
		{"pcx_owner_search", "GET", "/api/v1/settings", 403},
		// the app's own scopes do not open the new routes, and still open their own
		{"pcx_owner_read", "GET", "/api/v1/pocket/search?q=acme", 403},
		{"pcx_owner_read", "GET", "/api/v1/pocket/day/2026-06-02", 403},
		{"pcx_owner_read", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_owner_write", "GET", "/api/v1/pocket/search?q=acme", 403},
		{"pcx_owner_write", "GET", "/api/v1/pocket/widget", 403},
		{"pcx_owner_worklog", "GET", "/api/v1/pocket/day/2026-06-02", 403},
		{"pcx_owner_read", "GET", "/api/v1/invoices", 200},
		{"pcx_owner_write", "GET", "/api/v1/clients", 200},
		{"pcx_nobody_widget", "GET", "/api/v1/pocket/widget", 401},
	} {
		if code, _, raw := e.call(c.tok, c.method, c.path, map[string]any{}); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, raw)
		}
	}
	// nothing can be kept here, and the routes only read (the router answers
	// before the link is looked at: 404 or 405)
	for _, c := range []struct{ tok, path string }{
		{"pcx_owner_search", "/api/v1/pocket/capture"}, {"pcx_owner_widget", "/api/v1/pocket/capture"},
		{"pcx_owner_search", "/api/v1/pocket/search"}, {"pcx_owner_day", "/api/v1/pocket/day/2026-06-02"}, {"pcx_owner_widget", "/api/v1/pocket/widget"},
	} {
		if code, _, raw := e.call(c.tok, "POST", c.path, map[string]any{"request_id": "req-0000000000000001", "text": "x"}); code < 400 {
			t.Errorf("%s POST %s → %d (%s)", c.tok, c.path, code, raw)
		}
	}
	// the token acts for its person only
	if got := capItems(t, e.get("pcx_owner_search", "/api/v1/pocket/search?q=globex")); len(got) != 2 {
		t.Fatalf("the owner through the link: %v", got)
	}
	for _, q := range []string{"globex", "mobile", "spring"} {
		if got := capItems(t, e.get("pcx_other_search", "/api/v1/pocket/search?q="+q)); len(got) != 0 {
			t.Fatalf("someone else through the link finds %q: %v", q, got)
		}
	}
	if d := e.get("pcx_owner_day", "/api/v1/pocket/day/2026-06-02?tz=UTC"); len(capItems(t, d)) != 6 {
		t.Fatalf("the owner's day through the link: %v", d)
	}
	if d := e.get("pcx_other_day", "/api/v1/pocket/day/2026-06-02"); len(capItems(t, d)) != 3 || strings.Contains(fmt.Sprint(d), "Globex") {
		t.Fatalf("someone else's day through the link: %v", d)
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), false}, // not declared
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/payments"), false},
		{core.ScopeWidget, req("GET", "/api/v1/dashboard"), false},
		{"invoices:read", req("GET", "/api/v1/pocket/search?q=x"), false},
		{"invoices:read", req("GET", "/api/v1/pocket/day/2026-01-02"), false},
		{"invoices:write", req("GET", "/api/v1/pocket/widget"), false},
		{"invoices:read", req("GET", "/api/v1/invoices"), true},
		{"invoices:write", req("POST", "/api/v1/invoices"), true},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Invoicing tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	e := newCapEnv(t)
	if err := e.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.fc.mu.Lock()
	m := e.fc.manifest
	e.fc.mu.Unlock()
	if m["slug"] != "invoicing" || m["category"] != "Work" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || widget == nil || m["capture"] != nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, m["capture"])
	}
	if b := search["bangs"].([]any); len(b) != 3 || b[0] != "invoice" || b[1] != "invoices" || b[2] != "quote" || len(search["kinds"].([]any)) != 3 {
		t.Fatalf("search: %v", search)
	}
	if day["label"] != "Invoicing" || day["one"] != "invoice" || day["many"] != "invoices" || day["unit"] != "amount" || widget["title"] != "Invoices" {
		t.Fatalf("day and widget: %v %v", day, widget)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"invoices:read", "invoices:write", "worklog:read", "worklog:write", "move:export", core.ScopeSearch, core.ScopeDay, core.ScopeWidget} {
		if !offered[sc] {
			t.Errorf("manifest does not offer %s", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Errorf("manifest offers %s", core.ScopeCapture)
	}
}
