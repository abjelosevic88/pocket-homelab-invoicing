package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/mailer"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/numbering"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// Quotes: written and sent like invoices, answered by the client from the
// public link (/q/<token>). Accepting one starts a draft invoice with the same
// lines for the person to check and send; nothing is ever sent on its own.

// quoteInput is the JSON body for create/update.
type quoteInput struct {
	Number        string              `json:"number"`
	ClientID      int64               `json:"client_id"`
	IssueDate     string              `json:"issue_date"`
	ValidUntil    string              `json:"valid_until"`
	Currency      string              `json:"currency"`
	BillingMode   string              `json:"billing_mode"`
	PONumber      string              `json:"po_number"`
	DiscountType  string              `json:"discount_type"`
	DiscountValue float64             `json:"discount_value"`
	Notes         string              `json:"notes"`
	Terms         string              `json:"terms"`
	Footer        string              `json:"footer"`
	TemplateID    *int64              `json:"template_id"`
	Items         []store.InvoiceItem `json:"items"`
}

// quoteConvertMu keeps two acceptances (the client's click and the person's
// button) from starting two invoices for one quote.
var quoteConvertMu sync.Mutex

func (s *Server) applyQuoteInput(ctx context.Context, uid int64, q *store.Quote, in quoteInput, st store.Settings) error {
	client, err := s.store.GetClient(ctx, uid, in.ClientID)
	if err != nil {
		return fmt.Errorf("client not found")
	}
	q.ClientID, q.ClientName = client.ID, client.Name
	q.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if q.Currency == "" {
		q.Currency = firstNonEmpty(client.Currency, st.BaseCurrency)
	}
	q.IssueDate = in.IssueDate
	if _, err := time.Parse("2006-01-02", q.IssueDate); err != nil {
		q.IssueDate = store.Today()
	}
	q.ValidUntil = in.ValidUntil
	if _, err := time.Parse("2006-01-02", q.ValidUntil); err != nil {
		days := st.QuoteValidDays
		if days <= 0 {
			days = 30
		}
		t, _ := time.Parse("2006-01-02", q.IssueDate)
		q.ValidUntil = t.AddDate(0, 0, days).Format("2006-01-02")
	}
	if q.ValidUntil < q.IssueDate {
		return fmt.Errorf("valid until must not be before the quote date")
	}
	q.BillingMode = firstNonEmpty(in.BillingMode, client.BillingMode, "hourly")
	q.PONumber = strings.TrimSpace(in.PONumber)
	q.DiscountType = in.DiscountType
	switch q.DiscountType {
	case "percent", "fixed":
	default:
		q.DiscountType = "none"
	}
	q.DiscountValue = in.DiscountValue
	q.Notes, q.Terms, q.Footer = in.Notes, in.Terms, in.Footer
	q.TemplateID = s.ownTemplateID(ctx, uid, in.TemplateID)

	cur := s.store.GetCurrency(ctx, q.Currency)
	lines := make([]money.LineItem, 0, len(in.Items))
	q.Items = q.Items[:0]
	for _, it := range in.Items {
		if strings.TrimSpace(it.Description) == "" && it.Quantity == 0 && it.UnitPrice == 0 {
			continue
		}
		if it.Unit == "" {
			it.Unit = q.BillingMode
			if it.Unit == "" || it.Unit == "fixed" {
				it.Unit = "unit"
			}
		}
		lines = append(lines, money.LineItem{Quantity: it.Quantity, UnitPrice: it.UnitPrice, Discount: it.Discount, TaxRate: it.TaxRate})
		q.Items = append(q.Items, store.InvoiceItem{Description: it.Description, Unit: it.Unit, Quantity: it.Quantity, UnitPrice: it.UnitPrice, TaxRate: it.TaxRate, Discount: it.Discount})
	}
	if len(q.Items) == 0 {
		return fmt.Errorf("add at least one line")
	}
	tot := money.Compute(lines, q.DiscountType, q.DiscountValue, cur.Decimals)
	for i := range q.Items {
		q.Items[i].LineTotal = tot.LineTotals[i]
	}
	q.Subtotal, q.DiscountTotal, q.TaxTotal, q.Total = tot.Subtotal, tot.DiscountTotal, tot.TaxTotal, tot.Total
	return nil
}

