package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- Pocket Sign: get an invoice signed ----
//
// "Get it signed" on an invoice sends its PDF to the client through the
// person's Pocket Sign (pocket-homelab-sign, docs/design.md): Sign makes a
// document with a signature and date box for the client at the end, emails
// them their signing link, and keeps the history and the signed PDF. Handy
// for an invoice that doubles as a quote to accept or a delivery note to
// confirm. The document remembers the invoice (source_ref = its number,
// source_url = its page here); a line in the invoice's activity says when
// and to whom it went.
//
// Chosen per person under Settings → Pocket Sign, like Pocket Time: core (a
// Pocket Core app link, signing:write), direct (a URL and the person's psg_
// API token) or off. Not connected, the button is not shown.

var signScopes = []string{"signing:write"}

// signConn is how Invoicing reaches one person's Pocket Sign.
type signConn struct {
	base, public, token, via string
	http                     *http.Client
}

// signOff says Pocket Sign is not connected for the person.
type signOff struct{ msg string }

func (e *signOff) Error() string { return e.msg }

func isSignOff(err error) bool {
	var off *signOff
	return errors.As(err, &off)
}

// signMode is the person's Pocket Sign connection mode in effect.
func (s *Server) signMode(st store.Settings) string {
	return connMode(st.SignMode, st.SignToken, s.core.Configured())
}

func (s *Server) signDirectBase(ctx context.Context, st store.Settings) string {
	return strings.TrimRight(firstNonEmpty(st.SignURL, s.resolveSibling(ctx, "sign")), "/")
}

var signHTTP = &http.Client{Timeout: 60 * time.Second}

// signFor connects to the person's Pocket Sign following their mode. A
// *signOff error means "not connected".
func (s *Server) signFor(ctx context.Context, uid int64) (*signConn, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	switch s.signMode(st) {
	case modeOff:
		return nil, &signOff{"Pocket Sign is switched off (Settings → Pocket Sign)"}
	case modeDirect:
		if st.SignToken == "" {
			return nil, &signOff{"Pocket Sign is set to a direct connection but has no API token"}
		}
		base := s.signDirectBase(ctx, st)
		if base == "" {
			return nil, &signOff{"set the Pocket Sign URL in Settings → Pocket Sign"}
		}
		public := firstNonEmpty(strings.TrimRight(s.resolveSiblingPublic(ctx, "sign"), "/"), base)
		return &signConn{base: base, public: public, token: st.SignToken, via: modeDirect, http: signHTTP}, nil
	}
	if !s.core.Configured() {
		return nil, &signOff{"there is no Pocket Core on this box"}
	}
	u, err := s.store.GetUser(ctx, uid)
	if err != nil {
		return nil, &signOff{"no account to act for"}
	}
	tok, err := s.core.ActAs(ctx, u.Email, "sign", signScopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			return nil, &signOff{firstNonEmpty(linkReasonFor("Pocket Sign", le.Reason), le.Reason)}
		}
		if errors.Is(err, core.ErrNotConfigured) {
			return nil, &signOff{"there is no Pocket Core on this box"}
		}
		return nil, fmt.Errorf("Pocket Core did not answer, so Pocket Sign can't be reached: %w", err)
	}
	if tok.URL == "" || tok.AccessToken == "" {
		return nil, errors.New("Pocket Core gave no address or token for Pocket Sign")
	}
	base := strings.TrimRight(tok.URL, "/")
	return &signConn{base: base, public: firstNonEmpty(strings.TrimRight(tok.PublicURL, "/"), base), token: tok.AccessToken, via: modeCore, http: signHTTP}, nil
}

