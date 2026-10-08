package store

import "context"

// ListWebhooks lists webhook subscriptions.
func (s *Store) ListWebhooks(ctx context.Context, enabledOnly bool) ([]Webhook, error) {
	q := `SELECT id, url, events, secret, enabled, created_at FROM webhooks`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.URL, &w.Events, &w.Secret, &w.Enabled, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// SaveWebhook creates or updates a webhook.
func (s *Store) SaveWebhook(ctx context.Context, w *Webhook) error {
	if w.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO webhooks (url, events, secret, enabled) VALUES (?, ?, ?, ?)`, w.URL, w.Events, w.Secret, w.Enabled)
		if err != nil {
			return err
		}
		w.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE webhooks SET url=?, events=?, secret=?, enabled=? WHERE id=?`, w.URL, w.Events, w.Secret, w.Enabled, w.ID)
	return err
}

// DeleteWebhook removes a webhook.
func (s *Store) DeleteWebhook(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// LogActivity appends an audit entry.
func (s *Store) LogActivity(ctx context.Context, entityType string, entityID int64, action, message string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO activity_log (entity_type, entity_id, action, message) VALUES (?, ?, ?, ?)`, entityType, entityID, action, message)
}

// ListActivity returns recent activity, optionally for an entity.
func (s *Store) ListActivity(ctx context.Context, entityType string, entityID int64, limit int) ([]Activity, error) {
	q := `SELECT id, entity_type, entity_id, action, message, created_at FROM activity_log`
	args := []any{}
	if entityType != "" {
		q += ` WHERE entity_type = ? AND entity_id = ?`
		args = append(args, entityType, entityID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Action, &a.Message, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LogEmail records an email attempt.
func (s *Store) LogEmail(ctx context.Context, invoiceID int64, to, subject, status, errMsg string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO email_log (invoice_id, to_address, subject, status, error) VALUES (?, ?, ?, ?, ?)`, NullInt(invoiceID), to, subject, status, errMsg)
}

// ListEmailLog lists email attempts for an invoice.
func (s *Store) ListEmailLog(ctx context.Context, invoiceID int64, limit int) ([]EmailLog, error) {
	q := `SELECT id, invoice_id, to_address, subject, status, error, created_at FROM email_log`
	args := []any{}
	if invoiceID > 0 {
		q += ` WHERE invoice_id = ?`
		args = append(args, invoiceID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EmailLog{}
	for rows.Next() {
		var e EmailLog
		if err := rows.Scan(&e.ID, &e.InvoiceID, &e.To, &e.Subject, &e.Status, &e.Error, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