// quoteSeq returns the sequence number the next quote takes in the year of t.
func quoteSeq(st *store.Settings, t time.Time) int64 {
	seq := st.QuoteNextSeq
	if st.InvoiceSeqResetYr && st.QuoteSeqYear != 0 && st.QuoteSeqYear != t.Year() {
		seq = 1
	}
	if seq < 1 {
		seq = 1
	}
	return seq
}

func quoteFormat(st *store.Settings) string {
	if strings.TrimSpace(st.QuoteNumberFmt) == "" {
		return "Q-{YYYY}-{SEQ:4}"
	}
	return st.QuoteNumberFmt
}

// nextQuoteNumber allocates the next quote number and bumps the quote sequence.
func (s *Server) nextQuoteNumber(ctx context.Context, uid int64, st *store.Settings, issueDate, clientName string) (string, error) {
	t, err := time.Parse("2006-01-02", issueDate)
	if err != nil {
		t = time.Now()
	}
	st.QuoteNextSeq = quoteSeq(st, t)
	st.QuoteSeqYear = t.Year()
	for tries := 0; tries < 1000; tries++ {
		n := numbering.Format(quoteFormat(st), st.QuoteNextSeq, t, numbering.ClientCode(clientName))
		exists, err := s.store.QuoteNumberExists(ctx, uid, n, 0)
		if err != nil {
			return "", err
		}
		st.QuoteNextSeq++
		if !exists {
			return n, s.store.SaveSettings(ctx, uid, *st)
		}
	}
	return "", fmt.Errorf("could not allocate a quote number")
}

func (s *Server) handleNextQuoteNumber(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), uidOf(r))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	t := time.Now()
	writeJSON(w, http.StatusOK, map[string]string{"number": numbering.Format(quoteFormat(&st), quoteSeq(&st, t), t, "CLI")})
}

