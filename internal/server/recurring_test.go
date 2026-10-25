package server

import (
	"testing"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/calendar"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func TestNextRun(t *testing.T) {
	cases := []struct {
		from, freq string
		n          int
		want       string
	}{
		{"2026-01-31", "monthly", 1, "2026-02-28"},
		{"2026-01-15", "monthly", 1, "2026-02-15"},
		{"2026-01-15", "monthly", 3, "2026-04-15"},
		{"2026-11-30", "quarterly", 1, "2027-02-28"},
		{"2026-03-01", "weekly", 2, "2026-03-15"},
		{"2026-03-01", "biweekly", 1, "2026-03-15"},
		{"2024-02-29", "yearly", 1, "2025-03-01"},
		{"2026-12-31", "daily", 1, "2027-01-01"},
	}
	for _, c := range cases {
		if got := NextRun(c.from, c.freq, c.n); got != c.want {
			t.Errorf("NextRun(%s,%s,%d)=%s want %s", c.from, c.freq, c.n, got, c.want)
		}
	}
	ps, pe := periodFor("2026-02-01", "monthly", 1, "forward")
	if ps != "2026-02-01" || pe != "2026-02-28" {
		t.Errorf("period %s %s", ps, pe)
	}
	ps, pe = periodFor("2026-11-01", "monthly", 1, "arrears")
	if ps != "2026-10-01" || pe != "2026-10-31" {
		t.Errorf("arrears period %s %s", ps, pe)
	}
	ps, pe = periodFor("2026-01-01", "quarterly", 1, "arrears")
	if ps != "2025-10-01" || pe != "2025-12-31" {
		t.Errorf("arrears quarter %s %s", ps, pe)
	}
	if got := runDateFor(ps, pe, "arrears"); got != "2026-01-01" {
		t.Errorf("runDateFor = %s", got)
	}
}

func TestRecurringItemsWorkingDays(t *testing.T) {
	st := store.DefaultSettings()
	st.HoursPerDay = 8
	st.Holidays = []calendar.Holiday{{Date: "2026-05-01", Name: "Praznik rada", Yearly: true}}
	rec := &store.RecurringInvoice{QuantityMode: "working_days", Items: []store.RecurringItem{
		{Description: "Days – {month}", Unit: "day", Quantity: 21, UnitPrice: 400},
		{Description: "Hours", Unit: "hour", Quantity: 100, UnitPrice: 40},
		{Description: "Retainer", Unit: "month", Quantity: 1, UnitPrice: 1000},
	}}
	items, source := recurringItems(rec, st, "2026-05-01", "2026-05-31")
	if source != "working_days" {
		t.Fatalf("source %s", source)
	}
	// May 2026: 21 weekdays, 1 May (Friday) is a holiday -> 20
	if items[0].Quantity != 20 || items[1].Quantity != 160 || items[2].Quantity != 1 {
		t.Fatalf("quantities %v %v %v", items[0].Quantity, items[1].Quantity, items[2].Quantity)
	}
	if items[0].Description != "Days – May 2026" {
		t.Fatalf("placeholder: %q", items[0].Description)
	}
	rec.QuantityMode = "fixed"
	items, source = recurringItems(rec, st, "2026-05-01", "2026-05-31")
	if source != "profile" || items[0].Quantity != 21 {
		t.Fatalf("fixed mode: %s %v", source, items[0].Quantity)
	}
}

func TestShortNumber(t *testing.T) {
	for in, want := range map[string]string{"INV-005-2026": "005-2026", "005-2026": "005-2026", "R-2026-01": "2026-01", "2026-001": "2026-001", "INV": "INV"} {
		if got := shortNumber(in); got != want {
			t.Errorf("shortNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
