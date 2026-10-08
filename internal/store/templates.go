package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const templateCols = `id, name, layout, accent_color, labels, html, is_default, created_at, updated_at`

func scanTemplate(row interface{ Scan(...any) error }) (*InvoiceTemplate, error) {
	var t InvoiceTemplate
	var labels string
	if err := row.Scan(&t.ID, &t.Name, &t.Layout, &t.AccentColor, &labels, &t.HTML, &t.IsDefault, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Labels = map[string]string{}
	_ = json.Unmarshal([]byte(labels), &t.Labels)
	return &t, nil
}

// ListTemplates lists templates.
func (s *Store) ListTemplates(ctx context.Context) ([]InvoiceTemplate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+templateCols+` FROM invoice_templates ORDER BY is_default DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvoiceTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTemplate returns a template by id.
func (s *Store) GetTemplate(ctx context.Context, id int64) (*InvoiceTemplate, error) {
	return scanTemplate(s.DB.QueryRowContext(ctx, `SELECT `+templateCols+` FROM invoice_templates WHERE id = ?`, id))
}

// GetDefaultTemplate returns the default template (or the first one).
func (s *Store) GetDefaultTemplate(ctx context.Context) (*InvoiceTemplate, error) {
	t, err := scanTemplate(s.DB.QueryRowContext(ctx, `SELECT `+templateCols+` FROM invoice_templates ORDER BY is_default DESC, id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return &InvoiceTemplate{Name: "Classic", Layout: "classic", AccentColor: "#2563eb", Labels: map[string]string{}}, nil
	}
	return t, err
}

// SaveTemplate creates or updates a template.
func (s *Store) SaveTemplate(ctx context.Context, t *InvoiceTemplate) error {
	labels, _ := json.Marshal(t.Labels)
	if t.IsDefault {
		if _, err := s.DB.ExecContext(ctx, `UPDATE invoice_templates SET is_default = 0`); err != nil {
			return err
		}
	}
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO invoice_templates (name, layout, accent_color, labels, html, is_default) VALUES (?, ?, ?, ?, ?, ?)`, t.Name, t.Layout, t.AccentColor, string(labels), t.HTML, t.IsDefault)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE invoice_templates SET name=?, layout=?, accent_color=?, labels=?, html=?, is_default=?, updated_at=? WHERE id=?`, t.Name, t.Layout, t.AccentColor, string(labels), t.HTML, t.IsDefault, Now(), t.ID)
	return err
}

// DeleteTemplate removes a template.
func (s *Store) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM invoice_templates WHERE id = ? AND is_default = 0`, id)
	return err
}

// CountTemplates returns the number of templates.
func (s *Store) CountTemplates(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM invoice_templates`).Scan(&n)
	return n, err
}
