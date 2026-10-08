package store

import "context"

// ListAttachments returns files attached to an invoice.
func (s *Store) ListAttachments(ctx context.Context, invoiceID int64) ([]Attachment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, invoice_id, filename, stored_name, content_type, size, created_at FROM attachments WHERE invoice_id = ? ORDER BY id`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.InvoiceID, &a.Filename, &a.StoredName, &a.ContentType, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAttachment returns one attachment.
func (s *Store) GetAttachment(ctx context.Context, id int64) (*Attachment, error) {
	var a Attachment
	err := s.DB.QueryRowContext(ctx, `SELECT id, invoice_id, filename, stored_name, content_type, size, created_at FROM attachments WHERE id = ?`, id).Scan(&a.ID, &a.InvoiceID, &a.Filename, &a.StoredName, &a.ContentType, &a.Size, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateAttachment records an uploaded file.
func (s *Store) CreateAttachment(ctx context.Context, a *Attachment) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO attachments (invoice_id, filename, stored_name, content_type, size) VALUES (?, ?, ?, ?, ?)`, a.InvoiceID, a.Filename, a.StoredName, a.ContentType, a.Size)
	if err != nil {
		return err
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

// DeleteAttachment removes the record (the caller removes the file).
func (s *Store) DeleteAttachment(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, id)
	return err
}
