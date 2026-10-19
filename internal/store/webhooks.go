package store

import "context"

// ListWebhooks lists webhook subscriptions.
func (s *Store) ListWebhooks(ctx context.Context, uid int64, enabledOnly bool) ([]Webhook, error) {
	q := `SELECT id, url, events, secret, enabled, created_at FROM webhooks WHERE user_id = ?`
	if enabledOnly {
		q += ` AND enabled = 1`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`, uid)
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
func (s *Store) SaveWebhook(ctx context.Context, uid int64, w *Webhook) error {
	if w.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO webhooks (user_id, url, events, secret, enabled) VALUES (?, ?, ?, ?, ?)`, NullInt(uid), w.URL, w.Events, w.Secret, w.Enabled)
		if err != nil {
			return err
		}
		w.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE webhooks SET url=?, events=?, secret=?, enabled=? WHERE id=? AND user_id=?`, w.URL, w.Events, w.Secret, w.Enabled, w.ID, uid)
	return notFoundIfNone(res, err)
}

// DeleteWebhook removes a webhook.
func (s *Store) DeleteWebhook(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ? AND user_id = ?`, id, uid)
	return notFoundIfNone(res, err)
}

// LogActivity appends an audit entry.
func (s *Store) LogActivity(ctx context.Context, uid int64, entityType string, entityID int64, action, message string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO activity_log (user_id, entity_type, entity_id, action, message) VALUES (?, ?, ?, ?, ?)`, NullInt(uid), entityType, entityID, action, message)
}

// ListActivity returns recent activity, optionally for an entity.
func (s *Store) ListActivity(ctx context.Context, uid int64, entityType string, entityID int64, limit int) ([]Activity, error) {
	q := `SELECT id, entity_type, entity_id, action, message, created_at FROM activity_log WHERE user_id = ?`
	args := []any{uid}
	if entityType != "" {
		q += ` AND entity_type = ? AND entity_id = ?`
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
func (s *Store) LogEmail(ctx context.Context, uid, invoiceID int64, to, subject, status, errMsg string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO email_log (user_id, invoice_id, to_address, subject, status, error) VALUES (?, ?, ?, ?, ?, ?)`, NullInt(uid), NullInt(invoiceID), to, subject, status, errMsg)
}

// ListEmailLog lists email attempts for an invoice.
func (s *Store) ListEmailLog(ctx context.Context, uid, invoiceID int64, limit int) ([]EmailLog, error) {
	q := `SELECT id, invoice_id, to_address, subject, status, error, created_at FROM email_log WHERE user_id = ?`
	args := []any{uid}
	if invoiceID > 0 {
		q += ` AND invoice_id = ?`
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
