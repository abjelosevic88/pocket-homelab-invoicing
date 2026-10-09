package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/paperless"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// paperlessClient returns a client for the configured instance, or nil when the
// integration is off. Settings win over environment defaults. The client is
// cached so its name lookups stay warm between requests.
func (s *Server) paperlessClient(ctx context.Context) (*paperless.Client, store.Settings) {
	st, _ := s.store.GetSettings(ctx)
	base, token := st.PaperlessURL, st.PaperlessToken
	if base == "" {
		base = s.cfg.PaperlessURL
	}
	if token == "" {
		token = s.cfg.PaperlessToken
	}
	if base == "" || token == "" {
		return nil, st
	}
	key := base + "\x00" + token
	s.plMu.Lock()
	defer s.plMu.Unlock()
	if s.pl == nil || s.plKey != key {
		s.pl = paperless.New(base, token)
		s.plKey = key
	}
	return s.pl, st
}

// paperlessExternalURL is the base for links opened in the user's browser.
func (s *Server) paperlessExternalURL(st store.Settings) string {
	if st.PaperlessExternalURL != "" {
		return strings.TrimRight(st.PaperlessExternalURL, "/")
	}
	if st.PaperlessURL != "" {
		return strings.TrimRight(st.PaperlessURL, "/")
	}
	return s.cfg.PaperlessURL
}

func (s *Server) paperlessDocURL(st store.Settings, id int64) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/documents/%d/details", s.paperlessExternalURL(st), id)
}

