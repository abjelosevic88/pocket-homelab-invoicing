package store

import (
	"context"
	"database/sql"
)

// PaymentFilter narrows ListPayments.
type PaymentFilter struct {
	InvoiceID int64
	ClientID  int64
	From, To  string
	Limit     int
}

// ListPayments lists payments.
func (s *Store) ListPayments(ctx context.Context, uid int64, f PaymentFilter) ([]Payment, error) {
	q := `SELECT p.id, p.invoice_id, i.number, c.name, p.date, p.amount, p.currency, p.exchange_rate, p.applied_amount, p.method, p.reference, p.notes, p.created_at
		FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN clients c ON c.id = i.client_id WHERE p.user_id = ? AND i.user_id = ?`
	args := []any{uid, uid}
	if f.InvoiceID > 0 {
		q += ` AND p.invoice_id = ?`
		args = append(args, f.InvoiceID)
	}
	if f.ClientID > 0 {
		q += ` AND i.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.From != "" {
		q += ` AND p.date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND p.date <= ?`
		args = append(args, f.To)
	}
	q += ` ORDER BY p.date DESC, p.id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Payment{}
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.InvoiceID, &p.InvoiceNumber, &p.ClientName, &p.Date, &p.Amount, &p.Currency, &p.ExchangeRate, &p.AppliedAmount, &p.Method, &p.Reference, &p.Notes, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPayment returns one payment.
func (s *Store) GetPayment(ctx context.Context, uid, id int64) (*Payment, error) {
	var p Payment
	err := s.DB.QueryRowContext(ctx, `SELECT p.id, p.invoice_id, i.number, c.name, p.date, p.amount, p.currency, p.exchange_rate, p.applied_amount, p.method, p.reference, p.notes, p.created_at
		FROM payments p JOIN invoices i ON i.id = p.invoice_id JOIN clients c ON c.id = i.client_id WHERE p.id = ? AND p.user_id = ?`, id, uid).Scan(&p.ID, &p.InvoiceID, &p.InvoiceNumber, &p.ClientName, &p.Date, &p.Amount, &p.Currency, &p.ExchangeRate, &p.AppliedAmount, &p.Method, &p.Reference, &p.Notes, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePayment records a payment against one of the user's invoices.
func (s *Store) CreatePayment(ctx context.Context, uid int64, p *Payment) error {
	if !s.ownsInvoice(ctx, uid, p.InvoiceID) {
		return ErrNotFound
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO payments (user_id, invoice_id, date, amount, currency, exchange_rate, applied_amount, method, reference, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NullInt(uid), p.InvoiceID, p.Date, p.Amount, p.Currency, p.ExchangeRate, p.AppliedAmount, p.Method, p.Reference, p.Notes)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// DeletePayment removes a payment and returns the invoice id.
func (s *Store) DeletePayment(ctx context.Context, uid, id int64) (int64, error) {
	var invoiceID int64
	if err := s.DB.QueryRowContext(ctx, `SELECT invoice_id FROM payments WHERE id = ? AND user_id = ?`, id, uid).Scan(&invoiceID); err != nil {
		return 0, err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM payments WHERE id = ? AND user_id = ?`, id, uid)
	return invoiceID, err
}

func insertPayment(ctx context.Context, tx *sql.Tx, uid int64, p *Payment) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO payments (user_id, invoice_id, date, amount, currency, exchange_rate, applied_amount, method, reference, notes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NullInt(uid), p.InvoiceID, p.Date, p.Amount, p.Currency, p.ExchangeRate, p.AppliedAmount, p.Method, p.Reference, p.Notes)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}
