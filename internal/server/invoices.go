package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/docx"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/mailer"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/numbering"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// invoiceInput is the JSON body for create/update.
type invoiceInput struct {
	Number        string              `json:"number"`
	ClientID      int64               `json:"client_id"`
	Status        string              `json:"status"`
	IssueDate     string              `json:"issue_date"`
	DueDate       string              `json:"due_date"`
	Currency      string              `json:"currency"`
	ExchangeRate  float64             `json:"exchange_rate"`
	BillingMode   string              `json:"billing_mode"`
	PeriodStart   *string             `json:"period_start"`
	PeriodEnd     *string             `json:"period_end"`
	PONumber      string              `json:"po_number"`
	DiscountType  string              `json:"discount_type"`
	DiscountValue float64             `json:"discount_value"`
	Notes         string              `json:"notes"`
	Terms         string              `json:"terms"`
	Footer        string              `json:"footer"`
	TemplateID    *int64              `json:"template_id"`
	Items         []store.InvoiceItem `json:"items"`
	TimeEntryIDs  []int64             `json:"time_entry_ids"`
	ExpenseIDs    []int64             `json:"expense_ids"`
	CustomFields  map[string]string   `json:"custom_fields"`
}

func emptyToNil(p *string) *string {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	return p
}

// applyInput copies validated input onto an invoice and recomputes totals.
func (s *Server) applyInput(ctx context.Context, inv *store.Invoice, in invoiceInput, st store.Settings) error {
	client, err := s.store.GetClient(ctx, in.ClientID)
	if err != nil {
		return fmt.Errorf("client not found")
	}
	inv.ClientID = client.ID
	inv.ClientName = client.Name
	inv.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if inv.Currency == "" {
		inv.Currency = client.Currency
	}
	if inv.Currency == "" {
		inv.Currency = st.BaseCurrency
	}
	inv.IssueDate = in.IssueDate
	if inv.IssueDate == "" {
		inv.IssueDate = store.Today()
	}
	inv.DueDate = in.DueDate
	if inv.DueDate == "" {
		days := client.PaymentTermsDays
		if days <= 0 {
			days = st.DefaultDueDays
		}
		t, _ := time.Parse("2006-01-02", inv.IssueDate)
		inv.DueDate = t.AddDate(0, 0, days).Format("2006-01-02")
	}
	inv.BillingMode = in.BillingMode
	if inv.BillingMode == "" {
		inv.BillingMode = client.BillingMode
	}
	inv.PeriodStart = emptyToNil(in.PeriodStart)
	inv.PeriodEnd = emptyToNil(in.PeriodEnd)
	inv.PONumber = in.PONumber
	inv.DiscountType = in.DiscountType
	if inv.DiscountType == "" {
		inv.DiscountType = "none"
	}
	inv.DiscountValue = in.DiscountValue
	inv.Notes, inv.Terms, inv.Footer = in.Notes, in.Terms, in.Footer
	inv.TemplateID = in.TemplateID
	if inv.TemplateID != nil && *inv.TemplateID == 0 {
		inv.TemplateID = nil
	}
	if in.CustomFields != nil {
		inv.CustomFields = map[string]string{}
		for k, v := range in.CustomFields {
			if strings.TrimSpace(v) != "" {
				inv.CustomFields[k] = strings.TrimSpace(v)
			}
		}
	}

	// Exchange rate to base currency: explicit > stored > 1
	inv.ExchangeRate = in.ExchangeRate
	if inv.ExchangeRate <= 0 {
		if r, ok := s.store.GetRate(ctx, inv.Currency, st.BaseCurrency); ok {
			inv.ExchangeRate = r
		} else {
			inv.ExchangeRate = 1
		}
	}

	cur := s.store.GetCurrency(ctx, inv.Currency)
	items := make([]money.LineItem, 0, len(in.Items))
	inv.Items = inv.Items[:0]
	for _, it := range in.Items {
		if strings.TrimSpace(it.Description) == "" && it.Quantity == 0 && it.UnitPrice == 0 {
			continue
		}
		if it.Unit == "" {
			it.Unit = inv.BillingMode
			if it.Unit == "" || it.Unit == "fixed" {
				it.Unit = "unit"
			}
		}
		items = append(items, money.LineItem{Quantity: it.Quantity, UnitPrice: it.UnitPrice, Discount: it.Discount, TaxRate: it.TaxRate})
		inv.Items = append(inv.Items, store.InvoiceItem{Description: it.Description, Unit: it.Unit, Quantity: it.Quantity, UnitPrice: it.UnitPrice, TaxRate: it.TaxRate, Discount: it.Discount})
	}
	tot := money.Compute(items, inv.DiscountType, inv.DiscountValue, cur.Decimals)
	for i := range inv.Items {
		inv.Items[i].LineTotal = tot.LineTotals[i]
	}
	inv.Subtotal, inv.DiscountTotal, inv.TaxTotal, inv.Total = tot.Subtotal, tot.DiscountTotal, tot.TaxTotal, tot.Total
	return nil
}