func paperlessTags(st store.Settings) []string {
	var out []string
	for _, t := range strings.Split(st.PaperlessTags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// paperlessLink loads a link and resolves a pending task if needed.
func (s *Server) paperlessLink(ctx context.Context, kind string, refID int64) *store.PaperlessLink {
	l, err := s.store.GetPaperlessLink(ctx, kind, refID)
	if err != nil || l == nil {
		return nil
	}
	return s.resolvePaperlessLink(ctx, l)
}

// resolvePaperlessLink fills the browser URL and, for pending uploads, checks the task once.
func (s *Server) resolvePaperlessLink(ctx context.Context, l *store.PaperlessLink) *store.PaperlessLink {
	cl, st := s.paperlessClient(ctx)
	if l.PaperlessID == 0 && l.TaskID != "" && l.Error == "" && cl != nil {
		if t, err := cl.Task(ctx, l.TaskID); err == nil {
			s.applyTask(ctx, l, t)
		}
	}
	l.URL = s.paperlessDocURL(st, l.PaperlessID)
	return l
}

func (s *Server) applyTask(ctx context.Context, l *store.PaperlessLink, t *paperless.Task) bool {
	switch t.Status {
	case "SUCCESS":
		if t.RelatedDocument > 0 {
			l.PaperlessID = t.RelatedDocument
			l.TaskID = ""
			_ = s.store.SavePaperlessLink(ctx, l)
			return true
		}
		// Paperless reports success but no id, e.g. "Not consuming x: It is a duplicate of y (#123)".
		l.Error = firstNonEmpty(t.Result, "consumed without a document id")
		_ = s.store.SavePaperlessLink(ctx, l)
		return true
	case "FAILURE":
		l.Error = firstNonEmpty(t.Result, "consume task failed")
		_ = s.store.SavePaperlessLink(ctx, l)
		return true
	}
	return false
}

// watchTask polls a fresh upload for a while so the UI sees the document id without waiting for the scheduler.
func (s *Server) watchTask(kind string, refID int64, taskID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cl, _ := s.paperlessClient(ctx)
		if cl == nil {
			return
		}
		t, err := cl.WaitTask(ctx, taskID, 3*time.Second)
		if err != nil || t == nil {
			return
		}
		if l, _ := s.store.GetPaperlessLink(ctx, kind, refID); l != nil && l.TaskID == taskID {
			s.applyTask(ctx, l, t)
		}
	}()
}

// paperlessUpload sends bytes to Paperless and records the link. wait=true blocks up to 20 s for the document id.
func (s *Server) paperlessUpload(ctx context.Context, kind string, refID int64, filename string, data []byte, o paperless.UploadOptions, wait bool) (*store.PaperlessLink, error) {
	cl, st := s.paperlessClient(ctx)
	if cl == nil {
		return nil, fmt.Errorf("paperless is not configured (Settings → Paperless)")
	}
	if len(o.Tags) == 0 {
		o.Tags = paperlessTags(st)
	}
	task, err := cl.Upload(ctx, filename, data, o)
	if err != nil {
		return nil, err
	}
	l := &store.PaperlessLink{Kind: kind, RefID: refID, TaskID: task, Title: o.Title}
	if err := s.store.SavePaperlessLink(ctx, l); err != nil {
		return nil, err
	}
	if wait {
		wctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		t, err := cl.WaitTask(wctx, task, 2*time.Second)
		cancel()
		if err == nil && t != nil {
			s.applyTask(ctx, l, t)
		}
	}
	if l.PaperlessID == 0 && l.Error == "" {
		s.watchTask(kind, refID, task)
	}
	l.URL = s.paperlessDocURL(st, l.PaperlessID)
	return l, nil
}

// paperlessSendDocument archives a company document.
func (s *Server) paperlessSendDocument(ctx context.Context, d *store.Document, wait bool) (*store.PaperlessLink, error) {
	_, st := s.paperlessClient(ctx)
	data, err := os.ReadFile(s.documentPath(d))
	if err != nil {
		return nil, err
	}
	o := paperless.UploadOptions{Title: d.Title, Created: d.DocDate}
	if st.PaperlessCategoryAsType && d.Category != "" {
		o.DocumentType = d.Category
	}
	if st.PaperlessCreateCorrespondents && d.ClientName != "" {
		o.Correspondent = d.ClientName
	}
	l, err := s.paperlessUpload(ctx, "document", d.ID, d.Filename, data, o, wait)
	if err == nil {
		s.store.LogActivity(ctx, "document", d.ID, "paperless", "Sent to Paperless")
	}
	return l, err
}

// paperlessArchiveInvoice archives an invoice: the generated PDF and/or the uploaded
// files, following the same mode as email attachments. Already-archived parts are skipped.
func (s *Server) paperlessArchiveInvoice(ctx context.Context, inv *store.Invoice, source string, wait bool) ([]*store.PaperlessLink, error) {
	cl, st := s.paperlessClient(ctx)
	if cl == nil {
		return nil, fmt.Errorf("paperless is not configured (Settings → Paperless)")
	}
	if source == "" {
		source = firstNonEmpty(st.EmailAttachmentMode, "generated")
	}
	base := paperless.UploadOptions{Created: inv.IssueDate, DocumentType: st.PaperlessInvoiceType}
	if st.PaperlessCreateCorrespondents {
		base.Correspondent = inv.ClientName
	}
	var out []*store.PaperlessLink
	var firstErr error
	if source == "generated" || source == "both" || (source == "uploaded" && len(inv.Attachments) == 0) {
		if l, _ := s.store.GetPaperlessLink(ctx, "invoice", inv.ID); l == nil || (l.PaperlessID == 0 && l.Error != "") {
			data, err := s.renderPDF(ctx, inv)
			if err != nil {
				return nil, err
			}
			o := base
			o.Title = fmt.Sprintf("Invoice %s – %s", inv.Number, inv.ClientName)
			l, err := s.paperlessUpload(ctx, "invoice", inv.ID, safeFilename(inv.Number)+".pdf", data, o, wait)
			if err != nil {
				firstErr = err
			} else {
				out = append(out, l)
			}
		}
	}
	if source == "uploaded" || source == "both" {
		for i := range inv.Attachments {
			a := inv.Attachments[i]
			if l, _ := s.store.GetPaperlessLink(ctx, "attachment", a.ID); l != nil && (l.PaperlessID > 0 || l.Error == "") {
				continue
			}
			data, err := os.ReadFile(s.attachmentPath(&a))
			if err != nil {
				continue
			}
			o := base
			o.Title = fmt.Sprintf("Invoice %s – %s", inv.Number, inv.ClientName)
			if len(inv.Attachments) > 1 {
				o.Title += " (" + strings.TrimSuffix(a.Filename, filepath.Ext(a.Filename)) + ")"
			}
			l, err := s.paperlessUpload(ctx, "attachment", a.ID, a.Filename, data, o, wait)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			out = append(out, l)
		}
	}
	if len(out) > 0 {
		s.store.LogActivity(ctx, "invoice", inv.ID, "paperless", fmt.Sprintf("Archived to Paperless (%d file(s))", len(out)))
	}
	return out, firstErr
}

// paperlessAutoArchive runs in the background after a status change when the
// setting asks for it ("sent" also covers paid, since a paid invoice was sent).
func (s *Server) paperlessAutoArchive(inv *store.Invoice) {
	if inv == nil {
		return
	}
	ctx := context.Background()
	cl, st := s.paperlessClient(ctx)
	if cl == nil {
		return
	}
	want := false
	switch st.PaperlessArchiveInvoices {
	case "sent":
		want = inv.Status == store.StatusSent || inv.Status == store.StatusViewed || inv.Status == store.StatusPartial || inv.Status == store.StatusPaid || inv.Status == store.StatusOverdue
	case "paid":
		want = inv.Status == store.StatusPaid
	}
	if !want {
		return
	}
	id := inv.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		full, err := s.store.GetInvoice(ctx, id)
		if err != nil {
			return
		}
		if _, err := s.paperlessArchiveInvoice(ctx, full, "", false); err != nil {
			s.log.Warn("paperless: auto-archive failed", "invoice", full.Number, "err", err)
		}
	}()
}

