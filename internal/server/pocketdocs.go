package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pocketdocs"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- Pocket Documents ----
//
// Issued invoices (the PDF and/or the uploaded files) and company documents
// are filed in the person's Pocket Documents, under the client as
// correspondent; the client page lists that correspondent's documents and the
// Documents page browses the archive. Like Garage → Money, chosen per person
// under Settings → Pocket Documents:
//
//	core   – a Pocket Core app link: Invoicing asks Core for a short-lived
//	         token that acts for the person in Documents (scopes
//	         documents:read + documents:write, see Core's docs/links.md);
//	         nothing to paste, and a switch-off in Core applies at once
//	direct – a URL (or Core's / DOCUMENTS_URL's address) and the person's own
//	         pd_ API token saved here
//	off    – Invoicing does not talk to Pocket Documents at all
//
// With no mode stored: direct when a token is saved, else Pocket Core when
// the box has one, else direct. Nothing is filed in anyone else's Documents:
// the token always belongs to the person whose invoice it is.

var documentsScopes = []string{"documents:read", "documents:write"}

// docsConn is how Invoicing reaches one person's Pocket Documents.
type docsConn struct {
	client *pocketdocs.Client
	via    string // core | direct
	public string // what a browser opens
	st     store.Settings
}

// docURL is the browser link to a document.
func (dc *docsConn) docURL(id int64) string {
	if dc == nil || id <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/documents/%d", strings.TrimRight(dc.public, "/"), id)
}

// documentsMode is the person's connection mode in effect.
func (s *Server) documentsMode(st store.Settings) string {
	return connMode(st.DocumentsMode, st.DocumentsToken, s.core.Configured())
}

// linkReason turns Core's refusal of a Pocket Documents link token into a sentence for the person.
func linkReason(reason string) string { return linkReasonFor("Pocket Documents", reason) }

// linkReasonFor turns Core's refusal of a link token to app into a sentence for the person.
func linkReasonFor(app, reason string) string {
	switch reason {
	case "off_person":
		return "you switched the Invoicing → " + app + " link off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "the Invoicing → " + app + " link is switched off in Pocket Core (Links)"
	case "target_missing":
		return app + " is not registered with Pocket Core"
	case "not_offered":
		return "this " + app + " version does not accept app links yet"
	case "not_declared":
		return "Pocket Core has not seen this Pocket Invoicing version yet; it re-registers within a minute of starting"
	case "not_in_audience":
		return "you do not have access to " + app + " in Pocket Core"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	case "no_account":
		return "you have no " + app + " account of your own yet"
	}
	return ""
}

// docsNames is the shared name cache for one person on one Documents instance
// (link tokens change on every use, the names do not).
func (s *Server) docsNames(uid int64, base string) *pocketdocs.Names {
	key := strconv.FormatInt(uid, 10) + "\x00" + base
	s.pdMu.Lock()
	defer s.pdMu.Unlock()
	if s.pdNames == nil {
		s.pdNames = map[string]*pocketdocs.Names{}
	}
	n, ok := s.pdNames[key]
	if !ok {
		n = &pocketdocs.Names{}
		s.pdNames[key] = n
	}
	return n
}

// docsDirectBase is where a direct connection goes: the person's URL, else
// what Core says, else DOCUMENTS_URL.
func (s *Server) docsDirectBase(ctx context.Context, st store.Settings) string {
	return strings.TrimRight(firstNonEmpty(st.DocumentsURL, s.resolveSibling(ctx, "documents"), s.cfg.DocumentsURL), "/")
}