func (s *Server) handleListQuotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, total, err := s.store.ListQuotes(r.Context(), uidOf(r), store.QuoteFilter{Status: q.Get("status"), ClientID: qInt64(r, "client_id"), Search: q.Get("q"), Limit: qInt(r, "limit", 50), Offset: qInt(r, "offset", 0)})
	if err != nil {
		s.fail(w, err, "list quotes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

func (s *Server) handleGetQuote(w http.ResponseWriter, r *http.Request) {
	q, err := s.store.GetQuote(r.Context(), uidOf(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (s *Server) handleCreateQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	var in quoteInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	q := &store.Quote{Status: store.QuoteDraft}
	if err := s.applyQuoteInput(ctx, uid, q, in, st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if n := strings.TrimSpace(in.Number); n != "" {
		if taken, _ := s.store.QuoteNumberExists(ctx, uid, n, 0); taken {
			writeErr(w, http.StatusConflict, "you already have a quote with that number")
			return
		}
		q.Number = n
	} else if q.Number, err = s.nextQuoteNumber(ctx, uid, &st, q.IssueDate, q.ClientName); err != nil {
		s.fail(w, err, "allocate number")
		return
	}
	if err := s.store.CreateQuote(ctx, uid, q); err != nil {
		s.fail(w, err, "create quote")
		return
	}
	s.store.LogActivity(ctx, uid, "quote", q.ID, "created", "Quote "+q.Number+" created")
	full, _ := s.store.GetQuote(ctx, uid, q.ID)
	s.hooks.Emit(uid, "quote.created", full)
	writeJSON(w, http.StatusCreated, full)
}

func (s *Server) handleUpdateQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	q, err := s.store.GetQuote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	if q.Status == store.QuoteAccepted || q.Status == store.QuoteInvoiced {
		writeErr(w, http.StatusConflict, "the client accepted this quote, so it can no longer be changed (duplicate it to make a new one)")
		return
	}
	var in quoteInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if err := s.applyQuoteInput(ctx, uid, q, in, st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if n := strings.TrimSpace(in.Number); n != "" && n != q.Number {
		if taken, _ := s.store.QuoteNumberExists(ctx, uid, n, q.ID); taken {
			writeErr(w, http.StatusConflict, "you already have a quote with that number")
			return
		}
		q.Number = n
	}
	msg := "Quote " + q.Number + " updated"
	switch q.Status {
	case store.QuoteSent, store.QuoteViewed:
		// The client may already have read it: the link shows this new revision.
		q.Revision++
		msg = fmt.Sprintf("Revised to revision %d (the link shows it)", q.Revision)
	case store.QuoteDeclined, store.QuoteExpired:
		// A new revision starts over as a draft, to be sent again.
		q.Revision++
		q.Status = store.QuoteDraft
		q.SentAt, q.ViewedAt, q.DeclinedAt, q.DeclineReason = nil, nil, nil, ""
		msg = fmt.Sprintf("Revised to revision %d, back to draft", q.Revision)
	}
	if err := s.store.UpdateQuote(ctx, uid, q); err != nil {
		s.fail(w, err, "update quote")
		return
	}
	s.store.LogActivity(ctx, uid, "quote", q.ID, "updated", msg)
	full, _ := s.store.GetQuote(ctx, uid, q.ID)
	s.hooks.Emit(uid, "quote.updated", full)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	q, err := s.store.GetQuote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	if err := s.store.DeleteQuote(ctx, uid, q.ID); err != nil {
		s.fail(w, err, "delete quote")
		return
	}
	s.store.LogActivity(ctx, uid, "quote", q.ID, "deleted", "Quote "+q.Number+" deleted")
	s.hooks.Emit(uid, "quote.deleted", map[string]any{"id": q.ID, "number": q.Number})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDuplicateQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	src, err := s.store.GetQuote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	in := quoteInput{ClientID: src.ClientID, Currency: src.Currency, BillingMode: src.BillingMode, PONumber: src.PONumber, DiscountType: src.DiscountType, DiscountValue: src.DiscountValue, Notes: src.Notes, Terms: src.Terms, Footer: src.Footer, TemplateID: src.TemplateID, Items: src.Items}
	q := &store.Quote{Status: store.QuoteDraft}
	if err := s.applyQuoteInput(ctx, uid, q, in, st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if q.Number, err = s.nextQuoteNumber(ctx, uid, &st, q.IssueDate, q.ClientName); err != nil {
		s.fail(w, err, "allocate number")
		return
	}
	if err := s.store.CreateQuote(ctx, uid, q); err != nil {
		s.fail(w, err, "create quote")
		return
	}
	s.store.LogActivity(ctx, uid, "quote", q.ID, "created", "Duplicated from "+src.Number)
	full, _ := s.store.GetQuote(ctx, uid, q.ID)
	s.hooks.Emit(uid, "quote.created", full)
	writeJSON(w, http.StatusCreated, full)
}

// handleQuoteStatus covers the hand-made changes: mark sent (without email),
// back to draft, and mark declined (the client said no by phone).
func (s *Server) handleQuoteStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	q, err := s.store.GetQuote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Status {
	case store.QuoteSent:
		if q.Status != store.QuoteDraft {
			writeErr(w, http.StatusConflict, "only a draft can be marked sent")
			return
		}
		err = s.store.MarkQuoteSent(ctx, uid, q.ID)
		s.store.LogActivity(ctx, uid, "quote", q.ID, "status", "Marked as sent")
	case store.QuoteDraft:
		if q.Status == store.QuoteAccepted || q.Status == store.QuoteInvoiced {
			writeErr(w, http.StatusConflict, "an accepted quote can't go back to draft")
			return
		}
		err = s.store.SetQuoteStatus(ctx, uid, q.ID, store.QuoteDraft)
		s.store.LogActivity(ctx, uid, "quote", q.ID, "status", "Back to draft")
	case store.QuoteDeclined:
		var ok bool
		ok, err = s.store.DeclineQuote(ctx, uid, q.ID, strings.TrimSpace(in.Reason), store.QuoteDraft, store.QuoteSent, store.QuoteViewed, store.QuoteExpired)
		if err == nil && !ok {
			writeErr(w, http.StatusConflict, "this quote can't be marked declined now")
			return
		}
		s.store.LogActivity(ctx, uid, "quote", q.ID, "declined", "Marked as declined"+reasonSuffix(in.Reason))
	default:
		writeErr(w, http.StatusBadRequest, "status must be sent, draft or declined")
		return
	}
	if err != nil {
		s.fail(w, err, "update quote")
		return
	}
	full, _ := s.store.GetQuote(ctx, uid, q.ID)
	s.hooks.Emit(uid, "quote."+full.Status, full)
	writeJSON(w, http.StatusOK, full)
}

func reasonSuffix(reason string) string {
	if r := strings.TrimSpace(reason); r != "" {
		return ": " + r
	}
	return ""
}

// handleConvertQuote is the person's "Accept & make invoice": it marks the
// quote accepted when the client said yes some other way, then starts the
// draft invoice (also for an accepted quote whose invoice was deleted).
func (s *Server) handleConvertQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	q, err := s.store.GetQuote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	if q.Status == store.QuoteInvoiced && q.InvoiceID != nil {
		writeErr(w, http.StatusConflict, "invoice "+q.InvoiceNumber+" was already made from this quote")
		return
	}
	if q.Status != store.QuoteAccepted {
		u := userFrom(ctx)
		name := "you"
		if u != nil {
			name = firstNonEmpty(u.Name, u.Email, name)
		}
		ok, err := s.store.AcceptQuote(ctx, uid, q.ID, name, "", "", store.QuoteDraft, store.QuoteSent, store.QuoteViewed, store.QuoteDeclined, store.QuoteExpired)
		if err != nil {
			s.fail(w, err, "accept quote")
			return
		}
		if ok {
			s.store.LogActivity(ctx, uid, "quote", q.ID, "accepted", "Marked as accepted by "+name)
			if acc, err := s.store.GetQuote(ctx, uid, q.ID); err == nil {
				s.hooks.Emit(uid, "quote.accepted", acc)
			}
		}
	}
	inv, err := s.invoiceFromQuote(ctx, uid, q.ID)
	if err != nil {
		s.fail(w, err, "make invoice")
		return
	}
	full, _ := s.store.GetQuote(ctx, uid, q.ID)
	writeJSON(w, http.StatusOK, map[string]any{"quote": full, "invoice": inv})
}

// invoiceFromQuote starts a draft invoice with the accepted quote's lines and
// links it to the quote. It never sends anything. Returns the existing
// invoice when the quote already has one.
func (s *Server) invoiceFromQuote(ctx context.Context, uid, quoteID int64) (*store.Invoice, error) {
	quoteConvertMu.Lock()
	defer quoteConvertMu.Unlock()
	q, err := s.store.GetQuote(ctx, uid, quoteID)
	if err != nil {
		return nil, err
	}
	if q.InvoiceID != nil {
		return s.store.GetInvoice(ctx, uid, *q.InvoiceID)
	}
	if q.Status != store.QuoteAccepted {
		return nil, fmt.Errorf("quote %s is not accepted", q.Number)
	}
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return nil, err
	}
	client, err := s.store.GetClient(ctx, uid, q.ClientID)
	if err != nil {
		return nil, err
	}
	items := make([]store.InvoiceItem, len(q.Items))
	copy(items, q.Items)
	for i := range items {
		items[i].ID, items[i].InvoiceID = 0, 0
	}
	in := invoiceInput{
		ClientID: q.ClientID, Currency: q.Currency, BillingMode: q.BillingMode, PONumber: q.PONumber,
		DiscountType: q.DiscountType, DiscountValue: q.DiscountValue, Items: items,
		Notes: st.DefaultNotes, Footer: st.DefaultFooter,
		Terms: strings.ReplaceAll(st.DefaultTerms, "{due_days}", fmt.Sprint(firstNonZero(client.PaymentTermsDays, st.DefaultDueDays))),
	}
	inv := &store.Invoice{Status: store.StatusDraft}
	if err := s.applyInput(ctx, uid, inv, in, st); err != nil {
		return nil, err
	}
	if inv.Number, err = s.nextNumber(ctx, uid, &st, inv.IssueDate, inv.ClientName); err != nil {
		return nil, err
	}
	if err := s.store.CreateInvoice(ctx, uid, inv, s.invoiceEvent(ctx, uid, "created", inv)); err != nil {
		return nil, err
	}
	if err := s.store.LinkQuoteInvoice(ctx, uid, q.ID, inv.ID); err != nil {
		return nil, err
	}
	s.store.LogActivity(ctx, uid, "invoice", inv.ID, "created", "Draft made from accepted quote "+q.Number)
	s.store.LogActivity(ctx, uid, "quote", q.ID, "invoiced", "Draft invoice "+inv.Number+" made from it")
	full, err := s.store.GetInvoice(ctx, uid, inv.ID)
	if err != nil {
		return nil, err
	}
	s.hooks.Emit(uid, "invoice.created", full)
	if qq, err := s.store.GetQuote(ctx, uid, q.ID); err == nil {
		s.hooks.Emit(uid, "quote.invoiced", qq)
	}
	return full, nil
}

// ---- PDF / HTML ----

func (s *Server) renderQuotePDF(ctx context.Context, q *store.Quote) ([]byte, error) {
	doc, tpl, logo, logoType, err := s.buildDoc(ctx, q.AsInvoice(), q.TemplateID, true)
	if err != nil {
		return nil, err
	}
	return s.renderDoc(ctx, doc, tpl, logo, logoType)
}

func (s *Server) serveQuotePDF(w http.ResponseWriter, r *http.Request, q *store.Quote) {
	b, err := s.renderQuotePDF(r.Context(), q)
	if err != nil {
		s.fail(w, err, "render pdf")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s.pdf"`, disposition, safeFilename(q.Number)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func (s *Server) handleQuotePDF(w http.ResponseWriter, r *http.Request) {
	q, err := s.store.GetQuote(r.Context(), uidOf(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	s.serveQuotePDF(w, r, q)
}

// ---- email ----

func smtpConfig(st store.Settings) mailer.Config {
	cfg := mailer.Config{Host: st.SMTPHost, Port: st.SMTPPort, User: st.SMTPUser, Password: st.SMTPPassword, From: st.SMTPFrom, FromName: st.SMTPFromName, TLS: st.SMTPTLS, BCC: st.SMTPBCC}
	if cfg.FromName == "" {
		cfg.FromName = st.CompanyName
	}
	return cfg
}

func fmtDay(layout, day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil || layout == "" {
		return day
	}
	return t.Format(layout)
}

func (s *Server) quoteVars(ctx context.Context, q *store.Quote, st store.Settings, client *store.Client) map[string]string {
	cur := s.store.GetCurrency(ctx, q.Currency)
	sym, pos := cur.Symbol, "before"
	if strings.HasSuffix(sym, " ") {
		sym, pos = strings.TrimSpace(sym), "after"
	}
	contact := firstNonEmpty(client.ContactName, client.Name)
	return map[string]string{
		"{number}": q.Number, "{number_short}": shortNumber(q.Number), "{company}": st.CompanyName, "{client}": client.Name,
		"{contact}": contact, "{first_name}": strings.Fields(contact + " ")[0],
		"{total}": money.Format(q.Total, cur.Decimals, sym, pos), "{valid_until}": fmtDay(st.DateFormat, q.ValidUntil),
		"{issue_date}": fmtDay(st.DateFormat, q.IssueDate), "{link}": s.cfg.BaseURL + "/q/" + q.PublicToken,
	}
}

// sendQuoteEmail emails the quote with its PDF and the link where the client answers it.
func (s *Server) sendQuoteEmail(ctx context.Context, q *store.Quote, to, subject, body string) (string, error) {
	uid := q.UserID
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return "", err
	}
	if st.SMTPHost == "" {
		return "", fmt.Errorf("email is not set up (Settings → Email)")
	}
	client, err := s.store.GetClient(ctx, uid, q.ClientID)
	if err != nil {
		return "", err
	}
	to = firstNonEmpty(strings.TrimSpace(to), client.Email)
	if to == "" {
		return "", fmt.Errorf("the client has no email address")
	}
	subject = firstNonEmpty(subject, st.QuoteEmailSubject, store.DefaultSettings().QuoteEmailSubject)
	body = firstNonEmpty(body, st.QuoteEmailBody, store.DefaultSettings().QuoteEmailBody)
	for k, v := range s.quoteVars(ctx, q, st, client) {
		subject = strings.ReplaceAll(subject, k, v)
		body = strings.ReplaceAll(body, k, v)
	}
	if client.EmailCC != "" {
		to += "," + client.EmailCC
	}
	pdfBytes, err := s.renderQuotePDF(ctx, q)
	if err != nil {
		return "", err
	}
	var recipients []string
	for _, a := range strings.Split(to, ",") {
		if a = strings.TrimSpace(a); a != "" {
			recipients = append(recipients, a)
		}
	}
	msg := mailer.Message{To: recipients, Subject: subject, Body: body, Attachments: []mailer.Attachment{{Filename: safeFilename(q.Number) + ".pdf", ContentType: "application/pdf", Data: pdfBytes}}}
	if err := mailer.Send(smtpConfig(st), msg); err != nil {
		return "", err
	}
	return to, s.store.MarkQuoteSent(ctx, uid, q.ID)
}

func (s *Server) handleSendQuote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := uidOf(r)
	q, err := s.store.GetQuote(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get quote")
		return
	}
	if q.Status == store.QuoteAccepted || q.Status == store.QuoteInvoiced {
		writeErr(w, http.StatusConflict, "this quote is already accepted")
		return
	}
	if q.Status == store.QuoteDeclined || q.Status == store.QuoteExpired {
		writeErr(w, http.StatusConflict, "edit the quote first: a revised quote can be sent again")
		return
	}
	var in struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	_ = decode(r, &in)
	to, err := s.sendQuoteEmail(ctx, q, in.To, in.Subject, in.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	s.store.LogActivity(ctx, uid, "quote", q.ID, "sent", "Emailed to "+to)
	full, _ := s.store.GetQuote(ctx, uid, q.ID)
	s.hooks.Emit(uid, "quote.sent", full)
	writeJSON(w, http.StatusOK, full)
}

// notifyQuoteAnswer emails the person when a client accepts or declines from
// the link (with their own SMTP settings; nothing when email isn't set up).
func (s *Server) notifyQuoteAnswer(q *store.Quote, inv *store.Invoice) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := s.store.GetSettings(ctx, q.UserID)
	if err != nil || st.SMTPHost == "" {
		return
	}
	to := st.CompanyEmail
	if u, err := s.store.GetUser(ctx, q.UserID); err == nil && strings.Contains(u.Email, "@") {
		to = u.Email
	}
	to = firstNonEmpty(to, st.SMTPFrom)
	if to == "" {
		return
	}
	var subject, body string
	link := s.cfg.BaseURL + fmt.Sprintf("/quotes/%d", q.ID)
	if q.Status == store.QuoteDeclined {
		subject = fmt.Sprintf("Quote %s declined by %s", q.Number, q.ClientName)
		body = fmt.Sprintf("%s declined quote %s.\n", q.ClientName, q.Number)
		if q.DeclineReason != "" {
			body += "\nTheir reason: " + q.DeclineReason + "\n"
		}
		body += "\nThe quote: " + link + "\n"
	} else {
		subject = fmt.Sprintf("Quote %s accepted by %s", q.Number, q.ClientName)
		body = fmt.Sprintf("%s (%s) accepted quote %s.\n", q.AcceptedName, q.ClientName, q.Number)
		if q.PONumber != "" {
			body += "Their PO / reference: " + q.PONumber + "\n"
		}
		if inv != nil {
			body += fmt.Sprintf("\nDraft invoice %s is ready for you to check and send: %s/invoices/%d\n", inv.Number, s.cfg.BaseURL, inv.ID)
		}
		body += "\nThe quote: " + link + "\n"
	}
	if err := mailer.Send(smtpConfig(st), mailer.Message{To: []string{to}, Subject: subject, Body: body}); err != nil {
		s.log.Warn("quote: owner notification failed", "quote", q.Number, "err", err)
	}
}

// expireQuotes is the scheduler step: open quotes past their date expire.
func (s *Server) expireQuotes(ctx context.Context, uid int64) int {
	ids, err := s.store.MarkQuotesExpired(ctx, uid)
	if err != nil {
		s.log.Error("scheduler: expire quotes", "user", uid, "err", err)
	}
	for _, id := range ids {
		s.store.LogActivity(ctx, uid, "quote", id, "expired", "Expired (past its valid-until date)")
		if q, err := s.store.GetQuote(ctx, uid, id); err == nil {
			s.hooks.Emit(uid, "quote.expired", q)
		}
	}
	return len(ids)
}
