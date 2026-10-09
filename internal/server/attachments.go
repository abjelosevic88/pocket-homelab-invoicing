package server

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

const maxAttachmentSize = 25 << 20

func (s *Server) attachmentPath(a *store.Attachment) string {
	return filepath.Join(s.cfg.DataDir, "attachments", fmt.Sprint(a.InvoiceID), filepath.Base(a.StoredName))
}

// saveAttachment stores an uploaded file for an invoice.
func (s *Server) saveAttachment(ctx context.Context, invoiceID int64, filename string, r io.Reader) (*store.Attachment, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "" || filename == "." {
		filename = "attachment"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	dir := filepath.Join(s.cfg.DataDir, "attachments", fmt.Sprint(invoiceID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	stored := store.RandomToken(12) + ext
	f, err := os.Create(filepath.Join(dir, stored))
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxAttachmentSize+1))
	f.Close()
	if err != nil {
		os.Remove(filepath.Join(dir, stored))
		return nil, err
	}
	if n > maxAttachmentSize {
		os.Remove(filepath.Join(dir, stored))
		return nil, fmt.Errorf("file exceeds 25 MB")
	}
	a := &store.Attachment{InvoiceID: invoiceID, Filename: filename, StoredName: stored, ContentType: ct, Size: n}
	if err := s.store.CreateAttachment(ctx, a); err != nil {
		os.Remove(filepath.Join(dir, stored))
		return nil, err
	}
	return a, nil
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	inv, err := s.store.GetInvoice(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get invoice")
		return
	}
	if err := r.ParseMultipartForm(maxAttachmentSize); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid upload: "+err.Error())
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		files = r.MultipartForm.File["files"]
	}
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "no file field 'file'")
		return
	}
	var saved []store.Attachment
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			s.fail(w, err, "open upload")
			return
		}
		a, err := s.saveAttachment(ctx, inv.ID, fh.Filename, f)
		f.Close()
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		saved = append(saved, *a)
	}
	s.store.LogActivity(ctx, "invoice", inv.ID, "attachment", fmt.Sprintf("%d file(s) attached", len(saved)))
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListAttachments(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list attachments")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, a *store.Attachment) {
	p := s.attachmentPath(a)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing on disk")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeFilename(a.Filename)))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, p)
}

func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAttachment(r.Context(), idParam(r, "aid"))
	if err != nil {
		s.fail(w, err, "get attachment")
		return
	}
	s.serveAttachment(w, r, a)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAttachment(r.Context(), idParam(r, "aid"))
	if err != nil {
		s.fail(w, err, "get attachment")
		return
	}
	_ = os.Remove(s.attachmentPath(a))
	_ = s.store.DeletePaperlessLink(r.Context(), "attachment", a.ID)
	if err := s.store.DeleteAttachment(r.Context(), a.ID); err != nil {
		s.fail(w, err, "delete attachment")
		return
	}
	s.store.LogActivity(r.Context(), "invoice", a.InvoiceID, "attachment", "Removed "+a.Filename)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handlePublicAttachment serves an attachment via the public invoice link.
func (s *Server) handlePublicAttachment(w http.ResponseWriter, r *http.Request) {
	inv := s.publicInvoice(w, r)
	if inv == nil {
		return
	}
	a, err := s.store.GetAttachment(r.Context(), idParam(r, "aid"))
	if err != nil || a.InvoiceID != inv.ID {
		http.NotFound(w, r)
		return
	}
	s.serveAttachment(w, r, a)
}

// removeAttachmentFiles deletes all files for an invoice (used on invoice delete).
func (s *Server) removeAttachmentFiles(invoiceID int64) {
	_ = os.RemoveAll(filepath.Join(s.cfg.DataDir, "attachments", fmt.Sprint(invoiceID)))
}