// docsFor connects to the person's Pocket Documents following their mode.
func (s *Server) docsFor(ctx context.Context, uid int64) (*docsConn, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	switch s.documentsMode(st) {
	case modeOff:
		return nil, errors.New("Pocket Documents is switched off (Settings → Pocket Documents)")
	case modeDirect:
		if st.DocumentsToken == "" {
			return nil, errors.New("Pocket Documents is set to a direct connection but has no API token: add one in Settings → Pocket Documents, or switch to Pocket Core")
		}
		base := s.docsDirectBase(ctx, st)
		if base == "" {
			return nil, errors.New("set the Pocket Documents URL in Settings → Pocket Documents")
		}
		return &docsConn{client: pocketdocs.New(base, st.DocumentsToken, s.docsNames(uid, base)), via: modeDirect, public: firstNonEmpty(strings.TrimRight(st.DocumentsPublicURL, "/"), base), st: st}, nil
	}
	if !s.core.Configured() {
		return nil, errors.New("Pocket Documents through Pocket Core: there is no Pocket Core on this box; use a direct connection")
	}
	u, err := s.store.GetUser(ctx, uid)
	if err != nil {
		return nil, errors.New("pocket documents: no account to act for")
	}
	tok, err := s.core.ActAs(ctx, u.Email, "documents", documentsScopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			if t := linkReason(le.Reason); t != "" {
				return nil, errors.New(t)
			}
		}
		return nil, fmt.Errorf("Pocket Documents through Pocket Core: %w", err)
	}
	if tok.URL == "" || tok.AccessToken == "" {
		return nil, errors.New("Pocket Core gave no address or token for Pocket Documents")
	}
	base := strings.TrimRight(tok.URL, "/")
	return &docsConn{client: pocketdocs.New(base, tok.AccessToken, s.docsNames(uid, base)), via: modeCore, public: strings.TrimRight(firstNonEmpty(tok.PublicURL, tok.URL), "/"), st: st}, nil
}

// docsStatus says, without issuing a token, how the person reaches Pocket
// Documents: the mode in effect, whether it works ("core" / "direct" / "" for
// not connected) and why not.
func (s *Server) docsStatus(ctx context.Context, u *store.User, st store.Settings) (mode, via, detail string) {
	mode = s.documentsMode(st)
	switch mode {
	case modeOff:
		return mode, "", "switched off"
	case modeDirect:
		if st.DocumentsToken == "" {
			return mode, "", "add an API token"
		}
		if s.docsDirectBase(ctx, st) == "" {
			return mode, "", "add the URL"
		}
		return mode, modeDirect, ""
	}
	if u == nil {
		return mode, "", "nobody to act for"
	}
	if !s.core.Configured() {
		return mode, "", "there is no Pocket Core on this box"
	}
	on, reason := s.core.LinkOn(ctx, u.Email, "documents", documentsScopes...)
	if on {
		return mode, modeCore, ""
	}
	return mode, "", firstNonEmpty(linkReason(reason), reason)
}

