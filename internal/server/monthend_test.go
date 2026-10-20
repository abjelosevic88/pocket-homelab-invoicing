package server

import "testing"

func TestBumpCounter(t *testing.T) {
	cases := map[string]string{
		"14/15ПП":              "15/16ПП",
		"0031":                 "0032",
		"9/99":                 "10/100",
		"ELJA9MLH-ELJA9MLH-15": "ELJA9MLH-ELJA9MLH-16",
		"ELJA9MLH-ELJA9MLH-09": "ELJA9MLH-ELJA9MLH-10",
		"R-2026-007 A":         "R-2026-008 A",
		"  12  ":               "13",
		"no digits":            "",
		"":                     "",
	}
	for in, want := range cases {
		if got := bumpCounter(in); got != want {
			t.Errorf("bumpCounter(%q) = %q, want %q", in, got, want)
		}
	}
}