// decorateInvoicePaperless adds link info to an invoice and its attachments.
func (s *Server) decorateInvoicePaperless(ctx context.Context, inv *store.Invoice) {
	cl, _ := s.paperlessClient(ctx)
	if cl == nil {
		return
	}
	inv.Paperless = s.paperlessLink(ctx, "invoice", inv.ID)
	if len(inv.Attachments) > 0 {
		ids := make([]int64, len(inv.Attachments))
		for i := range inv.Attachments {
			ids[i] = inv.Attachments[i].ID
		}
		links, _ := s.store.PaperlessLinksFor(ctx, "attachment", ids)
		for i := range inv.Attachments {
			if l, ok := links[inv.Attachments[i].ID]; ok {
				inv.Attachments[i].Paperless = s.resolvePaperlessLink(ctx, l)
			}
		}
	}
}

// resolvePendingPaperless is the scheduler step that finishes consume tasks.
func (s *Server) resolvePendingPaperless(ctx context.Context) int {
	cl, _ := s.paperlessClient(ctx)
	if cl == nil {
		return 0
	}
	pending, err := s.store.PendingPaperlessLinks(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for i := range pending {
		l := &pending[i]
		t, err := cl.Task(ctx, l.TaskID)
		if err != nil {
			continue
		}
		if s.applyTask(ctx, l, t) {
			n++
			continue
		}
		// A task that never shows up (Paperless restarted, queue purged) is marked after a day.
		if created, err := time.Parse(time.RFC3339Nano, l.CreatedAt); err == nil && time.Since(created) > 24*time.Hour {
			l.Error = "consume task did not finish within 24 h"
			_ = s.store.SavePaperlessLink(ctx, l)
		}
	}
	return n
}

// ---------- HTTP handlers ----------

func (s *Server) handlePaperlessStatus(w http.ResponseWriter, r *http.Request) {
	cl, st := s.paperlessClient(r.Context())
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ps := cl.Ping(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":     true,
		"ok":             ps.OK,
		"error":          ps.Error,
		"version":        ps.Version,
		"document_count": ps.DocumentCount,
		"url":            s.paperlessExternalURL(st),
		"archive_mode":   st.PaperlessArchiveInvoices,
	})
}