func docsTags(st store.Settings) []string {
	var out []string
	for _, t := range strings.Split(st.DocumentsTags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// externalID names a local record in Pocket Documents, so filing it again finds the first copy.
func externalID(kind string, refID int64) string {
	return fmt.Sprintf("invoicing:%s:%d", kind, refID)
}

// fileInDocs uploads bytes for one local record and records the link. An
// upload Pocket Documents refuses as a duplicate (the same external id, or
// the same file) links to the document that is already there.
func (s *Server) fileInDocs(ctx context.Context, dc *docsConn, uid int64, kind string, refID int64, filename string, data []byte, o pocketdocs.UploadOptions) (*store.DocsLink, error) {
	if len(o.Tags) == 0 {
		o.Tags = docsTags(dc.st)
	}
	o.ExternalID = externalID(kind, refID)
	l := &store.DocsLink{Kind: kind, RefID: refID, Title: o.Title}
	d, err := dc.client.Upload(ctx, filename, data, o)
	var dup *pocketdocs.DuplicateError
	switch {
	case errors.As(err, &dup):
		l.DocumentsID = dup.ID
	case err != nil:
		l.Error = err.Error()
		_ = s.store.SaveDocsLink(ctx, uid, l)
		return nil, err
	default:
		l.DocumentsID = d.ID
	}
	if err := s.store.SaveDocsLink(ctx, uid, l); err != nil {
		return nil, err
	}
	l.URL = dc.docURL(l.DocumentsID)
	return l, nil
}

// clientCorrespondent is the correspondent a file for a client is filed
// under: the one linked on the client (by name), else (when enabled) one
// named after the client; both are created in Pocket Documents when missing.
func (s *Server) clientCorrespondent(ctx context.Context, uid, clientID int64, clientName string, st store.Settings) string {
	if clientID > 0 {
		if c, err := s.store.GetClient(ctx, uid, clientID); err == nil && strings.TrimSpace(c.DocumentsCorrespondent) != "" {
			return strings.TrimSpace(c.DocumentsCorrespondent)
		}
	}
	if st.DocumentsCreateCorrespondents {
		return clientName
	}
	return ""
}

// docsSendDocument files a company document.
func (s *Server) docsSendDocument(ctx context.Context, dc *docsConn, uid int64, d *store.Document) (*store.DocsLink, error) {
	data, err := os.ReadFile(s.documentPath(d))
	if err != nil {
		return nil, err
	}
	o := pocketdocs.UploadOptions{Title: d.Title, Created: d.DocDate}
	if dc.st.DocumentsCategoryAsType && d.Category != "" {
		o.DocumentType = d.Category
	}
	if d.ClientID != nil {
		o.Correspondent = s.clientCorrespondent(ctx, uid, *d.ClientID, d.ClientName, dc.st)
	}
	l, err := s.fileInDocs(ctx, dc, uid, "document", d.ID, d.Filename, data, o)
	if err == nil {
		s.store.LogActivity(ctx, uid, "document", d.ID, "filed", fmt.Sprintf("Filed in Pocket Documents (#%d)", l.DocumentsID))
	}
	return l, err
}

// docsSendDocumentAsync files company documents in the background (upload with "also file").
func (s *Server) docsSendDocumentAsync(uid int64, docs []store.Document) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dc, err := s.docsFor(ctx, uid)
		if err != nil {
			return
		}
		for i := range docs {
			if _, err := s.docsSendDocument(ctx, dc, uid, &docs[i]); err != nil {
				s.log.Warn("pocket documents: filing a document failed", "document", docs[i].ID, "err", err)
			}
		}
	}()
}

