package server

import "testing"

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
	ps, pe := periodFor("2026-02-01", "monthly", 1)
	if ps != "2026-02-01" || pe != "2026-02-28" {
		t.Errorf("period %s %s", ps, pe)
	}
}

func TestShortNumber(t *testing.T) {
	for in, want := range map[string]string{"INV-005-2026": "005-2026", "005-2026": "005-2026", "R-2026-01": "2026-01", "2026-001": "2026-001", "INV": "INV"} {
		if got := shortNumber(in); got != want {
			t.Errorf("shortNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
