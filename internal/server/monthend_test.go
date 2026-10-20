package server

import (
	"testing"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

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

func TestSuggestFromFillModes(t *testing.T) {
	defs := []store.CustomFieldDef{
		{Key: "fiscal", Fill: store.FillIncrement},
		{Key: "project", Fill: store.FillCopy},
		{Key: "po", Fill: store.FillNone},
		{Key: "words", Fill: store.FillIncrement},
		{Key: "unused", Fill: store.FillIncrement},
	}
	prev := map[string]string{"fiscal": "14/15ПП", "project": "P-12", "po": "PO-7", "words": "n/a", "other": "x"}
	last, next := suggestFrom(defs, prev)
	if len(last) != 4 || last["po"] != "PO-7" {
		t.Errorf("last = %v", last)
	}
	want := map[string]string{"fiscal": "15/16ПП", "project": "P-12"}
	if len(next) != len(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
	for k, v := range want {
		if next[k] != v {
			t.Errorf("next[%s] = %q, want %q", k, next[k], v)
		}
	}
}