// docsFileInvoice files an invoice: the generated PDF and/or the uploaded
// files, following the same mode as email attachments. Parts already filed
// are skipped.
func (s *Server) docsFileInvoice(ctx context.Context, inv *store.Invoice, source string) ([]*store.DocsLink, error) {
	uid := inv.UserID
	dc, err := s.docsFor(ctx, uid)
	if err != nil {
		return nil, err
	}
	st := dc.st
	if source == "" {
		source = firstNonEmpty(st.EmailAttachmentMode, "generated")
	}
	base := pocketdocs.UploadOptions{Created: inv.IssueDate, DocumentType: st.DocumentsInvoiceType}
	base.Correspondent = s.clientCorrespondent(ctx, uid, inv.ClientID, inv.ClientName, st)
	title := fmt.Sprintf("Invoice %s – %s", inv.Number, inv.ClientName)
	var out []*store.DocsLink
	var firstErr error
	if source == "generated" || source == "both" || (source == "uploaded" && len(inv.Attachments) == 0) {
		if l, _ := s.store.GetDocsLink(ctx, uid, "invoice", inv.ID); l == nil || l.DocumentsID == 0 {
			data, err := s.renderPDF(ctx, inv)
			if err != nil {
				return nil, err
			}
			o := base
			o.Title = title
			l, err := s.fileInDocs(ctx, dc, uid, "invoice", inv.ID, safeFilename(inv.Number)+".pdf", data, o)
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
			if l, _ := s.store.GetDocsLink(ctx, uid, "attachment", a.ID); l != nil && l.DocumentsID > 0 {
				continue
			}
			data, err := os.ReadFile(s.attachmentPath(&a))
			if err != nil {
				continue
			}
			o := base
			o.Title = title
			if len(inv.Attachments) > 1 {
				o.Title += " (" + strings.TrimSuffix(a.Filename, filepath.Ext(a.Filename)) + ")"
			}
			l, err := s.fileInDocs(ctx, dc, uid, "attachment", a.ID, a.Filename, data, o)
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
		s.store.LogActivity(ctx, uid, "invoice", inv.ID, "filed", fmt.Sprintf("Filed in Pocket Documents (%d file(s))", len(out)))
	}
	return out, firstErr
}

// docsAutoFile runs in the background after a status change when the
// setting asks for it ("sent" also covers paid, since a paid invoice was sent).
func (s *Server) docsAutoFile(inv *store.Invoice) {
	if inv == nil {
		return
	}
	st, err := s.store.GetSettings(context.Background(), inv.UserID)
	if err != nil || s.documentsMode(st) == modeOff {
		return
	}
	want := false
	switch st.DocumentsArchiveInvoices {
	case "sent":
		want = inv.Status == store.StatusSent || inv.Status == store.StatusViewed || inv.Status == store.StatusPartial || inv.Status == store.StatusPaid || inv.Status == store.StatusOverdue
	case "paid":
		want = inv.Status == store.StatusPaid
	}
	if !want {
		return
	}
	uid, id := inv.UserID, inv.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		full, err := s.store.GetInvoice(ctx, uid, id)
		if err != nil {
			return
		}
		if _, err := s.docsFileInvoice(ctx, full, ""); err != nil {
			s.log.Warn("pocket documents: filing the invoice automatically failed", "invoice", full.Number, "err", err)
		}
	}()
}

// docsPublicFor is the browser address of the person's Pocket Documents
// without issuing a token: the saved one for direct, Core's for core.
func (s *Server) docsPublicFor(ctx context.Context, uid int64, st store.Settings) string {
	switch s.documentsMode(st) {
	case modeDirect:
		return firstNonEmpty(strings.TrimRight(st.DocumentsPublicURL, "/"), s.docsDirectBase(ctx, st))
	case modeCore:
		return strings.TrimRight(s.resolveSiblingPublic(ctx, "documents"), "/")
	}
	return ""
}

// withURL fills a link's browser URL.
func withURL(l *store.DocsLink, public string) *store.DocsLink {
	if l != nil && l.DocumentsID > 0 && public != "" {
		l.URL = fmt.Sprintf("%s/documents/%d", strings.TrimRight(public, "/"), l.DocumentsID)
	}
	return l
}

// decorateInvoiceDocs adds link info to an invoice and its attachments.
func (s *Server) decorateInvoiceDocs(ctx context.Context, inv *store.Invoice) {
	st, _ := s.store.GetSettings(ctx, inv.UserID)
	if s.documentsMode(st) == modeOff {
		return
	}
	l, _ := s.store.GetDocsLink(ctx, inv.UserID, "invoice", inv.ID)
	var links map[int64]*store.DocsLink
	if len(inv.Attachments) > 0 {
		ids := make([]int64, len(inv.Attachments))
		for i := range inv.Attachments {
			ids[i] = inv.Attachments[i].ID
		}
		links, _ = s.store.DocsLinksFor(ctx, inv.UserID, "attachment", ids)
	}
	if l == nil && len(links) == 0 {
		return // nothing filed: Core is not asked
	}
	public := s.docsPublicFor(ctx, inv.UserID, st)
	inv.PocketDocs = withURL(l, public)
	for i := range inv.Attachments {
		if al, ok := links[inv.Attachments[i].ID]; ok {
			inv.Attachments[i].PocketDocs = withURL(al, public)
		}
	}
}

