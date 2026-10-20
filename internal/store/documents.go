package store

import (
	"context"
	"database/sql"
	"fmt"
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
func (s *Store) ListDocuments(ctx context.Context, uid int64, f DocumentFilter) ([]Document, error) {
	q := `SELECT ` + documentCols + ` FROM documents d LEFT JOIN clients c ON c.id = d.client_id WHERE d.user_id = ?`
	args := []any{uid}
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
func (s *Store) GetDocument(ctx context.Context, uid, id int64) (*Document, error) {
	return scanDocument(s.DB.QueryRowContext(ctx, `SELECT `+documentCols+` FROM documents d LEFT JOIN clients c ON c.id = d.client_id WHERE d.id = ? AND d.user_id = ?`, id, uid))
}

// CreateDocument inserts a document record (the caller stores the file).
func (s *Store) CreateDocument(ctx context.Context, uid int64, d *Document) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO documents (user_id, title, category, client_id, doc_date, expires_at, notes, filename, stored_name, content_type, size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NullInt(uid), d.Title, d.Category, d.ClientID, d.DocDate, d.ExpiresAt, d.Notes, d.Filename, d.StoredName, d.ContentType, d.Size)
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

// UpdateDocument saves editable metadata.
func (s *Store) UpdateDocument(ctx context.Context, uid int64, d *Document) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE documents SET title = ?, category = ?, client_id = ?, doc_date = ?, expires_at = ?, notes = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		d.Title, d.Category, d.ClientID, d.DocDate, d.ExpiresAt, d.Notes, Now(), d.ID, uid)
	return notFoundIfNone(res, err)
}

// ReplaceDocumentFile swaps the stored file of a document.
func (s *Store) ReplaceDocumentFile(ctx context.Context, uid, id int64, filename, storedName, contentType string, size int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE documents SET filename = ?, stored_name = ?, content_type = ?, size = ?, updated_at = ? WHERE id = ? AND user_id = ?`, filename, storedName, contentType, size, Now(), id, uid)
	return notFoundIfNone(res, err)
}

// DeleteDocument removes the record (the caller removes the file).
func (s *Store) DeleteDocument(ctx context.Context, uid, id int64) error {
	if _, err := s.GetDocument(ctx, uid, id); err != nil {
		return err
	}
	for _, t := range []string{"documents_links", "paperless_links"} {
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM `+t+` WHERE kind = 'document' AND ref_id = ? AND user_id = ?`, id, uid); err != nil {
			return err
		}
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM documents WHERE id = ? AND user_id = ?`, id, uid)
	return err
}

// DocumentCategories returns the distinct categories in use with counts.
func (s *Store) DocumentCategories(ctx context.Context, uid int64) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT category, COUNT(*) FROM documents WHERE user_id = ? AND category <> '' GROUP BY category ORDER BY category`, uid)
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
func (s *Store) ExpiringDocuments(ctx context.Context, uid int64, until string) ([]Document, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+documentCols+` FROM documents d LEFT JOIN clients c ON c.id = d.client_id WHERE d.user_id = ? AND d.expires_at <> '' AND d.expires_at <= ? ORDER BY d.expires_at`, uid, until)
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

// ---------- Pocket Documents links ----------

const docsLinkCols = `kind, ref_id, documents_id, title, error, created_at, updated_at, COALESCE(user_id, 0)`

func scanDocsLink(sc interface{ Scan(...any) error }) (*DocsLink, error) {
	var l DocsLink
	if err := sc.Scan(&l.Kind, &l.RefID, &l.DocumentsID, &l.Title, &l.Error, &l.CreatedAt, &l.UpdatedAt, &l.UserID); err != nil {
		return nil, err
	}
	return &l, nil
}

