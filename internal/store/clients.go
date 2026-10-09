package store

import (
	"context"
	"strings"
)

const clientCols = `c.id, c.name, c.contact_name, c.email, c.phone, c.address1, c.address2, c.city, c.state, c.postal_code, c.country, c.tax_id, c.website, c.currency, c.billing_mode, c.default_rate, c.payment_terms_days, c.notes, c.archived, c.created_at, c.updated_at, c.template_id, c.email_subject, c.email_body, c.email_cc, c.paperless_correspondent_id, c.paperless_correspondent`

func scanClient(row interface{ Scan(...any) error }, withStats bool) (*Client, error) {
	var c Client
	dest := []any{&c.ID, &c.Name, &c.ContactName, &c.Email, &c.Phone, &c.Address1, &c.Address2, &c.City, &c.State, &c.PostalCode, &c.Country, &c.TaxID, &c.Website, &c.Currency, &c.BillingMode, &c.DefaultRate, &c.PaymentTermsDays, &c.Notes, &c.Archived, &c.CreatedAt, &c.UpdatedAt, &c.TemplateID, &c.EmailSubject, &c.EmailBody, &c.EmailCC, &c.PaperlessCorrespondentID, &c.PaperlessCorrespondent}
	if withStats {
		dest = append(dest, &c.InvoiceCount, &c.Outstanding, &c.TotalBilled)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListClients returns clients with aggregate stats.
func (s *Store) ListClients(ctx context.Context, includeArchived bool, search string) ([]Client, error) {
	q := `SELECT ` + clientCols + `,
		(SELECT COUNT(1) FROM invoices i WHERE i.client_id = c.id AND i.status != 'cancelled'),
		COALESCE((SELECT SUM(i.total - i.amount_paid) FROM invoices i WHERE i.client_id = c.id AND i.status IN ('sent','viewed','partial','overdue')), 0),
		COALESCE((SELECT SUM(i.total) FROM invoices i WHERE i.client_id = c.id AND i.status NOT IN ('draft','cancelled')), 0)
		FROM clients c WHERE 1=1`
	args := []any{}
	if !includeArchived {
		q += ` AND c.archived = 0`
	}
	if search = strings.TrimSpace(search); search != "" {
		q += ` AND (c.name LIKE ? OR c.email LIKE ? OR c.contact_name LIKE ?)`
		like := "%" + search + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY c.archived, c.name COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		c, err := scanClient(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetClient returns one client.
func (s *Store) GetClient(ctx context.Context, id int64) (*Client, error) {
	return scanClient(s.DB.QueryRowContext(ctx, `SELECT `+clientCols+`,
		(SELECT COUNT(1) FROM invoices i WHERE i.client_id = c.id AND i.status != 'cancelled'),
		COALESCE((SELECT SUM(i.total - i.amount_paid) FROM invoices i WHERE i.client_id = c.id AND i.status IN ('sent','viewed','partial','overdue')), 0),
		COALESCE((SELECT SUM(i.total) FROM invoices i WHERE i.client_id = c.id AND i.status NOT IN ('draft','cancelled')), 0)
		FROM clients c WHERE c.id = ?`, id), true)
}

// CreateClient inserts a client.
func (s *Store) CreateClient(ctx context.Context, c *Client) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO clients (name, contact_name, email, phone, address1, address2, city, state, postal_code, country, tax_id, website, currency, billing_mode, default_rate, payment_terms_days, notes, archived, template_id, email_subject, email_body, email_cc, paperless_correspondent_id, paperless_correspondent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ContactName, c.Email, c.Phone, c.Address1, c.Address2, c.City, c.State, c.PostalCode, c.Country, c.TaxID, c.Website, c.Currency, c.BillingMode, c.DefaultRate, c.PaymentTermsDays, c.Notes, c.Archived, c.TemplateID, c.EmailSubject, c.EmailBody, c.EmailCC, c.PaperlessCorrespondentID, c.PaperlessCorrespondent)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

// UpdateClient updates a client.
func (s *Store) UpdateClient(ctx context.Context, c *Client) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE clients SET name=?, contact_name=?, email=?, phone=?, address1=?, address2=?, city=?, state=?, postal_code=?, country=?, tax_id=?, website=?, currency=?, billing_mode=?, default_rate=?, payment_terms_days=?, notes=?, archived=?, template_id=?, email_subject=?, email_body=?, email_cc=?, paperless_correspondent_id=?, paperless_correspondent=?, updated_at=? WHERE id=?`,
		c.Name, c.ContactName, c.Email, c.Phone, c.Address1, c.Address2, c.City, c.State, c.PostalCode, c.Country, c.TaxID, c.Website, c.Currency, c.BillingMode, c.DefaultRate, c.PaymentTermsDays, c.Notes, c.Archived, c.TemplateID, c.EmailSubject, c.EmailBody, c.EmailCC, c.PaperlessCorrespondentID, c.PaperlessCorrespondent, Now(), c.ID)
	return err
}

// DeleteClient deletes a client if it has no invoices; otherwise archives it.
func (s *Store) DeleteClient(ctx context.Context, id int64) (deleted bool, err error) {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM invoices WHERE client_id = ?`, id).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		_, err := s.DB.ExecContext(ctx, `UPDATE clients SET archived = 1, updated_at = ? WHERE id = ?`, Now(), id)
		return false, err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM time_entries WHERE client_id = ?`, id); err != nil {
		return false, err
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM recurring_invoices WHERE client_id = ?`, id); err != nil {
		return false, err
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, id)
	return true, err
}