// docsLinkFor loads one record's link with its browser URL.
func (s *Server) docsLinkFor(ctx context.Context, uid int64, kind string, refID int64) *store.DocsLink {
	l, err := s.store.GetDocsLink(ctx, uid, kind, refID)
	if err != nil || l == nil {
		return nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	return withURL(l, s.docsPublicFor(ctx, uid, st))
}

// adoptPaperlessLinks is the scheduler step that carries a person's old
// Paperless links over: a document imported from Paperless into Pocket
// Documents carries the external id "paperless:<id>". One that is not there
// is recorded as not filed, so it can be filed again (and is not looked up
// on every run).
func (s *Server) adoptPaperlessLinks(ctx context.Context, uid int64) int {
	// the database first: almost always nothing is left, and then Core is not asked
	old, err := s.store.PaperlessLinksToAdopt(ctx, uid)
	if err != nil || len(old) == 0 {
		return 0
	}
	dc, err := s.docsFor(ctx, uid)
	if err != nil {
		return 0
	}
	n := 0
	for _, p := range old {
		d, err := dc.client.FindByExternalID(ctx, fmt.Sprintf("paperless:%d", p.PaperlessID))
		if err != nil {
			return n // try again on the next run
		}
		l := &store.DocsLink{Kind: p.Kind, RefID: p.RefID, Title: p.Title}
		if d != nil {
			l.DocumentsID = d.ID
			n++
		} else {
			l.Error = fmt.Sprintf("was Paperless #%d, which is not in Pocket Documents; file it again", p.PaperlessID)
		}
		_ = s.store.SaveDocsLink(ctx, uid, l)
	}
	return n
}

// ---------- HTTP handlers ----------

func (s *Server) handleDocsStatus(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), uidOf(r)
	st, _ := s.store.GetSettings(ctx, uid)
	mode, via, detail := s.docsStatus(ctx, userFrom(ctx), st)
	out := map[string]any{"configured": via != "", "mode": mode, "via": via, "detail": detail, "archive_mode": st.DocumentsArchiveInvoices}
	if via == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dc, err := s.docsFor(pctx, uid)
	if err != nil {
		out["ok"], out["error"] = false, err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	ps := dc.client.Ping(pctx)
	out["ok"], out["error"], out["document_count"], out["url"] = ps.OK, ps.Error, ps.DocumentCount, dc.public
	writeJSON(w, http.StatusOK, out)
}

// handleTestDocs checks the connection: a URL/token typed into the form is
// tried as is (before it is saved); otherwise the person's mode decides
// (Pocket Core, the saved URL and token, or off).
func (s *Server) handleTestDocs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	_ = decode(r, &in)
	uid := uidOf(r)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var dc *docsConn
	if strings.TrimSpace(in.Token) != "" {
		st, _ := s.store.GetSettings(ctx, uid)
		st.DocumentsURL = firstNonEmpty(strings.TrimSpace(in.URL), st.DocumentsURL)
		base := s.docsDirectBase(ctx, st)
		if base == "" {
			writeErr(w, http.StatusBadRequest, "the URL is required")
			return
		}
		dc = &docsConn{client: pocketdocs.New(base, in.Token, nil), via: modeDirect, public: base}
	} else {
		var err error
		if dc, err = s.docsFor(ctx, uid); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ps := dc.client.Ping(ctx)
	if !ps.OK {
		writeErr(w, http.StatusBadGateway, ps.Error)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "document_count": ps.DocumentCount, "via": dc.via, "url": dc.public})
}

func (s *Server) requireDocs(w http.ResponseWriter, r *http.Request) (*docsConn, bool) {
	dc, err := s.docsFor(r.Context(), uidOf(r))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return nil, false
	}
	return dc, true
}

