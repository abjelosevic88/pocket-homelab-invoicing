package server

import (
	"bytes"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// The client's side of a quote: /q/<token> shows it with Accept and Decline,
// /q/<token>/pdf downloads it. No account and no JavaScript needed.

// publicQuote loads the quote behind the link (drafts are not shared), expires
// it when its date has passed and records the first view.
func (s *Server) publicQuote(w http.ResponseWriter, r *http.Request) *store.Quote {
	q, err := s.store.GetQuoteByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil || q.Status == store.QuoteDraft {
		http.NotFound(w, r)
		return nil
	}
	ctx := r.Context()
	if q.Open() && q.ValidUntil < store.Today() {
		s.expireQuotes(ctx, q.UserID)
		if fresh, err := s.store.GetQuote(ctx, q.UserID, q.ID); err == nil {
			q = fresh
		}
	}
	if q.ViewedAt == nil {
		if first, _ := s.store.MarkQuoteViewed(ctx, q.UserID, q.ID); first {
			s.store.LogActivity(ctx, q.UserID, "quote", q.ID, "viewed", "Opened via the link")
			if fresh, err := s.store.GetQuote(ctx, q.UserID, q.ID); err == nil {
				q = fresh
			}
			s.hooks.Emit(q.UserID, "quote.viewed", q)
		}
	}
	return q
}

func (s *Server) handlePublicQuote(w http.ResponseWriter, r *http.Request) {
	q := s.publicQuote(w, r)
	if q == nil {
		return
	}
	doc, tpl, _, _, err := s.buildDoc(r.Context(), q.AsInvoice(), q.TemplateID, true)
	if err != nil {
		s.fail(w, err, "build")
		return
	}
	body, err := pdf.RenderHTML(doc, tpl.HTML)
	if err != nil {
		s.fail(w, err, "render")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), q.UserID)
	out := injectAfterBody(body, s.quotePanel(q, doc, st, r.URL.Query().Get("e")))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

func (s *Server) handlePublicQuotePDF(w http.ResponseWriter, r *http.Request) {
	q := s.publicQuote(w, r)
	if q == nil {
		return
	}
	s.serveQuotePDF(w, r, q)
}

// clientIP is the address the answer came from (behind a trusted proxy, RealIP has set it).
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	for utf8.RuneCountInString(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

func (s *Server) handlePublicQuoteAccept(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	back := "/q/" + token
	q, err := s.store.GetQuoteByToken(r.Context(), token)
	if err != nil || q.Status == store.QuoteDraft {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, back+"?e=form", http.StatusSeeOther)
		return
	}
	name, po := clip(r.PostForm.Get("name"), 200), clip(r.PostForm.Get("po"), 100)
	if name == "" {
		http.Redirect(w, r, back+"?e=name", http.StatusSeeOther)
		return
	}
	if r.PostForm.Get("agree") == "" {
		http.Redirect(w, r, back+"?e=agree", http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	if q.Open() && q.ValidUntil < store.Today() {
		s.expireQuotes(ctx, q.UserID)
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	ok, err := s.store.AcceptQuote(ctx, q.UserID, q.ID, name, po, clientIP(r))
	if err != nil {
		s.fail(w, err, "accept quote")
		return
	}
	if ok {
		s.store.LogActivity(ctx, q.UserID, "quote", q.ID, "accepted", "Accepted by "+name+" via the link")
		acc, _ := s.store.GetQuote(ctx, q.UserID, q.ID)
		if acc != nil {
			s.hooks.Emit(q.UserID, "quote.accepted", acc)
		}
		// The draft invoice is only database work; the email to the person goes
		// out in the background so a slow mail server never keeps the client waiting.
		inv, err := s.invoiceFromQuote(ctx, q.UserID, q.ID)
		if err != nil {
			s.log.Error("quote: draft invoice", "quote", q.Number, "err", err)
			s.store.LogActivity(ctx, q.UserID, "quote", q.ID, "invoice_failed", "Could not make the draft invoice: "+err.Error())
		}
		if fresh, err := s.store.GetQuote(ctx, q.UserID, q.ID); err == nil {
			go s.notifyQuoteAnswer(fresh, inv)
		}
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) handlePublicQuoteDecline(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	back := "/q/" + token
	q, err := s.store.GetQuoteByToken(r.Context(), token)
	if err != nil || q.Status == store.QuoteDraft {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	_ = r.ParseForm()
	reason := clip(r.PostForm.Get("reason"), 2000)
	ctx := r.Context()
	ok, err := s.store.DeclineQuote(ctx, q.UserID, q.ID, reason)
	if err != nil {
		s.fail(w, err, "decline quote")
		return
	}
	if ok {
		s.store.LogActivity(ctx, q.UserID, "quote", q.ID, "declined", "Declined via the link"+reasonSuffix(reason))
		if fresh, err := s.store.GetQuote(ctx, q.UserID, q.ID); err == nil {
			s.hooks.Emit(q.UserID, "quote.declined", fresh)
			go s.notifyQuoteAnswer(fresh, nil)
		}
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// injectAfterBody puts extra markup right after the template's <body> tag.
func injectAfterBody(body []byte, extra string) []byte {
	idx := bytes.Index(body, []byte("<body"))
	if idx < 0 {
		return append([]byte(extra), body...)
	}
	end := bytes.IndexByte(body[idx:], '>')
	if end < 0 {
		return body
	}
	pos := idx + end + 1
	out := make([]byte, 0, len(body)+len(extra))
	out = append(out, body[:pos]...)
	out = append(out, extra...)
	return append(out, body[pos:]...)
}

const quotePanelCSS = `<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
.pq{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;font-size:15px;line-height:1.45;color:#111827}
.pq *{box-sizing:border-box}
.pq-bar{position:sticky;top:0;z-index:10;background:#111827;color:#fff;padding:10px 16px;display:flex;flex-wrap:wrap;gap:8px 12px;align-items:center;font-size:14px}
.pq-bar strong{flex:1;min-width:180px}
.pq-bar a{color:#fff;text-decoration:none;padding:7px 12px;border:1px solid rgba(255,255,255,.3);border-radius:6px;white-space:nowrap}
.pq-card{max-width:820px;margin:16px auto;padding:16px 18px;border:1px solid #e5e7eb;border-radius:10px;background:#fff;box-shadow:0 1px 2px rgba(0,0,0,.04)}
.pq-card h2{margin:0 0 4px;font-size:17px}
.pq-muted{color:#6b7280;font-size:13.5px}
.pq-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin:12px 0}
.pq label{display:block;font-size:13px;font-weight:600;color:#374151}
.pq input[type=text],.pq textarea{display:block;width:100%;margin-top:4px;padding:9px 10px;font:inherit;border:1px solid #d1d5db;border-radius:7px;background:#fff;color:#111827}
.pq textarea{min-height:70px}
.pq .pq-check{display:flex;gap:8px;align-items:flex-start;font-weight:500;margin:4px 0 12px}
.pq .pq-check input{margin-top:3px}
.pq button{font:inherit;font-weight:650;padding:10px 18px;border-radius:7px;border:0;cursor:pointer}
.pq .pq-yes{color:#fff}
.pq .pq-no{background:#fff;color:#b91c1c;border:1px solid #fca5a5}
.pq details{margin-top:12px}
.pq summary{cursor:pointer;color:#6b7280;font-size:14px}
.pq-note{padding:12px 14px;border-radius:8px;font-weight:500}
.pq-ok{background:#ecfdf5;color:#065f46;border:1px solid #a7f3d0}
.pq-off{background:#f9fafb;color:#374151;border:1px solid #e5e7eb}
.pq-err{background:#fef2f2;color:#991b1b;border:1px solid #fecaca;margin-bottom:10px}
@media (max-width:600px){.pq-grid{grid-template-columns:1fr}.pq-card{margin:10px;padding:14px}.pq button{width:100%}}
@media print{.pq{display:none!important}}
</style>`

// quotePanel is the bar and the answer card shown above the quote.
func (s *Server) quotePanel(q *store.Quote, doc *pdf.Document, st store.Settings, errCode string) string {
	e := html.EscapeString
	company := firstNonEmpty(doc.From.Name, "us")
	accent := firstNonEmpty(doc.AccentColor, "#2563eb")
	var b strings.Builder
	b.WriteString(quotePanelCSS)
	b.WriteString(`<div class="pq no-print">`)
	rev := ""
	if q.Revision > 1 {
		rev = fmt.Sprintf(" · revision %d", q.Revision)
	}
	fmt.Fprintf(&b, `<div class="pq-bar"><strong>Quote %s · %s%s</strong><span style="opacity:.85">%s: %s</span><a href="/q/%s/pdf?download=1">Download PDF</a></div>`,
		e(q.Number), e(company), e(rev), e(doc.Label("total")), e(doc.Total), e(q.PublicToken))
	b.WriteString(`<div class="pq-card">`)
	when := func(p *string) string {
		if p == nil || len(*p) < 10 {
			return ""
		}
		return fmtDay(st.DateFormat, (*p)[:10])
	}
	switch q.Status {
	case store.QuoteAccepted, store.QuoteInvoiced:
		fmt.Fprintf(&b, `<div class="pq-note pq-ok">✓ Accepted by %s on %s. Thank you — %s will be in touch.</div>`, e(q.AcceptedName), e(when(q.AcceptedAt)), e(company))
	case store.QuoteDeclined:
		fmt.Fprintf(&b, `<div class="pq-note pq-off">This quote was declined on %s. If you change your mind, ask %s for a new one.</div>`, e(when(q.DeclinedAt)), e(company))
	case store.QuoteExpired:
		fmt.Fprintf(&b, `<div class="pq-note pq-off">This quote expired on %s. Ask %s for a new one.</div>`, e(fmtDay(st.DateFormat, q.ValidUntil)), e(company))
	default:
		switch errCode {
		case "name":
			b.WriteString(`<div class="pq-note pq-err">Please type your name to accept.</div>`)
		case "agree":
			b.WriteString(`<div class="pq-note pq-err">Please tick the box to confirm you accept the quote.</div>`)
		case "form":
			b.WriteString(`<div class="pq-note pq-err">Something went wrong sending the form. Please try again.</div>`)
		}
		fmt.Fprintf(&b, `<h2>Accept this quote?</h2><div class="pq-muted">%s for %s, valid until %s.</div>`, e(doc.Total), e(q.ClientName), e(fmtDay(st.DateFormat, q.ValidUntil)))
		fmt.Fprintf(&b, `<form method="post" action="/q/%s/accept"><div class="pq-grid"><label>Your name<input type="text" name="name" required maxlength="200" autocomplete="name"></label><label>PO / reference (optional)<input type="text" name="po" maxlength="100" value="%s"></label></div>`, e(q.PublicToken), e(q.PONumber))
		fmt.Fprintf(&b, `<label class="pq-check"><input type="checkbox" name="agree" value="1" required> I accept quote %s and its terms on behalf of %s.</label>`, e(q.Number), e(q.ClientName))
		fmt.Fprintf(&b, `<button type="submit" class="pq-yes" style="background:%s">Accept quote</button></form>`, e(accent))
		fmt.Fprintf(&b, `<details><summary>Decline this quote</summary><form method="post" action="/q/%s/decline" style="margin-top:10px"><label>Reason (optional)<textarea name="reason" maxlength="2000" placeholder="Let %s know why"></textarea></label><div style="margin-top:10px"><button type="submit" class="pq-no">Decline quote</button></div></form></details>`, e(q.PublicToken), e(company))
	}
	b.WriteString(`</div></div>`)
	return b.String()
}
