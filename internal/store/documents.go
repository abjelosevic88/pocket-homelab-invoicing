package store

import (
	"context"
	"database/sql"
	"strings"
)

const documentCols = `d.id, d.title, d.category, d.client_id, COALESCE(c.name, ''), d.doc_date, d.expires_at, d.notes, d.filename, d.stored_name, d.content_type, d.size, d.created_at, d.updated_at`

func scanDocument(sc interface{ Scan(...any) error }) (*Document, error) {
	var d Document
	var cid sql.NullInt64
	if err := sc.Scan(&d.ID, &d.Title, &d.Category, &cid, &d.ClientName, &d.DocDate, &d.ExpiresAt, &d.Notes, &d.Filename, &d.StoredName, &d.ContentType, &d.Size, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	if cid.Valid {
		d.ClientID = &cid.Int64
	}
	return &d, nil
}

// DocumentFilter narrows ListDocuments.
type DocumentFilter struct {
	Query    string
	Category string
	ClientID int64
}

// ListDocuments returns company documents, newest first.
func (s *Store) ListDocuments(ctx context.Context, f DocumentFilter) ([]Document, error) {
	q := `SELECT ` + documentCols + ` FROM documents d LEFT JOIN clients c ON c.id = d.client_id WHERE 1=1`
	var args []any
	if f.Query != "" {
		like := "%" + strings.ToLower(f.Query) + "%"
		q += ` AND (lower(d.title) LIKE ? OR lower(d.filename) LIKE ? OR lower(d.notes) LIKE ? OR lower(d.category) LIKE ? OR lower(COALESCE(c.name,'')) LIKE ?)`
		args = append(args, like, like, like, like, like)
	}
	if f.Category != "" {
		q += ` AND d.category = ?`
		args = append(args, f.Category)
	}
	if f.ClientID > 0 {
		q += ` AND d.client_id = ?`
		args = append(args, f.ClientID)
	}
	q += ` ORDER BY COALESCE(NULLIF(d.doc_date,''), substr(d.created_at,1,10)) DESC, d.id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDocument returns one document.
func (s *Store) GetDocument(ctx context.Context, id int64) (*Document, error) {
	return scanDocument(s.DB.QueryRowContext(ctx, `SELECT `+documentCols+` FROM documents d LEFT JOIN clients c ON c.id = d.client_id WHERE d.id = ?`, id))
}

// CreateDocument inserts a document record (the caller stores the file).
func (s *Store) CreateDocument(ctx context.Context, d *Document) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO documents (title, category, client_id, doc_date, expires_at, notes, filename, stored_name, content_type, size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Title, d.Category, d.ClientID, d.DocDate, d.ExpiresAt, d.Notes, d.Filename, d.StoredName, d.ContentType, d.Size)
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

// UpdateDocument saves editable metadata.
func (s *Store) UpdateDocument(ctx context.Context, d *Document) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE documents SET title = ?, category = ?, client_id = ?, doc_date = ?, expires_at = ?, notes = ?, updated_at = ? WHERE id = ?`,
		d.Title, d.Category, d.ClientID, d.DocDate, d.ExpiresAt, d.Notes, Now(), d.ID)
	return err
}

// ReplaceDocumentFile swaps the stored file of a document.
func (s *Store) ReplaceDocumentFile(ctx context.Context, id int64, filename, storedName, contentType string, size int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE documents SET filename = ?, stored_name = ?, content_type = ?, size = ?, updated_at = ? WHERE id = ?`, filename, storedName, contentType, size, Now(), id)
	return err
}

// DeleteDocument removes the record (the caller removes the file).
func (s *Store) DeleteDocument(ctx context.Context, id int64) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM paperless_links WHERE kind = 'document' AND ref_id = ?`, id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	return err
}

// DocumentCategories returns the distinct categories in use with counts.
func (s *Store) DocumentCategories(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT category, COUNT(*) FROM documents WHERE category <> '' GROUP BY category ORDER BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

// ExpiringDocuments returns documents whose expiry date falls on or before the given date (YYYY-MM-DD).
func (s *Store) ExpiringDocuments(ctx context.Context, until string) ([]Document, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+documentCols+` FROM documents d LEFT JOIN clients c ON c.id = d.client_id WHERE d.expires_at <> '' AND d.expires_at <= ? ORDER BY d.expires_at`, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ---------- Paperless links ----------

const paperlessCols = `kind, ref_id, paperless_id, task_id, title, error, created_at, updated_at`

func scanPaperless(sc interface{ Scan(...any) error }) (*PaperlessLink, error) {
	var l PaperlessLink
	if err := sc.Scan(&l.Kind, &l.RefID, &l.PaperlessID, &l.TaskID, &l.Title, &l.Error, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// GetPaperlessLink returns the link for a record, or nil when none exists.
func (s *Store) GetPaperlessLink(ctx context.Context, kind string, refID int64) (*PaperlessLink, error) {
	l, err := scanPaperless(s.DB.QueryRowContext(ctx, `SELECT `+paperlessCols+` FROM paperless_links WHERE kind = ? AND ref_id = ?`, kind, refID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

// PaperlessLinksFor returns links of one kind for a set of ids.
func (s *Store) PaperlessLinksFor(ctx context.Context, kind string, ids []int64) (map[int64]*PaperlessLink, error) {
	out := map[int64]*PaperlessLink{}
	if len(ids) == 0 {
		return out, nil
	}
	q := `SELECT ` + paperlessCols + ` FROM paperless_links WHERE kind = ? AND ref_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	args := make([]any, 0, len(ids)+1)
	args = append(args, kind)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanPaperless(rows)
		if err != nil {
			return nil, err
		}
		out[l.RefID] = l
	}
	return out, rows.Err()
}

// SavePaperlessLink inserts or updates a link.
func (s *Store) SavePaperlessLink(ctx context.Context, l *PaperlessLink) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO paperless_links (kind, ref_id, paperless_id, task_id, title, error, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, ref_id) DO UPDATE SET paperless_id = excluded.paperless_id, task_id = excluded.task_id, title = excluded.title, error = excluded.error, updated_at = excluded.updated_at`,
		l.Kind, l.RefID, l.PaperlessID, l.TaskID, l.Title, l.Error, Now())
	return err
}

// DeletePaperlessLink removes a link (does not touch Paperless itself).
func (s *Store) DeletePaperlessLink(ctx context.Context, kind string, refID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM paperless_links WHERE kind = ? AND ref_id = ?`, kind, refID)
	return err
}

// PendingPaperlessLinks returns links whose consume task has not resolved yet.
func (s *Store) PendingPaperlessLinks(ctx context.Context) ([]PaperlessLink, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+paperlessCols+` FROM paperless_links WHERE paperless_id = 0 AND task_id <> '' AND error = '' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PaperlessLink{}
	for rows.Next() {
		l, err := scanPaperless(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// PaperlessLinkByPaperlessID finds a local record already linked to a Paperless document.
func (s *Store) PaperlessLinkByPaperlessID(ctx context.Context, paperlessID int64) (*PaperlessLink, error) {
	l, err := scanPaperless(s.DB.QueryRowContext(ctx, `SELECT `+paperlessCols+` FROM paperless_links WHERE paperless_id = ? AND paperless_id > 0 ORDER BY created_at LIMIT 1`, paperlessID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}