// handleDocsSearch lists the person's documents in Pocket Documents: a full
// text query (?q=), or one client's (?client_id=, by the client's correspondent).
func (s *Server) handleDocsSearch(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), uidOf(r)
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("page_size"))
	f := pocketdocs.Filter{Query: q.Get("q")}
	out := map[string]any{}
	if cid, _ := strconv.ParseInt(q.Get("client_id"), 10, 64); cid > 0 {
		c, err := s.store.GetClient(ctx, uid, cid)
		if err != nil {
			s.fail(w, err, "client")
			return
		}
		name := s.clientCorrespondent(ctx, uid, c.ID, c.Name, dc.st)
		out["correspondent"] = name
		if name != "" {
			f.CorrespondentID = dc.client.Lookup(ctx, pocketdocs.Correspondents, name)
		}
		out["correspondent_id"] = f.CorrespondentID
		if f.CorrespondentID == 0 {
			// no such correspondent in Documents yet: nothing filed for the client
			out["count"], out["page"], out["pages"], out["results"], out["linked"] = 0, 1, 0, []pocketdocs.Document{}, map[int64]*store.DocsLink{}
			out["url"] = dc.public
			writeJSON(w, http.StatusOK, out)
			return
		}
		out["url"] = fmt.Sprintf("%s/documents?correspondent=%d", dc.public, f.CorrespondentID)
	} else {
		out["url"] = dc.public
	}
	res, err := dc.client.List(ctx, f, page, size)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// browser links, and the documents already linked to a local record
	linked := map[int64]*store.DocsLink{}
	for i := range res.Results {
		res.Results[i].URL = dc.docURL(res.Results[i].ID)
		if l, _ := s.store.DocsLinkByDocumentsID(ctx, uid, res.Results[i].ID); l != nil {
			linked[res.Results[i].ID] = l
		}
	}
	out["count"], out["page"], out["pages"], out["results"], out["linked"] = res.Count, res.Page, res.Pages, res.Results, linked
	writeJSON(w, http.StatusOK, out)
}

