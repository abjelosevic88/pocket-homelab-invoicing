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

// seedPaperlessEnv copies PAPERLESS_URL / PAPERLESS_TOKEN into the box
// owner's own Paperless settings, once: they are a single box-wide credential,
// so they only stand in for the owner (members configure their own). After
// that the saved settings are all there is, so a Disconnect stays
// disconnected across restarts. Without an account yet it waits.
func (s *Server) seedPaperlessEnv(ctx context.Context) {
	if s.plSeeded.Load() {
		return
	}
	if s.cfg.PaperlessURL == "" && s.cfg.PaperlessToken == "" {
		s.plSeeded.Store(true)
		return
	}
	if v, _ := s.store.GetKV(ctx, "paperless_env_seeded"); v != "" {
		s.plSeeded.Store(true)
		return
	}
	owner := s.store.OwnerID(ctx)
	if owner == 0 {
		return
	}
	st, err := s.store.GetSettings(ctx, owner)
	if err != nil {
		return
	}
	if st.PaperlessURL == "" && st.PaperlessToken == "" {
		st.PaperlessURL, st.PaperlessToken = s.cfg.PaperlessURL, s.cfg.PaperlessToken
		if err := s.store.SaveSettings(ctx, owner, st); err != nil {
			return
		}
		s.log.Info("paperless: PAPERLESS_URL/PAPERLESS_TOKEN saved as the owner's Paperless connection")
	}
	if s.store.SetKV(ctx, "paperless_env_seeded", store.Now()) == nil {
		s.plSeeded.Store(true)
	}
}

// paperlessMode is the person's connection mode in effect.
func (s *Server) paperlessMode(st store.Settings) string {
	return connMode(st.PaperlessMode, st.PaperlessToken, s.core.Configured())
}

// paperlessConn returns a client for the person's Paperless, following their
// connection mode (Settings → Paperless):
//
//   - core: Pocket Core hands out the address and the person's own token (the
//     box's token only for an admin), reused by the SDK for about a minute;
//   - direct: the URL and token saved here;
//   - off: no client.
//
// The returned settings carry the address in use (PaperlessURL /
// PaperlessExternalURL) so links open the right place. Clients are cached per
// URL+token so name lookups stay warm between requests.
func (s *Server) paperlessConn(ctx context.Context, uid int64) (*paperless.Client, store.Settings, error) {
	return s.paperlessConnect(ctx, uid, false)
}