// nextNumber allocates the next invoice number and bumps the sequence.
func (s *Server) nextNumber(ctx context.Context, st *store.Settings, issueDate string, clientName string) (string, error) {
	t, err := time.Parse("2006-01-02", issueDate)
	if err != nil {
		t = time.Now()
	}
	if st.InvoiceSeqResetYr && st.InvoiceSeqYear != t.Year() && st.InvoiceSeqYear != 0 {
		st.InvoiceNextSeq = 1
	}
	st.InvoiceSeqYear = t.Year()
	if st.InvoiceNextSeq < 1 {
		st.InvoiceNextSeq = 1
	}
	for tries := 0; tries < 1000; tries++ {
		n := numbering.Format(st.InvoiceNumberFmt, st.InvoiceNextSeq, t, numbering.ClientCode(clientName))
		exists, err := s.store.NumberExists(ctx, n, 0)
		if err != nil {
			return "", err
		}
		st.InvoiceNextSeq++
		if !exists {
			return n, s.store.SaveSettings(ctx, *st)
		}
	}
	return "", fmt.Errorf("could not allocate invoice number")
}

// handleCheckNumber tells the editor whether a number is free, taken by this client, or used by others.
func (s *Server) handleCheckNumber(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	same, others, err := s.store.NumberUsage(r.Context(), q.Get("number"), qInt64(r, "client_id"), qInt64(r, "exclude"))
	if err != nil {
		s.fail(w, err, "check number")
		return
	}
	if others == nil {
		others = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"same_client": same, "other_clients": others})
}

func (s *Server) handleNextNumber(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	t := time.Now()
	seq := st.InvoiceNextSeq
	if st.InvoiceSeqResetYr && st.InvoiceSeqYear != t.Year() && st.InvoiceSeqYear != 0 {
		seq = 1
	}
	writeJSON(w, http.StatusOK, map[string]string{"number": numbering.Format(st.InvoiceNumberFmt, seq, t, "CLI")})
}

