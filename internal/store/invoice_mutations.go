package store

import (
	"context"
	"database/sql"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// CommitInvoice applies a payment/status change and its resulting event in one transaction.
func (s *Store) CommitInvoice(ctx context.Context, uid, id int64, p *Payment, status string, after ...core.CommitHook) (*Invoice, error) {
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		inv, err := InvoiceSnapshot(ctx, tx, uid, id)
		if err != nil {
			return err
		}
		if p != nil {
			if p.InvoiceID != id {
				return ErrNotFound
			}
			if err := insertPayment(ctx, tx, uid, p); err != nil {
				return err
			}
		}
		if status != "" {
			now := Now()
			var sent, paid *string
			if status == StatusSent && inv.SentAt == nil {
				sent = &now
			}
			if status == StatusPaid {
				paid = &now
			}
			if _, err := tx.ExecContext(ctx, `UPDATE invoices SET status=?,sent_at=COALESCE(?,sent_at),paid_at=COALESCE(?,paid_at),updated_at=? WHERE id=? AND user_id=?`, status, sent, paid, now, id, uid); err != nil {
				return err
			}
		}
		if _, err := recalcAmountPaid(ctx, tx, uid, id); err != nil {
			return err
		}
		for _, hook := range after {
			if hook != nil {
				if err := hook(tx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetInvoice(ctx, uid, id)
}

// RecordSentInvoice couples the successful email receipt, sent status and event.
func (s *Store) RecordSentInvoice(ctx context.Context, uid, id int64, to, subject string, after ...core.CommitHook) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := InvoiceSnapshot(ctx, tx, uid, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO email_log(user_id,invoice_id,to_address,subject,status,error) VALUES(?,?,?,?,'sent','')`, uid, id, to, subject); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE invoices SET status=CASE WHEN status='draft' THEN 'sent' ELSE status END,sent_at=COALESCE(sent_at,?),updated_at=? WHERE id=? AND user_id=?`, Now(), Now(), id, uid); err != nil {
			return err
		}
		for _, hook := range after {
			if hook != nil {
				if err := hook(tx); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