// GetDocsLink returns the link for a record, or nil when none exists.
func (s *Store) GetDocsLink(ctx context.Context, uid int64, kind string, refID int64) (*DocsLink, error) {
	l, err := scanDocsLink(s.DB.QueryRowContext(ctx, `SELECT `+docsLinkCols+` FROM documents_links WHERE kind = ? AND ref_id = ? AND user_id = ?`, kind, refID, uid))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

// DocsLinksFor returns links of one kind for a set of ids.
func (s *Store) DocsLinksFor(ctx context.Context, uid int64, kind string, ids []int64) (map[int64]*DocsLink, error) {
	out := map[int64]*DocsLink{}
	if len(ids) == 0 {
		return out, nil
	}
	q := `SELECT ` + docsLinkCols + ` FROM documents_links WHERE user_id = ? AND kind = ? AND ref_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	args := make([]any, 0, len(ids)+2)
	args = append(args, uid, kind)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanDocsLink(rows)
		if err != nil {
			return nil, err
		}
		out[l.RefID] = l
	}
	return out, rows.Err()
}

var linkTables = map[string]string{"document": "documents", "invoice": "invoices", "attachment": "attachments"}

// ownsLinked checks that the record a link points at (document, invoice or
// attachment) belongs to the user.
func (s *Store) ownsLinked(ctx context.Context, uid int64, kind string, refID int64) error {
	table := linkTables[kind]
	if table == "" {
		return fmt.Errorf("unknown link kind %q", kind)
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table+` WHERE id = ? AND user_id = ?`, refID, uid).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveDocsLink inserts or updates one of the user's links. The record it points at
// (document, invoice or attachment) must belong to the user.
func (s *Store) SaveDocsLink(ctx context.Context, uid int64, l *DocsLink) error {
	if err := s.ownsLinked(ctx, uid, l.Kind, l.RefID); err != nil {
		return err
	}
	l.UserID = uid
	_, err := s.DB.ExecContext(ctx, `INSERT INTO documents_links (user_id, kind, ref_id, documents_id, title, error, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, ref_id) DO UPDATE SET documents_id = excluded.documents_id, title = excluded.title, error = excluded.error, updated_at = excluded.updated_at
		WHERE documents_links.user_id = excluded.user_id`,
		uid, l.Kind, l.RefID, l.DocumentsID, l.Title, l.Error, Now())
	return err
}

// DeleteDocsLink removes a link (does not touch Pocket Documents itself),
// and the record's old Paperless link with it so it is not adopted again.
func (s *Store) DeleteDocsLink(ctx context.Context, uid int64, kind string, refID int64) error {
	for _, t := range []string{"documents_links", "paperless_links"} {
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM `+t+` WHERE kind = ? AND ref_id = ? AND user_id = ?`, kind, refID, uid); err != nil {
			return err
		}
	}
	return nil
}

// DocsLinkByDocumentsID finds a local record already linked to a Pocket Documents document.
func (s *Store) DocsLinkByDocumentsID(ctx context.Context, uid, documentsID int64) (*DocsLink, error) {
	l, err := scanDocsLink(s.DB.QueryRowContext(ctx, `SELECT `+docsLinkCols+` FROM documents_links WHERE user_id = ? AND documents_id = ? AND documents_id > 0 ORDER BY created_at LIMIT 1`, uid, documentsID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

// PaperlessLinksToAdopt returns the user's old Paperless links (a document id
// there) whose record has no Pocket Documents link yet.
func (s *Store) PaperlessLinksToAdopt(ctx context.Context, uid int64) ([]PaperlessLink, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.kind, p.ref_id, p.paperless_id, p.task_id, p.title, p.error, p.created_at, p.updated_at, COALESCE(p.user_id, 0)
		FROM paperless_links p
		WHERE p.user_id = ? AND p.paperless_id > 0
		  AND NOT EXISTS (SELECT 1 FROM documents_links d WHERE d.kind = p.kind AND d.ref_id = p.ref_id)
		ORDER BY p.created_at`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PaperlessLink{}
	for rows.Next() {
		var l PaperlessLink
		if err := rows.Scan(&l.Kind, &l.RefID, &l.PaperlessID, &l.TaskID, &l.Title, &l.Error, &l.CreatedAt, &l.UpdatedAt, &l.UserID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
