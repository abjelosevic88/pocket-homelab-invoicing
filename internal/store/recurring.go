package store

import (
	"context"
	"encoding/json"
)

const recurringCols = `r.id, r.name, r.client_id, c.name, r.status, r.frequency, r.interval, r.start_date, r.end_date, r.next_run, r.last_run, r.occurrences, r.max_occurrences, r.due_days, r.currency, r.billing_mode, r.items, r.discount_type, r.discount_value, r.notes, r.terms, r.auto_send, r.template_id, r.quantity_mode, r.period_mode, r.created_at, r.updated_at`

func scanRecurring(row interface{ Scan(...any) error }) (*RecurringInvoice, error) {
	var r RecurringInvoice
	var items string
	if err := row.Scan(&r.ID, &r.Name, &r.ClientID, &r.ClientName, &r.Status, &r.Frequency, &r.Interval, &r.StartDate, &r.EndDate, &r.NextRun, &r.LastRun, &r.Occurrences, &r.MaxOccurrences, &r.DueDays, &r.Currency, &r.BillingMode, &items, &r.DiscountType, &r.DiscountValue, &r.Notes, &r.Terms, &r.AutoSend, &r.TemplateID, &r.QuantityMode, &r.PeriodMode, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Items = []RecurringItem{}
	_ = json.Unmarshal([]byte(items), &r.Items)
	return &r, nil
}

// ListRecurring lists recurring profiles.
func (s *Store) ListRecurring(ctx context.Context) ([]RecurringInvoice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+recurringCols+` FROM recurring_invoices r JOIN clients c ON c.id = r.client_id ORDER BY r.status, r.next_run`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecurringInvoice{}
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// DueRecurring returns active profiles whose next_run <= today.
func (s *Store) DueRecurring(ctx context.Context) ([]RecurringInvoice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+recurringCols+` FROM recurring_invoices r JOIN clients c ON c.id = r.client_id WHERE r.status = 'active' AND r.next_run <= ? ORDER BY r.next_run`, Today())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecurringInvoice
	for rows.Next() {
		r, err := scanRecurring(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRecurring returns one profile.
func (s *Store) GetRecurring(ctx context.Context, id int64) (*RecurringInvoice, error) {
	return scanRecurring(s.DB.QueryRowContext(ctx, `SELECT `+recurringCols+` FROM recurring_invoices r JOIN clients c ON c.id = r.client_id WHERE r.id = ?`, id))
}

// SaveRecurring creates or updates a profile.
func (s *Store) SaveRecurring(ctx context.Context, r *RecurringInvoice) error {
	items, _ := json.Marshal(r.Items)
	if r.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO recurring_invoices (name, client_id, status, frequency, interval, start_date, end_date, next_run, last_run, occurrences, max_occurrences, due_days, currency, billing_mode, items, discount_type, discount_value, notes, terms, auto_send, template_id, quantity_mode, period_mode)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.Name, r.ClientID, r.Status, r.Frequency, r.Interval, r.StartDate, r.EndDate, r.NextRun, r.LastRun, r.Occurrences, r.MaxOccurrences, r.DueDays, r.Currency, r.BillingMode, string(items), r.DiscountType, r.DiscountValue, r.Notes, r.Terms, r.AutoSend, r.TemplateID, r.QuantityMode, r.PeriodMode)
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE recurring_invoices SET name=?, client_id=?, status=?, frequency=?, interval=?, start_date=?, end_date=?, next_run=?, last_run=?, occurrences=?, max_occurrences=?, due_days=?, currency=?, billing_mode=?, items=?, discount_type=?, discount_value=?, notes=?, terms=?, auto_send=?, template_id=?, quantity_mode=?, period_mode=?, updated_at=? WHERE id=?`,
		r.Name, r.ClientID, r.Status, r.Frequency, r.Interval, r.StartDate, r.EndDate, r.NextRun, r.LastRun, r.Occurrences, r.MaxOccurrences, r.DueDays, r.Currency, r.BillingMode, string(items), r.DiscountType, r.DiscountValue, r.Notes, r.Terms, r.AutoSend, r.TemplateID, r.QuantityMode, r.PeriodMode, Now(), r.ID)
	return err
}

// DeleteRecurring removes a profile (generated invoices are kept).
func (s *Store) DeleteRecurring(ctx context.Context, id int64) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE invoices SET recurring_id = NULL WHERE recurring_id = ?`, id); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM recurring_invoices WHERE id = ?`, id)
	return err
}