func (s *Server) handleListInvoices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.InvoiceFilter{Status: q.Get("status"), ClientID: qInt64(r, "client_id"), Search: q.Get("q"), From: q.Get("from"), To: q.Get("to"), Limit: qInt(r, "limit", 50), Offset: qInt(r, "offset", 0)}
	list, total, err := s.store.ListInvoices(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list invoices")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

func (s *Server) handleGetInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInvoice(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleCreateInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in invoiceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	inv := &store.Invoice{Status: store.StatusDraft}
	if in.Status == store.StatusSent {
		inv.Status = store.StatusSent
		now := store.Now()
		inv.SentAt = &now
	}
	if err := s.applyInput(ctx, inv, in, st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Number != "" {
		if same, _, _ := s.store.NumberUsage(ctx, in.Number, inv.ClientID, 0); same {
			writeErr(w, http.StatusConflict, "this client already has an invoice with that number")
			return
		}
		inv.Number = in.Number
	} else {
		inv.Number, err = s.nextNumber(ctx, &st, inv.IssueDate, inv.ClientName)
		if err != nil {
			s.fail(w, err, "allocate number")
			return
		}
	}
	if err := s.store.CreateInvoice(ctx, inv); err != nil {
		s.fail(w, err, "create invoice")
		return
	}
	if len(in.TimeEntryIDs) > 0 {
		_ = s.store.LinkTimeEntries(ctx, inv.ID, in.TimeEntryIDs)
	}
	if len(in.ExpenseIDs) > 0 {
		_ = s.store.LinkExpenses(ctx, inv.ID, in.ExpenseIDs)
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "created", "Invoice "+inv.Number+" created")
	full, _ := s.store.GetInvoice(ctx, inv.ID)
	s.hooks.Emit("invoice.created", full)
	writeJSON(w, http.StatusCreated, full)
}

func (s *Server) handleUpdateInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	inv, err := s.store.GetInvoice(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	var in invoiceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	st, _ := s.store.GetSettings(ctx)
	if err := s.applyInput(ctx, inv, in, st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Number != "" && in.Number != inv.Number {
		if same, _, _ := s.store.NumberUsage(ctx, in.Number, inv.ClientID, inv.ID); same {
			writeErr(w, http.StatusConflict, "this client already has an invoice with that number")
			return
		}
		inv.Number = in.Number
	}
	if in.Status != "" && in.Status != inv.Status {
		s.transition(inv, in.Status)
	}
	if err := s.store.UpdateInvoice(ctx, inv); err != nil {
		s.fail(w, err, "update invoice")
		return
	}
	if len(in.TimeEntryIDs) > 0 {
		_ = s.store.LinkTimeEntries(ctx, inv.ID, in.TimeEntryIDs)
	}
	full, err := s.store.RecalcAmountPaid(ctx, inv.ID)
	if err != nil {
		s.fail(w, err, "recalc")
		return
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "updated", "Invoice "+inv.Number+" updated")
	s.hooks.Emit("invoice.updated", full)
	writeJSON(w, http.StatusOK, full)
}

// transition applies a status change with timestamps.
func (s *Server) transition(inv *store.Invoice, status string) {
	now := store.Now()
	switch status {
	case store.StatusSent:
		if inv.SentAt == nil {
			inv.SentAt = &now
		}
		if inv.DueDate < store.Today() {
			status = store.StatusOverdue
		}
	case store.StatusPaid:
		inv.PaidAt = &now
		inv.AmountPaid = inv.Total
	case store.StatusDraft:
		inv.SentAt, inv.PaidAt, inv.ViewedAt = nil, nil, nil
	}
	inv.Status = status
}

func (s *Server) handleInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	inv, err := s.store.GetInvoice(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Status {
	case store.StatusDraft, store.StatusSent, store.StatusPaid, store.StatusCancelled:
	default:
		writeErr(w, http.StatusBadRequest, "status must be draft, sent, paid or cancelled")
		return
	}
	if in.Status == store.StatusPaid && inv.Total-inv.AmountPaid > 0 {
		// Record the remaining balance as a payment so reports stay consistent.
		p := &store.Payment{InvoiceID: inv.ID, Date: store.Today(), Amount: inv.Total - inv.AmountPaid, Currency: inv.Currency, ExchangeRate: 1, AppliedAmount: inv.Total - inv.AmountPaid, Method: "other", Notes: "Marked as paid"}
		if err := s.store.CreatePayment(ctx, p); err != nil {
			s.fail(w, err, "create payment")
			return
		}
	}
	s.transition(inv, in.Status)
	if err := s.store.UpdateInvoiceStatus(ctx, inv.ID, inv.Status, inv.SentAt, inv.ViewedAt, inv.PaidAt); err != nil {
		s.fail(w, err, "update status")
		return
	}
	full, err := s.store.RecalcAmountPaid(ctx, inv.ID)
	if err != nil {
		s.fail(w, err, "recalc")
		return
	}
	if in.Status == store.StatusDraft || in.Status == store.StatusCancelled {
		_ = s.store.UpdateInvoiceStatus(ctx, inv.ID, in.Status, nil, nil, nil)
		full, _ = s.store.GetInvoice(ctx, inv.ID)
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "status", "Status changed to "+full.Status)
	s.hooks.Emit("invoice."+full.Status, full)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleDeleteInvoice(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	inv, err := s.store.GetInvoice(r.Context(), id)
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	if err := s.store.DeleteInvoice(r.Context(), id); err != nil {
		s.fail(w, err, "delete invoice")
		return
	}
	s.removeAttachmentFiles(id)
	s.store.LogActivity(r.Context(), "invoice", id, "deleted", "Invoice "+inv.Number+" deleted")
	s.hooks.Emit("invoice.deleted", map[string]any{"id": id, "number": inv.Number})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDuplicateInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.GetInvoice(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	dup := *src
	dup.ID, dup.Status, dup.AmountPaid, dup.PublicToken = 0, store.StatusDraft, 0, ""
	dup.SentAt, dup.ViewedAt, dup.PaidAt, dup.RecurringID = nil, nil, nil, nil
	dup.IssueDate = store.Today()
	t, _ := time.Parse("2006-01-02", dup.IssueDate)
	days := st.DefaultDueDays
	if c, err := s.store.GetClient(ctx, dup.ClientID); err == nil && c.PaymentTermsDays > 0 {
		days = c.PaymentTermsDays
	}
	dup.DueDate = t.AddDate(0, 0, days).Format("2006-01-02")
	dup.Payments = nil
	for i := range dup.Items {
		dup.Items[i].ID = 0
	}
	dup.Number, err = s.nextNumber(ctx, &st, dup.IssueDate, dup.ClientName)
	if err != nil {
		s.fail(w, err, "allocate number")
		return
	}
	if err := s.store.CreateInvoice(ctx, &dup); err != nil {
		s.fail(w, err, "create invoice")
		return
	}
	s.store.LogActivity(ctx, "invoice", dup.ID, "created", "Duplicated from "+src.Number)
	full, _ := s.store.GetInvoice(ctx, dup.ID)
	writeJSON(w, http.StatusCreated, full)
}

// buildDocument assembles the render model for an invoice.
func (s *Server) buildDocument(ctx context.Context, inv *store.Invoice, templateID *int64) (*pdf.Document, *store.InvoiceTemplate, []byte, string, error) {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, nil, nil, "", err
	}
	client, _ := s.store.GetClient(ctx, inv.ClientID)
	var tpl *store.InvoiceTemplate
	if templateID != nil && *templateID > 0 {
		tpl, _ = s.store.GetTemplate(ctx, *templateID)
	}
	if tpl == nil && client != nil && client.TemplateID != nil && *client.TemplateID > 0 {
		tpl, _ = s.store.GetTemplate(ctx, *client.TemplateID) // per-client default
	}
	if tpl == nil {
		tpl, _ = s.store.GetDefaultTemplate(ctx)
	}
	logo, logoType, logoMime := s.readLogo(ctx, st)
	logoURL := ""
	if len(logo) > 0 {
		logoURL = "data:" + logoMime + ";base64," + base64.StdEncoding.EncodeToString(logo)
	}
	doc := pdf.Build(pdf.BuildInput{
		Invoice: inv, Client: client, Settings: st, Template: tpl,
		Currency: s.store.GetCurrency(ctx, inv.Currency), BaseCurrency: s.store.GetCurrency(ctx, st.BaseCurrency),
		PublicURL: s.cfg.BaseURL + "/i/" + inv.PublicToken, LogoDataURL: logoURL,
	})
	return doc, tpl, logo, logoType, nil
}

func (s *Server) renderPDF(ctx context.Context, inv *store.Invoice) ([]byte, error) {
	doc, tpl, logo, logoType, err := s.buildDocument(ctx, inv, inv.TemplateID)
	if err != nil {
		return nil, err
	}
	s.pdfCount.Add(1)
	if tpl.Kind == "docx" {
		filled, err := s.fillDocx(tpl, doc)
		if err != nil {
			return nil, err
		}
		return s.docx.Convert(ctx, filled)
	}
	return s.pdf.Render(ctx, doc, tpl.HTML, logo, logoType)
}

// fillDocx renders a Word template with the document data.
func (s *Server) fillDocx(tpl *store.InvoiceTemplate, doc *pdf.Document) ([]byte, error) {
	if tpl.DocxPath == "" {
		return nil, fmt.Errorf("template %q has no Word file uploaded", tpl.Name)
	}
	b, err := os.ReadFile(s.docxTemplatePath(tpl))
	if err != nil {
		return nil, fmt.Errorf("read template file: %w", err)
	}
	return docx.Fill(b, doc.Data)
}

func (s *Server) handleInvoiceDocx(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInvoice(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	doc, tpl, _, _, err := s.buildDocument(r.Context(), inv, inv.TemplateID)
	if err != nil {
		s.fail(w, err, "build document")
		return
	}
	if tpl.Kind != "docx" {
		if id := qInt64(r, "template_id"); id > 0 {
			tpl, err = s.store.GetTemplate(r.Context(), id)
			if err != nil {
				s.fail(w, err, "get template")
				return
			}
		} else {
			writeErr(w, http.StatusBadRequest, "this invoice does not use a Word template; pass ?template_id=<id of a Word template>")
			return
		}
	}
	filled, err := s.fillDocx(tpl, doc)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.docx"`, safeFilename(inv.Number)))
	_, _ = w.Write(filled)
}

func (s *Server) handleInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInvoice(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	s.servePDF(w, r, inv)
}

func (s *Server) servePDF(w http.ResponseWriter, r *http.Request, inv *store.Invoice) {
	b, err := s.renderPDF(r.Context(), inv)
	if err != nil {
		s.fail(w, err, "render pdf")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s.pdf"`, disposition, safeFilename(inv.Number)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func safeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '"' {
			return '-'
		}
		return r
	}, s)
}

func (s *Server) handleInvoiceHTML(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInvoice(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	s.serveHTML(w, r, inv)
}

func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request, inv *store.Invoice) {
	doc, tpl, _, _, err := s.buildDocument(r.Context(), inv, inv.TemplateID)
	if err != nil {
		s.fail(w, err, "build document")
		return
	}
	b, err := pdf.RenderHTML(doc, tpl.HTML)
	if err != nil {
		s.fail(w, err, "render html")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

// sendInvoiceEmail renders the PDF and emails it to the client.
func (s *Server) sendInvoiceEmail(ctx context.Context, inv *store.Invoice, to, subjectOverride, bodyOverride string) error {
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return err
	}
	client, err := s.store.GetClient(ctx, inv.ClientID)
	if err != nil {
		return err
	}
	if to == "" {
		to = client.Email
	}
	if to == "" {
		return fmt.Errorf("client has no email address")
	}
	cur := s.store.GetCurrency(ctx, inv.Currency)
	sym, pos := cur.Symbol, "before"
	if strings.HasSuffix(sym, " ") {
		sym, pos = strings.TrimSpace(sym), "after"
	}
	vars := map[string]string{
		"{number}": inv.Number, "{company}": st.CompanyName, "{client}": client.Name,
		"{total}": money.Format(inv.Total, cur.Decimals, sym, pos), "{balance}": money.Format(inv.Total-inv.AmountPaid, cur.Decimals, sym, pos),
		"{due_date}": inv.DueDate, "{issue_date}": inv.IssueDate, "{link}": s.cfg.BaseURL + "/i/" + inv.PublicToken,
	}
	// precedence: explicit override (dialog) > client template > global template
	subject, body := st.EmailSubject, st.EmailBody
	if client.EmailSubject != "" {
		subject = client.EmailSubject
	}
	if client.EmailBody != "" {
		body = client.EmailBody
	}
	if subjectOverride != "" {
		subject = subjectOverride
	}
	if bodyOverride != "" {
		body = bodyOverride
	}
	vars["{contact}"] = firstNonEmpty(client.ContactName, client.Name)
	vars["{period}"] = ""
	if inv.PeriodStart != nil && inv.PeriodEnd != nil {
		vars["{period}"] = *inv.PeriodStart + " – " + *inv.PeriodEnd
	}
	for k, v := range vars {
		subject = strings.ReplaceAll(subject, k, v)
		body = strings.ReplaceAll(body, k, v)
	}
	if client.EmailCC != "" {
		to = to + "," + client.EmailCC
	}
	var attachments []mailer.Attachment
	mode := st.EmailAttachmentMode
	if mode == "" {
		mode = "generated"
	}
	if mode == "generated" || mode == "both" || len(inv.Attachments) == 0 {
		pdfBytes, err := s.renderPDF(ctx, inv)
		if err != nil {
			return err
		}
		attachments = append(attachments, mailer.Attachment{Filename: safeFilename(inv.Number) + ".pdf", ContentType: "application/pdf", Data: pdfBytes})
	}
	if mode == "uploaded" || mode == "both" {
		for _, a := range inv.Attachments {
			data, err := os.ReadFile(s.attachmentPath(&a))
			if err != nil {
				s.log.Warn("email: attachment missing", "file", a.Filename, "err", err)
				continue
			}
			attachments = append(attachments, mailer.Attachment{Filename: a.Filename, ContentType: a.ContentType, Data: data})
		}
	}
	cfg := mailer.Config{Host: st.SMTPHost, Port: st.SMTPPort, User: st.SMTPUser, Password: st.SMTPPassword, From: st.SMTPFrom, FromName: st.SMTPFromName, TLS: st.SMTPTLS, BCC: st.SMTPBCC}
	if cfg.FromName == "" {
		cfg.FromName = st.CompanyName
	}
	recipients := []string{}
	for _, a := range strings.Split(to, ",") {
		if a = strings.TrimSpace(a); a != "" {
			recipients = append(recipients, a)
		}
	}
	err = mailer.Send(cfg, mailer.Message{To: recipients, Subject: subject, Body: body, Attachments: attachments})
	status, errMsg := "sent", ""
	if err != nil {
		status, errMsg = "failed", err.Error()
	}
	s.store.LogEmail(ctx, inv.ID, to, subject, status, errMsg)
	return err
}

func (s *Server) handleSendInvoice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	inv, err := s.store.GetInvoice(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	var in struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	_ = decode(r, &in)
	if err := s.sendInvoiceEmail(ctx, inv, in.To, in.Subject, in.Body); err != nil {
		writeErr(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	if inv.Status == store.StatusDraft {
		s.transition(inv, store.StatusSent)
		_ = s.store.UpdateInvoiceStatus(ctx, inv.ID, inv.Status, inv.SentAt, nil, nil)
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "sent", "Emailed to "+firstNonEmpty(in.To, "client"))
	full, _ := s.store.GetInvoice(ctx, inv.ID)
	s.hooks.Emit("invoice.sent", full)
	writeJSON(w, http.StatusOK, full)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *Server) handleInvoiceEmails(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListEmailLog(r.Context(), idParam(r, "id"), 50)
	if err != nil {
		s.fail(w, err, "email log")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleInvoiceFromTime builds a draft invoice from unbilled time entries (and optionally expenses).
func (s *Server) handleInvoiceFromTime(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		ClientID        int64   `json:"client_id"`
		From            string  `json:"from"`
		To              string  `json:"to"`
		EntryIDs        []int64 `json:"entry_ids"`
		ExpenseIDs      []int64 `json:"expense_ids"`
		GroupBy         string  `json:"group_by"`     // "entry" | "project" | "day" | "total"
		BillingMode     string  `json:"billing_mode"` // hourly | daily (converts hours to days)
		Rate            float64 `json:"rate"`
		IncludeExpenses bool    `json:"include_expenses"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	client, err := s.store.GetClient(ctx, in.ClientID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "client not found")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	entries, err := s.store.ListTimeEntries(ctx, store.TimeFilter{ClientID: in.ClientID, Unbilled: true, From: in.From, To: in.To})
	if err != nil {
		s.fail(w, err, "list time")
		return
	}
	if len(in.EntryIDs) > 0 {
		keep := map[int64]bool{}
		for _, id := range in.EntryIDs {
			keep[id] = true
		}
		filtered := entries[:0]
		for _, e := range entries {
			if keep[e.ID] {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	if len(entries) == 0 && len(in.ExpenseIDs) == 0 && !in.IncludeExpenses {
		writeErr(w, http.StatusBadRequest, "no unbilled time entries for this client in the selected range")
		return
	}
	rate := in.Rate
	if rate <= 0 {
		rate = client.DefaultRate
	}
	if rate <= 0 {
		rate = st.DefaultHourlyRate
	}
	mode := in.BillingMode
	if mode == "" {
		mode = client.BillingMode
	}
	if mode != "daily" {
		mode = "hourly"
	}
	hoursPerDay := st.HoursPerDay
	if hoursPerDay <= 0 {
		hoursPerDay = 8
	}
	taxRate := st.DefaultTaxRate

	type group struct {
		desc    string
		minutes int
		rate    float64
		first   string
	}
	groups := []*group{}
	byKey := map[string]*group{}
	var ids []int64
	var minStart, maxStart string
	for _, e := range entries {
		ids = append(ids, e.ID)
		day := e.StartedAt[:10]
		if minStart == "" || day < minStart {
			minStart = day
		}
		if day > maxStart {
			maxStart = day
		}
		r := rate
		if e.Rate != nil && *e.Rate > 0 {
			r = *e.Rate
		}
		var key, desc string
		switch in.GroupBy {
		case "project":
			key, desc = e.Project+"|"+fmt.Sprint(r), firstNonEmpty(e.Project, "Work")
		case "day":
			key, desc = day+"|"+fmt.Sprint(r), "Work on "+day
		case "total":
			key, desc = "total|"+fmt.Sprint(r), "Professional services"
		default:
			key = fmt.Sprint(e.ID)
			desc = firstNonEmpty(e.Description, e.Project, "Work")
			if e.Project != "" && e.Description != "" {
				desc = e.Project + ": " + e.Description
			}
			desc += " (" + day + ")"
		}
		g, ok := byKey[key]
		if !ok {
			g = &group{desc: desc, rate: r, first: day}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.minutes += e.DurationMinutes
	}
	inv := &store.Invoice{Status: store.StatusDraft}
	input := invoiceInput{ClientID: client.ID, Currency: client.Currency, BillingMode: mode, Notes: st.DefaultNotes, Terms: strings.ReplaceAll(st.DefaultTerms, "{due_days}", fmt.Sprint(firstNonZero(client.PaymentTermsDays, st.DefaultDueDays))), Footer: st.DefaultFooter}
	if minStart != "" {
		input.PeriodStart, input.PeriodEnd = &minStart, &maxStart
	}
	for _, g := range groups {
		hours := float64(g.minutes) / 60
		if mode == "daily" {
			days := money.Round(hours/hoursPerDay, 2)
			input.Items = append(input.Items, store.InvoiceItem{Description: g.desc, Unit: "day", Quantity: days, UnitPrice: g.rate, TaxRate: taxRate})
		} else {
			input.Items = append(input.Items, store.InvoiceItem{Description: g.desc, Unit: "hour", Quantity: money.Round(hours, 2), UnitPrice: g.rate, TaxRate: taxRate})
		}
	}
	// Expenses
	var expenseIDs []int64
	if in.IncludeExpenses || len(in.ExpenseIDs) > 0 {
		exps, _ := s.store.ListExpenses(ctx, store.ExpenseFilter{ClientID: client.ID, Unbilled: true, From: in.From, To: in.To})
		keep := map[int64]bool{}
		for _, id := range in.ExpenseIDs {
			keep[id] = true
		}
		for _, e := range exps {
			if len(in.ExpenseIDs) > 0 && !keep[e.ID] {
				continue
			}
			amount := e.Amount
			if e.Currency != client.Currency {
				if r, ok := s.store.GetRate(ctx, e.Currency, client.Currency); ok {
					amount = money.Convert(amount, r, 2)
				}
			}
			input.Items = append(input.Items, store.InvoiceItem{Description: "Expense: " + firstNonEmpty(e.Description, e.Category) + " (" + e.Date + ")", Unit: "unit", Quantity: 1, UnitPrice: amount, TaxRate: taxRate})
			expenseIDs = append(expenseIDs, e.ID)
		}
	}
	if len(input.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "nothing to invoice")
		return
	}
	if err := s.applyInput(ctx, inv, input, st); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	inv.Number, err = s.nextNumber(ctx, &st, inv.IssueDate, inv.ClientName)
	if err != nil {
		s.fail(w, err, "allocate number")
		return
	}
	if err := s.store.CreateInvoice(ctx, inv); err != nil {
		s.fail(w, err, "create invoice")
		return
	}
	_ = s.store.LinkTimeEntries(ctx, inv.ID, ids)
	_ = s.store.LinkExpenses(ctx, inv.ID, expenseIDs)
	s.store.LogActivity(ctx, "invoice", inv.ID, "created", fmt.Sprintf("Created from %d time entries", len(ids)))
	full, _ := s.store.GetInvoice(ctx, inv.ID)
	s.hooks.Emit("invoice.created", full)
	writeJSON(w, http.StatusCreated, full)
}

func firstNonZero(v ...int) int {
	for _, n := range v {
		if n != 0 {
			return n
		}
	}
	return 0
}
