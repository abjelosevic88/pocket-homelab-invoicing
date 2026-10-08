package server

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

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
	rates, err := s.rates.Fetch(ctx, st.BaseCurrency, quotes)
	if err != nil {
		return 0, fmt.Errorf("fetch rates: %w", err)
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
		if err := s.store.UpsertRate(ctx, store.ExchangeRate{Base: st.BaseCurrency, Quote: quote, Rate: rate, Source: s.rates.Name()}); err != nil {
			return n, err
		}
		n++
	}
	s.log.Info("exchange rates refreshed", "base", st.BaseCurrency, "updated", n, "provider", s.rates.Name())
	return n, nil
}
