package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CBBH fetches the official daily exchange list of the Central Bank of Bosnia and
// Herzegovina (https://www.cbbh.ba/CurrencyExchange/). Rates are published in BAM per
// N units of the foreign currency; the middle rate ("srednji kurs") is used, which is
// what accountants in BiH expect on invoices. The endpoint takes a date, so the list
// valid on an invoice's issue date can be fetched exactly. No key needed.
type CBBH struct {
	Client  *http.Client
	BaseURL string

	mu    sync.Mutex
	cache map[string]cbbhList // date -> list (the bank re-dates weekends/holidays to the last published list)
}

type cbbhList struct {
	date    string             // actual list date (YYYY-MM-DD)
	perUnit map[string]float64 // BAM per 1 unit of currency
	at      time.Time
}

// NewCBBH returns a provider using www.cbbh.ba.
func NewCBBH() *CBBH {
	return &CBBH{Client: &http.Client{Timeout: 15 * time.Second}, BaseURL: "https://www.cbbh.ba", cache: map[string]cbbhList{}}
}

// Name implements Provider.
func (c *CBBH) Name() string { return "cbbh" }

// Fetch implements Provider using today's list.
func (c *CBBH) Fetch(ctx context.Context, base string, quotes []string) (Rates, error) {
	r, _, err := c.FetchOn(ctx, time.Now().Format("2006-01-02"), base, quotes)
	return r, err
}

// FetchOn implements Historical. It returns base→quote rates valid on date and the date
// of the list actually used (the bank returns the previous list for weekends and holidays,
// and today's list for dates in the future).
func (c *CBBH) FetchOn(ctx context.Context, date, base string, quotes []string) (Rates, string, error) {
	list, err := c.list(ctx, date)
	if err != nil {
		return nil, "", err
	}
	bamPerBase, ok := list.perUnit[strings.ToUpper(base)]
	if !ok {
		return nil, "", fmt.Errorf("cbbh: no rate for base currency %s (the list quotes %d currencies against BAM)", base, len(list.perUnit)-1)
	}
	out := Rates{}
	for _, q := range quotes {
		q = strings.ToUpper(q)
		bamPerQuote, ok := list.perUnit[q]
		if !ok || bamPerQuote <= 0 {
			continue
		}
		out[q] = bamPerBase / bamPerQuote // 1 base = bamPerBase BAM = bamPerBase/bamPerQuote quote
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("cbbh: none of %s are on the list", strings.Join(quotes, ","))
	}
	return out, list.date, nil
}

func (c *CBBH) list(ctx context.Context, date string) (cbbhList, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return cbbhList{}, fmt.Errorf("cbbh: bad date %q", date)
	}
	c.mu.Lock()
	cached, ok := c.cache[date]
	c.mu.Unlock()
	// Past lists never change; today's (and re-dated) lists are re-checked hourly in case the day's list lands late.
	if ok && (cached.date == date && date < time.Now().Format("2006-01-02") || time.Since(cached.at) < time.Hour) {
		return cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/CurrencyExchange/GetJson?date="+date, nil)
	if err != nil {
		return cbbhList{}, err
	}
	req.Header.Set("User-Agent", "pocket-invoicing")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return cbbhList{}, fmt.Errorf("cbbh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cbbhList{}, fmt.Errorf("cbbh: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Date  string `json:"Date"`
		Items []struct {
			AlphaCode string `json:"AlphaCode"`
			Units     string `json:"Units"`
			Middle    string `json:"Middle"`
		} `json:"CurrencyExchangeItems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return cbbhList{}, fmt.Errorf("cbbh: decode: %w", err)
	}
	if len(body.Items) == 0 {
		return cbbhList{}, fmt.Errorf("cbbh: empty list for %s", date)
	}
	l := cbbhList{date: date, perUnit: map[string]float64{"BAM": 1}, at: time.Now()}
	if len(body.Date) >= 10 {
		l.date = body.Date[:10]
	}
	for _, it := range body.Items {
		units, err1 := strconv.ParseFloat(strings.TrimSpace(it.Units), 64)
		mid, err2 := strconv.ParseFloat(strings.TrimSpace(it.Middle), 64)
		if err1 != nil || err2 != nil || units <= 0 || mid <= 0 {
			continue
		}
		l.perUnit[strings.ToUpper(it.AlphaCode)] = mid / units
	}
	c.mu.Lock()
	c.cache[date] = l
	c.mu.Unlock()
	return l, nil
}