// paperlessConnect is paperlessConn; fresh skips the credential cache (Test connection).
func (s *Server) paperlessConnect(ctx context.Context, uid int64, fresh bool) (*paperless.Client, store.Settings, error) {
	s.seedPaperlessEnv(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	var base, token string
	switch s.paperlessMode(st) {
	case modeOff:
		return nil, st, fmt.Errorf("paperless is switched off (Settings → Paperless)")
	case modeCore:
		u, err := s.store.GetUser(ctx, uid)
		if err != nil {
			return nil, st, fmt.Errorf("paperless: no account to act for")
		}
		cred, err := s.serviceFor(ctx, u.Email, "paperless", "Paperless-ngx", fresh)
		if err != nil {
			return nil, st, err
		}
		base, token = strings.TrimRight(cred.URL, "/"), cred.Key
		st.PaperlessURL, st.PaperlessExternalURL = base, firstNonEmpty(cred.PublicURL, base)
		if token == "" {
			return nil, st, fmt.Errorf("the Paperless-ngx credential in Pocket Core has no token")
		}
	default:
		base, token = st.PaperlessURL, st.PaperlessToken
		if base == "" || token == "" {
			return nil, st, fmt.Errorf("paperless is not configured (Settings → Paperless)")
		}
	}
	key := base + "\x00" + token
	s.plMu.Lock()
	defer s.plMu.Unlock()
	if s.pl == nil {
		s.pl = map[string]*paperless.Client{}
	}
	cl, ok := s.pl[key]
	if !ok {
		cl = paperless.New(base, token)
		s.pl[key] = cl
	}
	return cl, st, nil
}

// paperlessClient is paperlessConn without the reason: nil when the person
// has no working Paperless connection.
func (s *Server) paperlessClient(ctx context.Context, uid int64) (*paperless.Client, store.Settings) {
	cl, st, err := s.paperlessConn(ctx, uid)
	if err != nil {
		return nil, st
	}
	return cl, st
}

// paperlessExternalURL is the base for links opened in the user's browser.
func (s *Server) paperlessExternalURL(st store.Settings) string {
	if st.PaperlessExternalURL != "" {
		return strings.TrimRight(st.PaperlessExternalURL, "/")
	}
	return strings.TrimRight(st.PaperlessURL, "/")
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
func (s *Server) paperlessLink(ctx context.Context, uid int64, kind string, refID int64) *store.PaperlessLink {
	l, err := s.store.GetPaperlessLink(ctx, uid, kind, refID)
	if err != nil || l == nil {
		return nil
	}
	return s.resolvePaperlessLink(ctx, l)
}

// resolvePaperlessLink fills the browser URL and, for pending uploads, checks the task once.
func (s *Server) resolvePaperlessLink(ctx context.Context, l *store.PaperlessLink) *store.PaperlessLink {
	cl, st := s.paperlessClient(ctx, l.UserID)
	return s.resolvePaperlessLinkWith(ctx, l, cl, st)
}

// resolvePaperlessLinkWith is resolvePaperlessLink with the person's client already in hand.
func (s *Server) resolvePaperlessLinkWith(ctx context.Context, l *store.PaperlessLink, cl *paperless.Client, st store.Settings) *store.PaperlessLink {
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
			_ = s.store.SavePaperlessLink(ctx, l.UserID, l)
			return true
		}
		// Paperless reports success but no id, e.g. "Not consuming x: It is a duplicate of y (#123)".
		l.Error = firstNonEmpty(t.Result, "consumed without a document id")
		_ = s.store.SavePaperlessLink(ctx, l.UserID, l)
		return true
	case "FAILURE":
		l.Error = firstNonEmpty(t.Result, "consume task failed")
		_ = s.store.SavePaperlessLink(ctx, l.UserID, l)
		return true
	}
	return false
}

// watchTask polls a fresh upload for a while so the UI sees the document id without waiting for the scheduler.
func (s *Server) watchTask(uid int64, kind string, refID int64, taskID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cl, _ := s.paperlessClient(ctx, uid)
		if cl == nil {
			return
		}
		t, err := cl.WaitTask(ctx, taskID, 3*time.Second)
		if err != nil || t == nil {
			return
		}
		if l, _ := s.store.GetPaperlessLink(ctx, uid, kind, refID); l != nil && l.TaskID == taskID {
			s.applyTask(ctx, l, t)
		}
	}()
}

// paperlessUpload sends bytes to Paperless and records the link. wait=true blocks up to 20 s for the document id.
func (s *Server) paperlessUpload(ctx context.Context, uid int64, kind string, refID int64, filename string, data []byte, o paperless.UploadOptions, wait bool) (*store.PaperlessLink, error) {
	cl, st, err := s.paperlessConn(ctx, uid)
	if err != nil {
		return nil, err
	}
	if len(o.Tags) == 0 {
		o.Tags = paperlessTags(st)
	}
	task, err := cl.Upload(ctx, filename, data, o)
	if err != nil {
		return nil, err
	}
	l := &store.PaperlessLink{Kind: kind, RefID: refID, TaskID: task, Title: o.Title}
	if err := s.store.SavePaperlessLink(ctx, uid, l); err != nil {
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
		s.watchTask(uid, kind, refID, task)
	}
	l.URL = s.paperlessDocURL(st, l.PaperlessID)
	return l, nil
}

// paperlessSendDocument archives a company document.
func (s *Server) paperlessSendDocument(ctx context.Context, uid int64, d *store.Document, wait bool) (*store.PaperlessLink, error) {
	_, st := s.paperlessClient(ctx, uid)
	data, err := os.ReadFile(s.documentPath(d))
	if err != nil {
		return nil, err
	}
	o := paperless.UploadOptions{Title: d.Title, Created: d.DocDate}
	if st.PaperlessCategoryAsType && d.Category != "" {
		o.DocumentType = d.Category
	}
	if d.ClientID != nil {
		o.CorrespondentID, o.Correspondent = s.clientCorrespondent(ctx, uid, *d.ClientID, d.ClientName, st)
	}
	l, err := s.paperlessUpload(ctx, uid, "document", d.ID, d.Filename, data, o, wait)
	if err == nil {
		s.store.LogActivity(ctx, uid, "document", d.ID, "paperless", "Sent to Paperless")
	}
	return l, err
}

