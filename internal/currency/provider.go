// Package currency fetches exchange rates from external providers.
package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Rates maps quote currency -> rate for one base currency.
type Rates map[string]float64

// Provider fetches rates.
type Provider interface {
	Name() string
	Fetch(ctx context.Context, base string, quotes []string) (Rates, error)
}

// Frankfurter uses the free ECB-backed https://frankfurter.app API (no key needed).
type Frankfurter struct {
	Client  *http.Client
	BaseURL string
}

// NewFrankfurter returns a provider using api.frankfurter.app.
func NewFrankfurter() *Frankfurter {
	return &Frankfurter{Client: &http.Client{Timeout: 15 * time.Second}, BaseURL: "https://api.frankfurter.app"}
}

// Name implements Provider.
func (f *Frankfurter) Name() string { return "frankfurter" }

// Fetch implements Provider.
func (f *Frankfurter) Fetch(ctx context.Context, base string, quotes []string) (Rates, error) {
	url := fmt.Sprintf("%s/latest?base=%s", f.BaseURL, base)
	if len(quotes) > 0 {
		url += "&symbols=" + strings.Join(quotes, ",")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pocket-invoicing")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("frankfurter: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Rates) == 0 {
		return nil, fmt.Errorf("frankfurter: no rates returned for %s", base)
	}
	return body.Rates, nil
}

// None is a provider that never fetches (fully offline / manual rates only).
type None struct{}

// Name implements Provider.
func (None) Name() string { return "none" }

// Fetch implements Provider.
func (None) Fetch(context.Context, string, []string) (Rates, error) {
	return nil, fmt.Errorf("exchange rate provider disabled")
}

// New returns a provider by name.
func New(name string) Provider {
	switch strings.ToLower(name) {
	case "frankfurter", "ecb":
		return NewFrankfurter()
	default:
		return None{}
	}
}
