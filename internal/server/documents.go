package server

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

const maxDocumentSize = 50 << 20

func (s *Server) documentPath(d *store.Document) string {
	return filepath.Join(s.cfg.DataDir, "documents", filepath.Base(d.StoredName))
}

// storeDocumentFile writes an uploaded file into the documents folder.
func (s *Server) storeDocumentFile(filename string, r io.Reader) (stored, contentType string, size int64, err error) {
	ext := strings.ToLower(filepath.Ext(filename))
	contentType = mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	dir := filepath.Join(s.cfg.DataDir, "documents")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	stored = store.RandomToken(12) + ext
	full := filepath.Join(dir, stored)
	f, err := os.Create(full)
	if err != nil {
		return
	}
	size, err = io.Copy(f, io.LimitReader(r, maxDocumentSize+1))
	f.Close()
	if err != nil {
		os.Remove(full)
		return
	}
	if size > maxDocumentSize {
		os.Remove(full)
		err = fmt.Errorf("file exceeds 50 MB")
	}
	return
}

func cleanFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" {
		return "document"
	}
	return name
}

// titleFromFilename turns "Ugovor_Athena-2026.pdf" into "Ugovor Athena 2026".
func titleFromFilename(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(base)
	return strings.TrimSpace(strings.Join(strings.Fields(base), " "))
}

func parseClientID(v string) *int64 {
	if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
		return &n
	}
	return nil
}

func validDate(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return ""
	}
	return v
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.DocumentFilter{Query: strings.TrimSpace(q.Get("q")), Category: strings.TrimSpace(q.Get("category"))}
	if id := parseClientID(q.Get("client_id")); id != nil {
		f.ClientID = *id
	}
	list, err := s.store.ListDocuments(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list documents")
		return
	}
	s.decorateDocuments(r.Context(), list)
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDocumentCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.DocumentCategories(r.Context())
	if err != nil {
		s.fail(w, err, "categories")
		return
	}
	// Suggested categories so the picker is never empty on a fresh install.
	for _, c := range []string{"Contracts", "Company registration", "Tax", "Bank", "Insurance", "Certificates", "Correspondence", "Other"} {
		if _, ok := cats[c]; !ok {
			cats[c] = 0
		}
	}
	until := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	expiring, _ := s.store.ExpiringDocuments(r.Context(), until)
	writeJSON(w, http.StatusOK, map[string]any{"categories": cats, "expiring": expiring})
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetDocument(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	d.Paperless = s.paperlessLink(r.Context(), "document", d.ID)
	writeJSON(w, http.StatusOK, d)
}

// handleUploadDocuments accepts one or more files plus shared metadata fields.
func (s *Server) handleUploadDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseMultipartForm(maxDocumentSize); err != nil {
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
	meta := store.Document{
		Title:     strings.TrimSpace(r.FormValue("title")),
		Category:  strings.TrimSpace(r.FormValue("category")),
		ClientID:  parseClientID(r.FormValue("client_id")),
		DocDate:   validDate(r.FormValue("doc_date")),
		ExpiresAt: validDate(r.FormValue("expires_at")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
	}
	var saved []store.Document
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			s.fail(w, err, "open upload")
			return
		}
		name := cleanFilename(fh.Filename)
		stored, ct, size, err := s.storeDocumentFile(name, f)
		f.Close()
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		d := meta
		d.Filename, d.StoredName, d.ContentType, d.Size = name, stored, ct, size
		if d.Title == "" || len(files) > 1 {
			d.Title = titleFromFilename(name)
		}
		if err := s.store.CreateDocument(ctx, &d); err != nil {
			os.Remove(filepath.Join(s.cfg.DataDir, "documents", stored))
			s.fail(w, err, "create document")
			return
		}
		s.store.LogActivity(ctx, "document", d.ID, "created", "Uploaded "+d.Title)
		saved = append(saved, d)
	}
	if r.FormValue("paperless") == "1" {
		for i := range saved {
			s.paperlessSendDocument(ctx, &saved[i], false)
		}
	}
	s.decorateDocuments(ctx, saved)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.store.GetDocument(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	var in struct {
		Title     string `json:"title"`
		Category  string `json:"category"`
		ClientID  *int64 `json:"client_id"`
		DocDate   string `json:"doc_date"`
		ExpiresAt string `json:"expires_at"`
		Notes     string `json:"notes"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d.Title = strings.TrimSpace(in.Title)
	if d.Title == "" {
		d.Title = titleFromFilename(d.Filename)
	}
	d.Category = strings.TrimSpace(in.Category)
	d.ClientID = in.ClientID
	if d.ClientID != nil && *d.ClientID <= 0 {
		d.ClientID = nil
	}
	d.DocDate = validDate(in.DocDate)
	d.ExpiresAt = validDate(in.ExpiresAt)
	d.Notes = strings.TrimSpace(in.Notes)
	if err := s.store.UpdateDocument(ctx, d); err != nil {
		s.fail(w, err, "update document")
		return
	}
	d, _ = s.store.GetDocument(ctx, d.ID)
	d.Paperless = s.paperlessLink(ctx, "document", d.ID)
	writeJSON(w, http.StatusOK, d)
}

// handleReplaceDocumentFile swaps the file but keeps the metadata.
func (s *Server) handleReplaceDocumentFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.store.GetDocument(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	if err := r.ParseMultipartForm(maxDocumentSize); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid upload: "+err.Error())
		return
	}
	fh := r.MultipartForm.File["file"]
	if len(fh) == 0 {
		writeErr(w, http.StatusBadRequest, "no file field 'file'")
		return
	}
	f, err := fh[0].Open()
	if err != nil {
		s.fail(w, err, "open upload")
		return
	}
	defer f.Close()
	name := cleanFilename(fh[0].Filename)
	stored, ct, size, err := s.storeDocumentFile(name, f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	old := s.documentPath(d)
	if err := s.store.ReplaceDocumentFile(ctx, d.ID, name, stored, ct, size); err != nil {
		os.Remove(filepath.Join(s.cfg.DataDir, "documents", stored))
		s.fail(w, err, "replace file")
		return
	}
	_ = os.Remove(old)
	d, _ = s.store.GetDocument(ctx, d.ID)
	d.Paperless = s.paperlessLink(ctx, "document", d.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDocumentFile(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetDocument(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	p := s.documentPath(d)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing on disk")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", d.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeFilename(d.Filename)))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, p)
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetDocument(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get document")
		return
	}
	_ = os.Remove(s.documentPath(d))
	if err := s.store.DeleteDocument(r.Context(), d.ID); err != nil {
		s.fail(w, err, "delete document")
		return
	}
	s.store.LogActivity(r.Context(), "document", d.ID, "deleted", "Removed "+d.Title)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// decorateDocuments attaches Paperless link info to a list.
func (s *Server) decorateDocuments(ctx context.Context, list []store.Document) {
	if len(list) == 0 {
		return
	}
	ids := make([]int64, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	links, err := s.store.PaperlessLinksFor(ctx, "document", ids)
	if err != nil {
		return
	}
	for i := range list {
		if l, ok := links[list[i].ID]; ok {
			list[i].Paperless = s.resolvePaperlessLink(ctx, l)
		}
	}
}
