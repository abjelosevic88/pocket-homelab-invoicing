// Package numbering renders invoice number patterns.
//
// Supported placeholders: {YYYY} {YY} {MM} {DD} {SEQ} {SEQ:n} {CLIENT}
package numbering

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var seqRe = regexp.MustCompile(`\{SEQ(?::(\d+))?\}`)

// Format renders a pattern.
func Format(pattern string, seq int64, date time.Time, clientCode string) string {
	out := pattern
	out = strings.ReplaceAll(out, "{YYYY}", date.Format("2006"))
	out = strings.ReplaceAll(out, "{YY}", date.Format("06"))
	out = strings.ReplaceAll(out, "{MM}", date.Format("01"))
	out = strings.ReplaceAll(out, "{DD}", date.Format("02"))
	out = strings.ReplaceAll(out, "{CLIENT}", clientCode)
	out = seqRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := seqRe.FindStringSubmatch(m)
		width := 0
		if len(sub) > 1 && sub[1] != "" {
			width, _ = strconv.Atoi(sub[1])
		}
		return fmt.Sprintf("%0*d", width, seq)
	})
	return out
}

// ClientCode derives a short upper-case code from a client name (e.g. "Acme Corp" -> "ACM").
func ClientCode(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() >= 3 {
				break
			}
		}
	}
	if b.Len() == 0 {
		return "CLI"
	}
	return b.String()
}