// handleDocsFile proxies the file or thumbnail so the browser never sees the token.
func (s *Server) handleDocsFile(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	id := idParam(r, "pid")
	download := r.URL.Query().Get("download") == "1"
	var resp *http.Response
	var err error
	if strings.HasSuffix(r.URL.Path, "/thumb") {
		resp, err = dc.client.Thumbnail(r.Context(), id)
	} else {
		resp, err = dc.client.Download(r.Context(), id, r.URL.Query().Get("original") == "1", !download)
	}
	if err != nil {
		var he *pocketdocs.HTTPError
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			writeErr(w, http.StatusNotFound, "not found in Pocket Documents")
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", firstNonEmpty(resp.Header.Get("Content-Type"), "application/octet-stream"))
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if !download {
			cd = strings.Replace(cd, "attachment", "inline", 1)
		}
		w.Header().Set("Content-Disposition", cd)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = io.Copy(w, resp.Body)
}

// handleDocsImport copies a Pocket Documents document into company documents.
func (s *Server) handleDocsImport(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	ctx, uid := r.Context(), uidOf(r)
	id := idParam(r, "pid")
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid document id")
		return
	}
	var in struct {
		Category string `json:"category"`
		ClientID *int64 `json:"client_id"`
		Original bool   `json:"original"`
	}
	_ = decode(r, &in)
	in.ClientID = s.ownClientID(ctx, uid, in.ClientID)
	if l, _ := s.store.DocsLinkByDocumentsID(ctx, uid, id); l != nil && l.Kind == "document" {
		if d, err := s.store.GetDocument(ctx, uid, l.RefID); err == nil {
			d.PocketDocs = withURL(l, dc.public)
			writeJSON(w, http.StatusOK, d)
			return
		}
	}
	pd, err := dc.client.Get(ctx, id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	resp, err := dc.client.Download(ctx, id, in.Original, false)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	name = cleanFilename(firstNonEmpty(name, pd.OriginalFileName, safeFilename(pd.Title)+".pdf"))
	stored, ct, size, err := s.storeDocumentFile(name, resp.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if ct == "application/octet-stream" && resp.Header.Get("Content-Type") != "" {
		ct = resp.Header.Get("Content-Type")
	}
	d := &store.Document{Title: firstNonEmpty(pd.Title, titleFromFilename(name)), Category: strings.TrimSpace(in.Category), ClientID: in.ClientID, DocDate: pd.Created, Filename: name, StoredName: stored, ContentType: ct, Size: size}
	if d.Category == "" && dc.st.DocumentsCategoryAsType {
		d.Category = pd.DocumentType
	}
	if d.ClientID == nil && pd.Correspondent != "" {
		d.ClientID = s.clientByCorrespondent(ctx, uid, pd.Correspondent)
	}
	if err := s.store.CreateDocument(ctx, uid, d); err != nil {
		os.Remove(filepath.Join(s.cfg.DataDir, "documents", stored))
		s.fail(w, err, "create document")
		return
	}
	l := &store.DocsLink{Kind: "document", RefID: d.ID, DocumentsID: pd.ID, Title: pd.Title}
	_ = s.store.SaveDocsLink(ctx, uid, l)
	s.store.LogActivity(ctx, uid, "document", d.ID, "created", "Imported from Pocket Documents #"+strconv.FormatInt(pd.ID, 10))
	d, _ = s.store.GetDocument(ctx, uid, d.ID)
	d.PocketDocs = withURL(l, dc.public)
	writeJSON(w, http.StatusCreated, d)
}

// clientByCorrespondent finds the person's client a correspondent stands
// for: linked by name, else named the same.
func (s *Server) clientByCorrespondent(ctx context.Context, uid int64, name string) *int64 {
	clients, err := s.store.ListClients(ctx, uid, true, "")
	if err != nil {
		return nil
	}
	for _, match := range []func(store.Client) bool{
		func(c store.Client) bool { return strings.EqualFold(strings.TrimSpace(c.DocumentsCorrespondent), name) },
		func(c store.Client) bool { return strings.EqualFold(c.Name, name) },
	} {
		for _, c := range clients {
			if match(c) {
				id := c.ID
				return &id
			}
		}
	}
	return nil
}

func (s *Server) handleDocumentToDocs(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	ctx, uid := r.Context(), uidOf(r)
	d, err := s.store.GetDocument(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	if l, _ := s.store.GetDocsLink(ctx, uid, "document", d.ID); l != nil && l.DocumentsID > 0 && r.URL.Query().Get("force") != "1" {
		writeJSON(w, http.StatusOK, withURL(l, dc.public))
		return
	}
	l, err := s.docsSendDocument(ctx, dc, uid, d)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleInvoiceToDocs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireDocs(w, r); !ok {
		return
	}
	ctx, uid := r.Context(), uidOf(r)
	inv, err := s.store.GetInvoice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	var in struct {
		Source string `json:"source"` // generated | uploaded | both | "" (= email attachment mode)
	}
	_ = decode(r, &in)
	links, err := s.docsFileInvoice(ctx, inv, in.Source)
	if err != nil && len(links) == 0 {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	full, _ := s.store.GetInvoice(ctx, uid, inv.ID)
	s.decorateInvoiceDocs(ctx, full)
	writeJSON(w, http.StatusOK, full)
}

func (s *Server) handleAttachmentToDocs(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	ctx, uid := r.Context(), uidOf(r)
	a, err := s.store.GetAttachment(ctx, uid, idParam(r, "aid"))
	if err != nil {
		s.fail(w, err, "get attachment")
		return
	}
	inv, err := s.store.GetInvoice(ctx, uid, a.InvoiceID)
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	data, err := os.ReadFile(s.attachmentPath(a))
	if err != nil {
		writeErr(w, http.StatusNotFound, "file missing on disk")
		return
	}
	o := pocketdocs.UploadOptions{Title: fmt.Sprintf("Invoice %s – %s", inv.Number, inv.ClientName), Created: inv.IssueDate, DocumentType: dc.st.DocumentsInvoiceType}
	o.Correspondent = s.clientCorrespondent(ctx, uid, inv.ClientID, inv.ClientName, dc.st)
	l, err := s.fileInDocs(ctx, dc, uid, "attachment", a.ID, a.Filename, data, o)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.store.LogActivity(ctx, uid, "invoice", inv.ID, "filed", "Filed "+a.Filename+" in Pocket Documents")
	writeJSON(w, http.StatusOK, l)
}

// handleUnlinkDocs forgets a link (the document stays in Pocket Documents).
func (s *Server) handleUnlinkDocs(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	switch kind {
	case "document", "invoice", "attachment":
	default:
		writeErr(w, http.StatusBadRequest, "kind must be document, invoice or attachment")
		return
	}
	if err := s.store.DeleteDocsLink(r.Context(), uidOf(r), kind, idParam(r, "id")); err != nil {
		s.fail(w, err, "unlink")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDocsCorrespondents(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		dc.client.Refresh(r.Context())
	}
	writeJSON(w, http.StatusOK, dc.client.Entries(r.Context(), pocketdocs.Correspondents))
}

// handleCreateCorrespondent creates a correspondent in Pocket Documents (or returns the existing one).
func (s *Server) handleCreateCorrespondent(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
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
	ctx, uid := r.Context(), uidOf(r)
	id, err := dc.client.Ensure(ctx, pocketdocs.Correspondents, in.Name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	name := firstNonEmpty(dc.client.Name(ctx, pocketdocs.Correspondents, id), in.Name)
	if in.ClientID > 0 {
		if c, err := s.store.GetClient(ctx, uid, in.ClientID); err == nil {
			c.DocumentsCorrespondent = name
			_ = s.store.UpdateClient(ctx, uid, c)
		}
	}
	writeJSON(w, http.StatusOK, pocketdocs.Entry{ID: id, Name: name})
}

// handleSyncCorrespondents links clients to correspondents by name and optionally creates missing ones.
func (s *Server) handleSyncCorrespondents(w http.ResponseWriter, r *http.Request) {
	dc, ok := s.requireDocs(w, r)
	if !ok {
		return
	}
	var in struct {
		Create bool `json:"create"`
	}
	_ = decode(r, &in)
	ctx, uid := r.Context(), uidOf(r)
	dc.client.Refresh(ctx)
	clients, err := s.store.ListClients(ctx, uid, false, "")
	if err != nil {
		s.fail(w, err, "clients")
		return
	}
	entries := dc.client.Entries(ctx, pocketdocs.Correspondents)
	matched, created, skipped := []string{}, []string{}, []string{}
	for i := range clients {
		c := &clients[i]
		if strings.TrimSpace(c.DocumentsCorrespondent) != "" {
			continue
		}
		id := dc.client.Lookup(ctx, pocketdocs.Correspondents, c.Name)
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
			nid, err := dc.client.Ensure(ctx, pocketdocs.Correspondents, c.Name)
			if err != nil {
				skipped = append(skipped, c.Name+": "+err.Error())
				continue
			}
			id = nid
			created = append(created, c.Name)
		} else if id > 0 {
			matched = append(matched, c.Name+" → "+dc.client.Name(ctx, pocketdocs.Correspondents, id))
		}
		if id == 0 {
			skipped = append(skipped, c.Name)
			continue
		}
		full, err := s.store.GetClient(ctx, uid, c.ID) // the list leaves some fields out
		if err != nil {
			skipped = append(skipped, c.Name+": "+err.Error())
			continue
		}
		full.DocumentsCorrespondent = firstNonEmpty(dc.client.Name(ctx, pocketdocs.Correspondents, id), c.Name)
		if err := s.store.UpdateClient(ctx, uid, full); err != nil {
			skipped = append(skipped, c.Name+": "+err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matched": matched, "created": created, "unmatched": skipped})
}
