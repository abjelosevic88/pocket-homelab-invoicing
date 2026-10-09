package server

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// rateOn returns how many units of `to` one unit of `from` was worth on date, preferring
// the provider's official list for that day when it supports dates (CBBH), otherwise the
// stored rate. source describes where the number came from, for the UI and the log.
func (s *Server) rateOn(ctx context.Context, date, from, to string) (rate float64, source string, ok bool) {
	if from == to {
		return 1, "", true
	}
	if h, isHist := s.provider(ctx).(currency.Historical); isHist {
		if date == "" {
			date = store.Today()
		}
		rates, listDate, err := h.FetchOn(ctx, date, from, []string{to})
		if err == nil && rates[to] > 0 {
			return rates[to], h.Name() + " " + listDate, true
		}
		if err != nil {
			s.log.Warn("historical rate lookup failed, using stored rate", "provider", h.Name(), "date", date, "from", from, "to", to, "err", err)
		}
	}
	if r, ok := s.store.GetRate(ctx, from, to); ok {
		return r, "stored", true
	}
	return 0, "", false
}

func base64Encode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// refreshRates fetches rates for all enabled currencies from the provider.
// Manual rates are never overwritten.
func (s *Server) refreshRates(ctx context.Context) (int, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return 0, err
	}
	curs, err := s.store.ListCurrencies(ctx, true)
	if err != nil {
		return 0, err
	}
	var quotes []string
	for _, c := range curs {
		if c.Code != st.BaseCurrency {
			quotes = append(quotes, c.Code)
		}
	}
	if len(quotes) == 0 {
		return 0, nil
	}
	prov := s.provider(ctx)
	rates, err := prov.Fetch(ctx, st.BaseCurrency, quotes)
	source := prov.Name()
	if err != nil {
		// Provider doesn't know the base (e.g. BAM, RSD): fall back to EUR rates and
		// convert through a stored base→EUR rate (pegged currencies make this exact).
		baseToEUR, ok := s.store.GetRate(ctx, st.BaseCurrency, "EUR")
		if !ok || st.BaseCurrency == "EUR" {
			return 0, fmt.Errorf("fetch rates: %w (tip: add a manual %s→EUR rate and refresh again to derive the rest)", err, st.BaseCurrency)
		}
		eurQuotes := make([]string, 0, len(quotes))
		for _, q := range quotes {
			if q != "EUR" {
				eurQuotes = append(eurQuotes, q)
			}
		}
		eurRates, err2 := prov.Fetch(ctx, "EUR", eurQuotes)
		if err2 != nil {
			return 0, fmt.Errorf("fetch rates via EUR: %w", err2)
		}
		rates = map[string]float64{}
		for q, r := range eurRates {
			rates[q] = r * baseToEUR // base→quote = base→EUR × EUR→quote
		}
		source = prov.Name() + " via EUR"
	}
	existing, _ := s.store.ListRates(ctx, st.BaseCurrency)
	manual := map[string]bool{}
	for _, r := range existing {
		if r.Source == "manual" {
			manual[r.Quote] = true
		}
	}
	n := 0
	for quote, rate := range rates {
		if manual[quote] || rate <= 0 {
			continue
		}
		// store as quote->base (how many base units per 1 quote unit) AND base->quote for convenience
		if err := s.store.UpsertRate(ctx, store.ExchangeRate{Base: st.BaseCurrency, Quote: quote, Rate: rate, Source: source}); err != nil {
			return n, err
		}
		n++
	}
	s.log.Info("exchange rates refreshed", "base", st.BaseCurrency, "updated", n, "provider", prov.Name())
	return n, nil
}
