package server

import (
	"bytes"
	"fmt"
	"html"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// renderPublicHTML wraps the invoice HTML with a small action bar (download PDF / print).
func (s *Server) renderPublicHTML(doc *pdf.Document, tmpl string, inv *store.Invoice) ([]byte, error) {
	body, err := pdf.RenderHTML(doc, tmpl)
	if err != nil {
		return nil, err
	}
	bar := fmt.Sprintf(`<div class="no-print" style="position:sticky;top:0;z-index:10;background:#111827;color:#fff;padding:10px 16px;display:flex;gap:12px;align-items:center;font-family:system-ui,sans-serif;font-size:14px">
<strong style="flex:1">%s · %s</strong>
<span style="opacity:.8">%s: %s</span>
<a href="/i/%s/pdf?download=1" style="background:%s;color:#fff;text-decoration:none;padding:8px 14px;border-radius:6px;font-weight:600">Download PDF</a>
<a href="#" onclick="window.print();return false" style="color:#fff;text-decoration:none;padding:8px 14px;border:1px solid rgba(255,255,255,.3);border-radius:6px">Print</a>
</div>`, html.EscapeString(doc.Number), html.EscapeString(doc.From.Name), html.EscapeString(doc.Label("balance_due")), html.EscapeString(doc.Balance), html.EscapeString(inv.PublicToken), html.EscapeString(doc.AccentColor))
	idx := bytes.Index(body, []byte("<body"))
	if idx < 0 {
		return append([]byte(bar), body...), nil
	}
	end := bytes.IndexByte(body[idx:], '>')
	if end < 0 {
		return body, nil
	}
	pos := idx + end + 1
	out := make([]byte, 0, len(body)+len(bar))
	out = append(out, body[:pos]...)
	out = append(out, bar...)
	out = append(out, body[pos:]...)
	return out, nil
}
