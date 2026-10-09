package currency

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

const cbbhSample = `{"Date":"2026-10-03T00:00:00","Number":195,"CurrencyExchangeItems":[
 {"Country":"EMU","NumCode":"978","AlphaCode":"EUR","Units":"1","Buy":"1.955830","Middle":"1.955830","Sell":"1.955830","Star":null},
 {"Country":"Hungary","NumCode":"348","AlphaCode":"HUF","Units":"100","Buy":"0.532680","Middle":"0.534015","Sell":"0.535350","Star":null},
 {"Country":"USA","NumCode":"840","AlphaCode":"USD","Units":"1","Buy":"1.680000","Middle":"1.684211","Sell":"1.688422","Star":null}]}`

func TestCBBH(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/CurrencyExchange/GetJson" || r.URL.Query().Get("date") != "2026-10-04" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(cbbhSample))
	}))
	defer srv.Close()
	c := NewCBBH()
	c.BaseURL = srv.URL

	rates, date, err := c.FetchOn(context.Background(), "2026-10-04", "BAM", []string{"EUR", "USD", "HUF", "XXX"})
	if err != nil {
		t.Fatal(err)
	}
	if date != "2026-10-03" {
		t.Errorf("list date = %s, want the bank's re-dated 2026-10-03", date)
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
	if !near(rates["EUR"], 1/1.955830) || !near(rates["USD"], 1/1.684211) || !near(rates["HUF"], 100/0.534015) {
		t.Errorf("rates = %v", rates)
	}
	if _, ok := rates["XXX"]; ok {
		t.Error("unknown currency should be skipped")
	}
	// Cross rate through BAM when the base is a foreign currency.
	rates, _, err = c.FetchOn(context.Background(), "2026-10-04", "EUR", []string{"USD", "BAM"})
	if err != nil {
		t.Fatal(err)
	}
	if !near(rates["BAM"], 1.955830) || !near(rates["USD"], 1.955830/1.684211) {
		t.Errorf("EUR rates = %v", rates)
	}
	if calls != 1 {
		t.Errorf("expected the list to be cached, got %d fetches", calls)
	}
	if _, _, err := c.FetchOn(context.Background(), "2026-10-04", "XYZ", []string{"EUR"}); err == nil {
		t.Error("unknown base should fail")
	}
	if _, _, err := c.FetchOn(context.Background(), "yesterday", "BAM", []string{"EUR"}); err == nil {
		t.Error("bad date should fail")
	}
}
