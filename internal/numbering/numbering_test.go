package numbering

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	d := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"INV-{YYYY}-{SEQ:4}":      "INV-2026-0042",
		"{YY}{MM}-{SEQ}":          "2603-42",
		"{CLIENT}/{YYYY}/{SEQ:3}": "ACM/2026/042",
		"plain":                   "plain",
	}
	for in, want := range cases {
		if got := Format(in, 42, d, "ACM"); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
	if ClientCode("Acme Corp") != "ACM" || ClientCode("") != "CLI" || ClientCode("7-Eleven") != "7EL" {
		t.Fatal("client code")
	}
}