// signStatus says, without issuing a token, how the person reaches Pocket
// Sign: the mode in effect, "core" / "direct" / "" (not connected) and why not.
func (s *Server) signStatus(ctx context.Context, u *store.User, st store.Settings) (mode, via, detail string) {
	mode = s.signMode(st)
	switch mode {
	case modeOff:
		return mode, "", "switched off"
	case modeDirect:
		if st.SignToken == "" {
			return mode, "", "add an API token"
		}
		if s.signDirectBase(ctx, st) == "" {
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
	on, reason := s.core.LinkOn(ctx, u.Email, "sign", signScopes...)
	if on {
		return mode, modeCore, ""
	}
	return mode, "", firstNonEmpty(linkReasonFor("Pocket Sign", reason), reason)
}

// call sends one request to Pocket Sign and decodes the answer into out.
// Refusals come back as errors in plain words.
func (sc *signConn) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, sc.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+sc.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := sc.http.Do(req)
	if err != nil {
		return errors.New("Pocket Sign did not answer")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return errors.New("Pocket Sign did not accept the token (check Settings → Pocket Sign)")
	case res.StatusCode == http.StatusForbidden:
		return errors.New("Pocket Sign did not allow it: switch the Invoicing → Pocket Sign link on in Pocket Core → Links")
	case res.StatusCode == http.StatusNotFound:
		return errors.New("this Pocket Sign version can't take documents from other apps yet (update it)")
	case res.StatusCode >= 300:
		var e apiError
		_ = json.Unmarshal(raw, &e)
		if e.Error != "" {
			return errors.New("Pocket Sign: " + e.Error)
		}
		return fmt.Errorf("Pocket Sign answered HTTP %d", res.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("Pocket Sign sent an answer this version does not understand: %w", err)
		}
	}
	return nil
}

// signResult is what "Get it signed" answers: the document in Pocket Sign.
type signResult struct {
	DocumentID int64  `json:"document_id"`
	Status     string `json:"status"`
	URL        string `json:"url"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Emailed    bool   `json:"emailed"`
	Link       string `json:"link,omitempty"` // the client's signing link, when it was not emailed
	Note       string `json:"note,omitempty"`
}

// signedDoc reads Sign's answer to POST /documents: the document itself, or
// {document, emails} when it was sent in the same call.
type signedDoc struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	Recipients []struct {
		Email      string `json:"email"`
		Link       string `json:"link"`
		EmailError string `json:"email_error"`
	} `json:"recipients"`
}

// handleInvoiceSign sends the invoice PDF to the client to sign in Pocket
// Sign. Body (all optional): name and email of who signs (default the
// client's contact and email) and the message in their email.
func (s *Server) handleInvoiceSign(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), uidOf(r)
	var in struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Message string `json:"message"`
	}
	_ = decode(r, &in)
	inv, err := s.store.GetInvoice(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	name, email := strings.TrimSpace(in.Name), strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || name == "" {
		if c, err := s.store.GetClient(ctx, uid, inv.ClientID); err == nil {
			if email == "" {
				email = strings.ToLower(strings.TrimSpace(c.Email))
			}
			if name == "" {
				name = firstNonEmpty(strings.TrimSpace(c.ContactName), strings.TrimSpace(c.Name))
			}
		}
	}
	name = firstNonEmpty(name, inv.ClientName)
	if email == "" {
		writeErr(w, http.StatusBadRequest, "who should sign it? The client has no email: type one")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		writeErr(w, http.StatusBadRequest, "that email address does not look right")
		return
	}
	sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	sc, err := s.signFor(sctx, uid)
	if err != nil {
		status := http.StatusBadGateway
		if isSignOff(err) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	pdf, err := s.renderPDF(sctx, inv)
	if err != nil {
		s.fail(w, err, "render pdf")
		return
	}
	title := "Invoice " + inv.Number
	if inv.ClientName != "" {
		title += " — " + inv.ClientName
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		msg = "Please sign invoice " + inv.Number + " to confirm you accept it."
	}
	page := ""
	if s.cfg.BaseURL != "" {
		page = fmt.Sprintf("%s/invoices/%d", s.cfg.BaseURL, inv.ID)
	}
	body := map[string]any{
		"title": title, "message": msg, "pdf_base64": base64.StdEncoding.EncodeToString(pdf), "filename": safeFilename(inv.Number) + ".pdf",
		"recipients":  []map[string]any{{"name": name, "email": email, "role": "signer"}},
		"auto_fields": true, "send": true, "source_ref": inv.Number, "source_url": page,
	}
	var raw json.RawMessage
	if err := sc.call(sctx, http.MethodPost, "/api/v1/documents", body, &raw); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	var doc signedDoc
	var wrapped struct {
		Document *signedDoc `json:"document"`
		Emails   []struct {
			Sent  bool   `json:"sent"`
			Error string `json:"error"`
		} `json:"emails"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Document != nil {
		doc = *wrapped.Document
	} else if err := json.Unmarshal(raw, &doc); err != nil || doc.ID == 0 {
		writeErr(w, http.StatusBadGateway, "Pocket Sign sent an answer this version does not understand")
		return
	}
	res := signResult{DocumentID: doc.ID, Status: doc.Status, Name: name, Email: email}
	if sc.public != "" {
		res.URL = fmt.Sprintf("%s/documents/%d", sc.public, doc.ID)
	}
	res.Emailed = doc.Status != "draft"
	for _, e := range wrapped.Emails {
		res.Emailed = e.Sent
		if !e.Sent && e.Error != "" {
			res.Note = e.Error
		}
	}
	for _, rc := range doc.Recipients {
		if strings.EqualFold(rc.Email, email) {
			if rc.EmailError != "" {
				res.Emailed, res.Note = false, rc.EmailError
			}
			if !res.Emailed {
				res.Link = rc.Link
			}
		}
	}
	what := "Sent for signing in Pocket Sign to " + name + " <" + email + ">"
	if doc.Status == "draft" {
		what = "Prepared for signing in Pocket Sign (a draft) for " + name + " <" + email + ">"
	} else if !res.Emailed {
		what += " (not emailed: share the link from Pocket Sign)"
	}
	s.store.LogActivity(ctx, uid, "invoice", inv.ID, "sign", what)
	writeJSON(w, http.StatusOK, res)
}

// handleSignStatus tells the invoice page whether "Get it signed" can work.
func (s *Server) handleSignStatus(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), uidOf(r)
	st, _ := s.store.GetSettings(ctx, uid)
	mode, via, detail := s.signStatus(ctx, userFrom(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"configured": via != "", "mode": mode, "via": via, "detail": detail})
}

// handleTestSign checks the Pocket Sign connection (the saved one, or a URL + token being typed).
func (s *Server) handleTestSign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	_ = decode(r, &in)
	uid := uidOf(r)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var sc *signConn
	if strings.TrimSpace(in.Token) != "" {
		st, _ := s.store.GetSettings(ctx, uid)
		st.SignURL = firstNonEmpty(strings.TrimRight(strings.TrimSpace(in.URL), "/"), st.SignURL)
		base := s.signDirectBase(ctx, st)
		if base == "" {
			writeErr(w, http.StatusBadRequest, "the URL is required")
			return
		}
		sc = &signConn{base: base, token: strings.TrimSpace(in.Token), via: modeDirect, http: signHTTP}
	} else {
		var err error
		if sc, err = s.signFor(ctx, uid); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var me struct {
		Email string `json:"email"`
	}
	if err := sc.call(ctx, http.MethodGet, "/api/v1/auth/me", nil, &me); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "via": sc.via, "email": me.Email})
}