// paperlessArchiveInvoice archives an invoice: the generated PDF and/or the uploaded
// files, following the same mode as email attachments. Already-archived parts are skipped.
func (s *Server) paperlessArchiveInvoice(ctx context.Context, inv *store.Invoice, source string, wait bool) ([]*store.PaperlessLink, error) {
	uid := inv.UserID
	_, st, err := s.paperlessConn(ctx, uid)
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = firstNonEmpty(st.EmailAttachmentMode, "generated")
	}
	base := paperless.UploadOptions{Created: inv.IssueDate, DocumentType: st.PaperlessInvoiceType}
	base.CorrespondentID, base.Correspondent = s.clientCorrespondent(ctx, uid, inv.ClientID, inv.ClientName, st)
	var out []*store.PaperlessLink
	var firstErr error
	if source == "generated" || source == "both" || (source == "uploaded" && len(inv.Attachments) == 0) {
		if l, _ := s.store.GetPaperlessLink(ctx, uid, "invoice", inv.ID); l == nil || (l.PaperlessID == 0 && l.Error != "") {
			data, err := s.renderPDF(ctx, inv)
			if err != nil {
				return nil, err
			}
			o := base
			o.Title = fmt.Sprintf("Invoice %s – %s", inv.Number, inv.ClientName)
			l, err := s.paperlessUpload(ctx, uid, "invoice", inv.ID, safeFilename(inv.Number)+".pdf", data, o, wait)
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
			if l, _ := s.store.GetPaperlessLink(ctx, uid, "attachment", a.ID); l != nil && (l.PaperlessID > 0 || l.Error == "") {
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
			l, err := s.paperlessUpload(ctx, uid, "attachment", a.ID, a.Filename, data, o, wait)
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
		s.store.LogActivity(ctx, uid, "invoice", inv.ID, "paperless", fmt.Sprintf("Archived to Paperless (%d file(s))", len(out)))
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
	uid := inv.UserID
	cl, st := s.paperlessClient(ctx, uid)
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
		full, err := s.store.GetInvoice(ctx, uid, id)
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
	cl, st := s.paperlessClient(ctx, inv.UserID)
	if cl == nil {
		return
	}
	if l, err := s.store.GetPaperlessLink(ctx, inv.UserID, "invoice", inv.ID); err == nil && l != nil {
		inv.Paperless = s.resolvePaperlessLinkWith(ctx, l, cl, st)
	}
	if len(inv.Attachments) > 0 {
		ids := make([]int64, len(inv.Attachments))
		for i := range inv.Attachments {
			ids[i] = inv.Attachments[i].ID
		}
		links, _ := s.store.PaperlessLinksFor(ctx, inv.UserID, "attachment", ids)
		for i := range inv.Attachments {
			if l, ok := links[inv.Attachments[i].ID]; ok {
				inv.Attachments[i].Paperless = s.resolvePaperlessLinkWith(ctx, l, cl, st)
			}
		}
	}
}

// resolvePendingPaperless is the scheduler step that finishes one user's consume tasks
// with that user's Paperless credentials.
func (s *Server) resolvePendingPaperless(ctx context.Context, uid int64) int {
	// the database first: most runs have nothing pending, and then Core is not asked
	pending, err := s.store.PendingPaperlessLinks(ctx, uid)
	if err != nil || len(pending) == 0 {
		return 0
	}
	cl, _ := s.paperlessClient(ctx, uid)
	if cl == nil {
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
			_ = s.store.SavePaperlessLink(ctx, uid, l)
		}
	}
	return n
}

// ---------- HTTP handlers ----------

func (s *Server) handlePaperlessStatus(w http.ResponseWriter, r *http.Request) {
	cl, st := s.paperlessClient(r.Context(), uidOf(r))
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

// handleTestPaperless checks the connection: a URL/token typed into the form
// is tried as is (before it is saved); otherwise the person's mode decides
// (Pocket Core, the saved URL and token, or off).
func (s *Server) handleTestPaperless(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	_ = decode(r, &in)
	uid := uidOf(r)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var cl *paperless.Client
	via, source := modeDirect, ""
	if in.Token != "" {
		st, _ := s.store.GetSettings(ctx, uid)
		cl = paperless.New(firstNonEmpty(in.URL, st.PaperlessURL), in.Token)
		if !cl.Configured() {
			writeErr(w, http.StatusBadRequest, "URL and token are required")
			return
		}
	} else {
		var st store.Settings
		var err error
		cl, st, err = s.paperlessConnect(ctx, uid, true)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if s.paperlessMode(st) == modeCore {
			via = modeCore
			if u, err := s.store.GetUser(ctx, uid); err == nil {
				source = s.sources.get(u.Email, "paperless")
			}
		}
	}
	ps := cl.Ping(ctx)
	if !ps.OK {
		writeErr(w, http.StatusBadGateway, ps.Error)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": ps.Version, "document_count": ps.DocumentCount, "via": via, "source": source})
}

func (s *Server) requirePaperless(w http.ResponseWriter, r *http.Request) (*paperless.Client, store.Settings, bool) {
	cl, st, err := s.paperlessConn(r.Context(), uidOf(r))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
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
		if l, _ := s.store.PaperlessLinkByPaperlessID(r.Context(), uidOf(r), d.ID); l != nil {
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
	uid := uidOf(r)
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
	in.ClientID = s.ownClientID(ctx, uid, in.ClientID)
	if l, _ := s.store.PaperlessLinkByPaperlessID(ctx, uid, id); l != nil && l.Kind == "document" {
		if d, err := s.store.GetDocument(ctx, uid, l.RefID); err == nil {
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
		if clients, err := s.store.ListClients(ctx, uid, true, pd.Correspondent); err == nil {
			for _, c := range clients {
				if strings.EqualFold(c.Name, pd.Correspondent) {
					cid := c.ID
					d.ClientID = &cid
					break
				}
			}
		}
	}
	if err := s.store.CreateDocument(ctx, uid, d); err != nil {
		os.Remove(filepath.Join(s.cfg.DataDir, "documents", stored))
		s.fail(w, err, "create document")
		return
	}
	_ = s.store.SavePaperlessLink(ctx, uid, &store.PaperlessLink{Kind: "document", RefID: d.ID, PaperlessID: pd.ID, Title: pd.Title})
	s.store.LogActivity(ctx, uid, "document", d.ID, "created", "Imported from Paperless #"+strconv.FormatInt(pd.ID, 10))
	d, _ = s.store.GetDocument(ctx, uid, d.ID)
	d.Paperless = s.paperlessLink(ctx, uid, "document", d.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleDocumentToPaperless(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requirePaperless(w, r); !ok {
		return
	}
	uid := uidOf(r)
	d, err := s.store.GetDocument(r.Context(), uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	if l, _ := s.store.GetPaperlessLink(r.Context(), uid, "document", d.ID); l != nil && l.PaperlessID > 0 && r.URL.Query().Get("force") != "1" {
		writeJSON(w, http.StatusOK, s.resolvePaperlessLink(r.Context(), l))
		return
	}
	l, err := s.paperlessSendDocument(r.Context(), uid, d, true)
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
	inv, err := s.store.GetInvoice(r.Context(), uidOf(r), idParam(r, "id"))
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
	full, _ := s.store.GetInvoice(r.Context(), uidOf(r), inv.ID)
	s.decorateInvoicePaperless(r.Context(), full)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleAttachmentToPaperless(w http.ResponseWriter, r *http.Request) {
	if _, st, ok := s.requirePaperless(w, r); !ok {
		return
	} else {
		uid := uidOf(r)
		a, err := s.store.GetAttachment(r.Context(), uid, idParam(r, "aid"))
		if err != nil {
			s.fail(w, err, "get attachment")
			return
		}
		inv, err := s.store.GetInvoice(r.Context(), uid, a.InvoiceID)
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
		o.CorrespondentID, o.Correspondent = s.clientCorrespondent(r.Context(), uid, inv.ClientID, inv.ClientName, st)
		l, err := s.paperlessUpload(r.Context(), uid, "attachment", a.ID, a.Filename, data, o, true)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		s.store.LogActivity(r.Context(), uid, "invoice", inv.ID, "paperless", "Sent "+a.Filename+" to Paperless")
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
	if err := s.store.DeletePaperlessLink(r.Context(), uidOf(r), kind, idParam(r, "id")); err != nil {
		s.fail(w, err, "unlink")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// clientCorrespondent decides which Paperless correspondent an upload for a client gets:
// the one linked on the client, else (when enabled) one named after the client.
func (s *Server) clientCorrespondent(ctx context.Context, uid, clientID int64, clientName string, st store.Settings) (int64, string) {
	if clientID > 0 {
		if c, err := s.store.GetClient(ctx, uid, clientID); err == nil && c.PaperlessCorrespondentID > 0 {
			return c.PaperlessCorrespondentID, ""
		}
	}
	if st.PaperlessCreateCorrespondents {
		return 0, clientName
	}
	return 0, ""
}

func (s *Server) handlePaperlessCorrespondents(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		cl.Refresh()
	}
	writeJSON(w, http.StatusOK, cl.Entries(r.Context(), "correspondents"))
}

// handleCreateCorrespondent creates a correspondent in Paperless (or returns the existing one).
func (s *Server) handleCreateCorrespondent(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	var in struct {
		Name     string `json:"name"`
		ClientID int64  `json:"client_id"` // optional: link this client right away
	}
	_ = decode(r, &in)
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	id, err := cl.Ensure(r.Context(), "correspondents", in.Name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if in.ClientID > 0 {
		if c, err := s.store.GetClient(r.Context(), uidOf(r), in.ClientID); err == nil {
			c.PaperlessCorrespondentID, c.PaperlessCorrespondent = id, cl.Name(r.Context(), "correspondents", id)
			_ = s.store.UpdateClient(r.Context(), uidOf(r), c)
		}
	}
	writeJSON(w, http.StatusOK, paperless.Entry{ID: id, Name: cl.Name(r.Context(), "correspondents", id)})
}

// handleSyncCorrespondents links clients to correspondents by name and optionally creates missing ones.
func (s *Server) handleSyncCorrespondents(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := s.requirePaperless(w, r)
	if !ok {
		return
	}
	var in struct {
		Create bool `json:"create"`
	}
	_ = decode(r, &in)
	ctx := r.Context()
	cl.Refresh()
	uid := uidOf(r)
	clients, err := s.store.ListClients(ctx, uid, false, "")
	if err != nil {
		s.fail(w, err, "clients")
		return
	}
	entries := cl.Entries(ctx, "correspondents")
	matched, created, skipped := []string{}, []string{}, []string{}
	for i := range clients {
		c := &clients[i]
		if c.PaperlessCorrespondentID > 0 {
			continue
		}
		id := cl.Lookup(ctx, "correspondents", c.Name)
		if id == 0 {
			// Fuzzy: a correspondent whose name is contained in the client name or vice versa ("Athena Studio" ~ "Athena Studio S.à r.l.")
			lc := strings.ToLower(c.Name)
			for _, e := range entries {
				le := strings.ToLower(e.Name)
				if len(le) >= 4 && (strings.Contains(lc, le) || strings.Contains(le, lc)) {
					id = e.ID
					break
				}
			}
		}
		if id == 0 && in.Create {
			nid, err := cl.Ensure(ctx, "correspondents", c.Name)
			if err != nil {
				skipped = append(skipped, c.Name+": "+err.Error())
				continue
			}
			id = nid
			created = append(created, c.Name)
		} else if id > 0 {
			matched = append(matched, c.Name+" → "+cl.Name(ctx, "correspondents", id))
		}
		if id == 0 {
			skipped = append(skipped, c.Name)
			continue
		}
		c.PaperlessCorrespondentID, c.PaperlessCorrespondent = id, cl.Name(ctx, "correspondents", id)
		if err := s.store.UpdateClient(ctx, uid, c); err != nil {
			skipped = append(skipped, c.Name+": "+err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matched": matched, "created": created, "unmatched": skipped})
}