// handleTestPaperless checks a URL/token pair before it is saved.
func (s *Server) handleTestPaperless(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	_ = decode(r, &in)
	st, _ := s.store.GetSettings(r.Context())
	if in.Token == "" {
		in.Token = firstNonEmpty(st.PaperlessToken, s.cfg.PaperlessToken)
	}
	if in.URL == "" {
		in.URL = firstNonEmpty(st.PaperlessURL, s.cfg.PaperlessURL)
	}
	cl := paperless.New(in.URL, in.Token)
	if !cl.Configured() {
		writeErr(w, http.StatusBadRequest, "URL and token are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ps := cl.Ping(ctx)
	if !ps.OK {
		writeErr(w, http.StatusBadGateway, ps.Error)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) requirePaperless(w http.ResponseWriter, r *http.Request) (*paperless.Client, store.Settings, bool) {
	cl, st := s.paperlessClient(r.Context())
	if cl == nil {
		writeErr(w, http.StatusConflict, "paperless is not configured (Settings → Paperless)")
		return nil, st, false
	}
	return cl, st, true
}

func (s *Server) handlePaperlessSearch(w http.ResponseWriter, r *http.Request) {
	cl, st, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	extra := url.Values{}
	for _, k := range []string{"correspondent__id", "document_type__id", "tags__id__all", "created__date__gte", "created__date__lte"} {
		if v := q.Get(k); v != "" {
			extra.Set(k, v)
		}
	}
	res, err := cl.Search(r.Context(), q.Get("q"), page, size, extra)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// Rewrite links to the browser-facing URL and mark documents already linked locally.
	for i := range res.Results {
		res.Results[i].URL = s.paperlessDocURL(st, res.Results[i].ID)
	}
	linked := map[int64]*store.PaperlessLink{}
	for _, d := range res.Results {
		if l, _ := s.store.PaperlessLinkByPaperlessID(r.Context(), d.ID); l != nil {
			linked[d.ID] = l
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": res.Count, "page": res.Page, "pages": res.Pages, "results": res.Results, "linked": linked})
}

func (s *Server) handlePaperlessNames(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tags":           cl.Names(r.Context(), "tags"),
		"correspondents": cl.Names(r.Context(), "correspondents"),
		"document_types": cl.Names(r.Context(), "document_types"),
	})
}

// handlePaperlessFile proxies the file or thumbnail so the browser never needs the API token.
func (s *Server) handlePaperlessFile(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	id := idParam(r, "pid")
	var resp *http.Response
	var err error
	if strings.HasSuffix(r.URL.Path, "/thumb") {
		resp, err = cl.Thumbnail(r.Context(), id)
	} else {
		resp, err = cl.Download(r.Context(), id, r.URL.Query().Get("original") == "1")
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if r.URL.Query().Get("download") != "1" {
			cd = strings.Replace(cd, "attachment", "inline", 1)
		}
		w.Header().Set("Content-Disposition", cd)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = io.Copy(w, resp.Body)
}

// handlePaperlessImport copies a Paperless document into company documents.
func (s *Server) handlePaperlessImport(w http.ResponseWriter, r *http.Request) {
	cl, st, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id := idParam(r, "pid")
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid paperless document id")
		return
	}
	var in struct {
		Category string `json:"category"`
		ClientID *int64 `json:"client_id"`
		Original bool   `json:"original"`
	}
	_ = decode(r, &in)
	if l, _ := s.store.PaperlessLinkByPaperlessID(ctx, id); l != nil && l.Kind == "document" {
		if d, err := s.store.GetDocument(ctx, l.RefID); err == nil {
			d.Paperless = s.resolvePaperlessLink(ctx, l)
			writeJSON(w, http.StatusOK, d)
			return
		}
	}
	pd, err := cl.Get(ctx, id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	resp, err := cl.Download(ctx, id, in.Original)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	name := firstNonEmpty(pd.ArchivedFileName, pd.OriginalFileName, safeFilename(pd.Title)+".pdf")
	if in.Original && pd.OriginalFileName != "" {
		name = pd.OriginalFileName
	}
	name = cleanFilename(name)
	stored, ct, size, err := s.storeDocumentFile(name, resp.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if ct == "application/octet-stream" && resp.Header.Get("Content-Type") != "" {
		ct = resp.Header.Get("Content-Type")
	}
	d := &store.Document{Title: firstNonEmpty(pd.Title, titleFromFilename(name)), Category: strings.TrimSpace(in.Category), ClientID: in.ClientID, DocDate: pd.Created, Filename: name, StoredName: stored, ContentType: ct, Size: size}
	if d.Category == "" && st.PaperlessCategoryAsType {
		d.Category = pd.DocumentType
	}
	if d.ClientID == nil && pd.Correspondent != "" {
		if clients, err := s.store.ListClients(ctx, true, pd.Correspondent); err == nil {
			for _, c := range clients {
				if strings.EqualFold(c.Name, pd.Correspondent) {
					cid := c.ID
					d.ClientID = &cid
					break
				}
			}
		}
	}
	if err := s.store.CreateDocument(ctx, d); err != nil {
		os.Remove(filepath.Join(s.cfg.DataDir, "documents", stored))
		s.fail(w, err, "create document")
		return
	}
	_ = s.store.SavePaperlessLink(ctx, &store.PaperlessLink{Kind: "document", RefID: d.ID, PaperlessID: pd.ID, Title: pd.Title})
	s.store.LogActivity(ctx, "document", d.ID, "created", "Imported from Paperless #"+strconv.FormatInt(pd.ID, 10))
	d, _ = s.store.GetDocument(ctx, d.ID)
	d.Paperless = s.paperlessLink(ctx, "document", d.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleDocumentToPaperless(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requirePaperless(w, r); !ok {
		return
	}
	d, err := s.store.GetDocument(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	if l, _ := s.store.GetPaperlessLink(r.Context(), "document", d.ID); l != nil && l.PaperlessID > 0 && r.URL.Query().Get("force") != "1" {
		writeJSON(w, http.StatusOK, s.resolvePaperlessLink(r.Context(), l))
		return
	}
	l, err := s.paperlessSendDocument(r.Context(), d, true)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleInvoiceToPaperless(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requirePaperless(w, r); !ok {
		return
	}
	inv, err := s.store.GetInvoice(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	var in struct {
		Source string `json:"source"` // generated | uploaded | both | "" (= email attachment mode)
	}
	_ = decode(r, &in)
	links, err := s.paperlessArchiveInvoice(r.Context(), inv, in.Source, true)
	if err != nil && len(links) == 0 {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	full, _ := s.store.GetInvoice(r.Context(), inv.ID)
	s.decorateInvoicePaperless(r.Context(), full)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleAttachmentToPaperless(w http.ResponseWriter, r *http.Request) {
	if _, st, ok := s.requirePaperless(w, r); !ok {
		return
	} else {
		a, err := s.store.GetAttachment(r.Context(), idParam(r, "aid"))
		if err != nil {
			s.fail(w, err, "get attachment")
			return
		}
		inv, err := s.store.GetInvoice(r.Context(), a.InvoiceID)
		if err != nil {
			s.fail(w, err, "get invoice")
			return
		}
		data, err := os.ReadFile(s.attachmentPath(a))
		if err != nil {
			writeErr(w, http.StatusNotFound, "file missing on disk")
			return
		}
		o := paperless.UploadOptions{Title: fmt.Sprintf("Invoice %s – %s", inv.Number, inv.ClientName), Created: inv.IssueDate, DocumentType: st.PaperlessInvoiceType}
		if st.PaperlessCreateCorrespondents {
			o.Correspondent = inv.ClientName
		}
		l, err := s.paperlessUpload(r.Context(), "attachment", a.ID, a.Filename, data, o, true)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		s.store.LogActivity(r.Context(), "invoice", inv.ID, "paperless", "Sent "+a.Filename+" to Paperless")
		writeJSON(w, http.StatusOK, l)
	}
}

// handleUnlinkPaperless forgets a link (the document stays in Paperless).
func (s *Server) handleUnlinkPaperless(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	switch kind {
	case "document", "invoice", "attachment":
	default:
		writeErr(w, http.StatusBadRequest, "kind must be document, invoice or attachment")
		return
	}
	if err := s.store.DeletePaperlessLink(r.Context(), kind, idParam(r, "id")); err != nil {
		s.fail(w, err, "unlink")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
