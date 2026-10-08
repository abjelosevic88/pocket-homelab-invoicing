package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const invoiceCols = `i.id, i.number, i.client_id, c.name, i.status, i.issue_date, i.due_date, i.currency, i.exchange_rate, i.billing_mode, i.period_start, i.period_end, i.po_number, i.discount_type, i.discount_value, i.subtotal, i.discount_total, i.tax_total, i.total, i.amount_paid, i.notes, i.terms, i.footer, i.template_id, i.recurring_id, i.public_token, i.sent_at, i.viewed_at, i.paid_at, i.created_at, i.updated_at, i.custom_fields`

func scanInvoice(row interface{ Scan(...any) error }) (*Invoice, error) {
	var inv Invoice
	var cf string
	if err := row.Scan(&inv.ID, &inv.Number, &inv.ClientID, &inv.ClientName, &inv.Status, &inv.IssueDate, &inv.DueDate, &inv.Currency, &inv.ExchangeRate, &inv.BillingMode, &inv.PeriodStart, &inv.PeriodEnd, &inv.PONumber, &inv.DiscountType, &inv.DiscountValue, &inv.Subtotal, &inv.DiscountTotal, &inv.TaxTotal, &inv.Total, &inv.AmountPaid, &inv.Notes, &inv.Terms, &inv.Footer, &inv.TemplateID, &inv.RecurringID, &inv.PublicToken, &inv.SentAt, &inv.ViewedAt, &inv.PaidAt, &inv.CreatedAt, &inv.UpdatedAt, &cf); err != nil {
		return nil, err
	}
	inv.CustomFields = map[string]string{}
	_ = json.Unmarshal([]byte(cf), &inv.CustomFields)
	inv.Balance = inv.Total - inv.AmountPaid
	if inv.Balance < 0.000001 && inv.Balance > -0.000001 {
		inv.Balance = 0
	}
	return &inv, nil
}

// InvoiceFilter narrows ListInvoices.
type InvoiceFilter struct {
	Status   string
	ClientID int64
	Search   string
	From     string // issue_date >=
	To       string // issue_date <=
	Limit    int
	Offset   int
}

// ListInvoices returns invoices matching the filter (no items).
func (s *Store) ListInvoices(ctx context.Context, f InvoiceFilter) ([]Invoice, int, error) {
	where := ` FROM invoices i JOIN clients c ON c.id = i.client_id WHERE 1=1`
	args := []any{}
	if f.Status != "" && f.Status != "all" {
		switch f.Status {
		case "open", "outstanding":
			where += ` AND i.status IN ('sent','viewed','partial','overdue')`
		case "overdue":
			where += ` AND i.status IN ('sent','viewed','partial','overdue') AND i.due_date < ?`
			args = append(args, Today())
		default:
			where += ` AND i.status = ?`
			args = append(args, f.Status)
		}
	}
	if f.ClientID > 0 {
		where += ` AND i.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.Search = strings.TrimSpace(f.Search); f.Search != "" {
		where += ` AND (i.number LIKE ? OR c.name LIKE ? OR i.po_number LIKE ?)`
		like := "%" + f.Search + "%"
		args = append(args, like, like, like)
	}
	if f.From != "" {
		where += ` AND i.issue_date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		where += ` AND i.issue_date <= ?`
		args = append(args, f.To)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + invoiceCols + where + ` ORDER BY i.issue_date DESC, i.id DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d OFFSET %d`, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *inv)
	}
	return out, total, rows.Err()
}

// GetInvoice loads an invoice with items and payments.
func (s *Store) GetInvoice(ctx context.Context, id int64) (*Invoice, error) {
	inv, err := scanInvoice(s.DB.QueryRowContext(ctx, `SELECT `+invoiceCols+` FROM invoices i JOIN clients c ON c.id = i.client_id WHERE i.id = ?`, id))
	if err != nil {
		return nil, err
	}
	return s.hydrateInvoice(ctx, inv)
}

// GetInvoiceByToken loads an invoice via its public token.
func (s *Store) GetInvoiceByToken(ctx context.Context, token string) (*Invoice, error) {
	inv, err := scanInvoice(s.DB.QueryRowContext(ctx, `SELECT `+invoiceCols+` FROM invoices i JOIN clients c ON c.id = i.client_id WHERE i.public_token = ?`, token))
	if err != nil {
		return nil, err
	}
	return s.hydrateInvoice(ctx, inv)
}

func (s *Store) hydrateInvoice(ctx context.Context, inv *Invoice) (*Invoice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, invoice_id, position, description, unit, quantity, unit_price, tax_rate, discount, line_total FROM invoice_items WHERE invoice_id = ? ORDER BY position, id`, inv.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inv.Items = []InvoiceItem{}
	for rows.Next() {
		var it InvoiceItem
		if err := rows.Scan(&it.ID, &it.InvoiceID, &it.Position, &it.Description, &it.Unit, &it.Quantity, &it.UnitPrice, &it.TaxRate, &it.Discount, &it.LineTotal); err != nil {
			return nil, err
		}
		inv.Items = append(inv.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	inv.Payments, err = s.ListPayments(ctx, PaymentFilter{InvoiceID: inv.ID})
	if err != nil {
		return nil, err
	}
	inv.Attachments, err = s.ListAttachments(ctx, inv.ID)
	return inv, err
}

func marshalCF(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// CreateInvoice inserts an invoice and its items.
func (s *Store) CreateInvoice(ctx context.Context, inv *Invoice) error {
	if inv.PublicToken == "" {
		inv.PublicToken = RandomToken(16)
	}
	return s.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO invoices (number, client_id, status, issue_date, due_date, currency, exchange_rate, billing_mode, period_start, period_end, po_number, discount_type, discount_value, subtotal, discount_total, tax_total, total, amount_paid, notes, terms, footer, template_id, recurring_id, public_token, sent_at, paid_at, custom_fields)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			inv.Number, inv.ClientID, inv.Status, inv.IssueDate, inv.DueDate, inv.Currency, inv.ExchangeRate, inv.BillingMode, inv.PeriodStart, inv.PeriodEnd, inv.PONumber, inv.DiscountType, inv.DiscountValue, inv.Subtotal, inv.DiscountTotal, inv.TaxTotal, inv.Total, inv.AmountPaid, inv.Notes, inv.Terms, inv.Footer, inv.TemplateID, inv.RecurringID, inv.PublicToken, inv.SentAt, inv.PaidAt, marshalCF(inv.CustomFields))
		if err != nil {
			return err
		}
		inv.ID, _ = res.LastInsertId()
		return insertItems(ctx, tx, inv)
	})
}

func insertItems(ctx context.Context, tx *sql.Tx, inv *Invoice) error {
	for i := range inv.Items {
		it := &inv.Items[i]
		it.InvoiceID = inv.ID
		it.Position = i
		res, err := tx.ExecContext(ctx, `INSERT INTO invoice_items (invoice_id, position, description, unit, quantity, unit_price, tax_rate, discount, line_total) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			inv.ID, i, it.Description, it.Unit, it.Quantity, it.UnitPrice, it.TaxRate, it.Discount, it.LineTotal)
		if err != nil {
			return err
		}
		it.ID, _ = res.LastInsertId()
	}
	return nil
}

// UpdateInvoice replaces an invoice and its items.
func (s *Store) UpdateInvoice(ctx context.Context, inv *Invoice) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE invoices SET number=?, client_id=?, status=?, issue_date=?, due_date=?, currency=?, exchange_rate=?, billing_mode=?, period_start=?, period_end=?, po_number=?, discount_type=?, discount_value=?, subtotal=?, discount_total=?, tax_total=?, total=?, amount_paid=?, notes=?, terms=?, footer=?, template_id=?, sent_at=?, viewed_at=?, paid_at=?, custom_fields=?, updated_at=? WHERE id=?`,
			inv.Number, inv.ClientID, inv.Status, inv.IssueDate, inv.DueDate, inv.Currency, inv.ExchangeRate, inv.BillingMode, inv.PeriodStart, inv.PeriodEnd, inv.PONumber, inv.DiscountType, inv.DiscountValue, inv.Subtotal, inv.DiscountTotal, inv.TaxTotal, inv.Total, inv.AmountPaid, inv.Notes, inv.Terms, inv.Footer, inv.TemplateID, inv.SentAt, inv.ViewedAt, inv.PaidAt, marshalCF(inv.CustomFields), Now(), inv.ID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM invoice_items WHERE invoice_id = ?`, inv.ID); err != nil {
			return err
		}
		return insertItems(ctx, tx, inv)
	})
}

// UpdateInvoiceStatus sets status and timestamps.
func (s *Store) UpdateInvoiceStatus(ctx context.Context, id int64, status string, sentAt, viewedAt, paidAt *string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE invoices SET status=?, sent_at=COALESCE(?, sent_at), viewed_at=COALESCE(?, viewed_at), paid_at=COALESCE(?, paid_at), updated_at=? WHERE id=?`, status, sentAt, viewedAt, paidAt, Now(), id)
	return err
}

// DeleteInvoice removes an invoice (and unlinks time entries).
func (s *Store) DeleteInvoice(ctx context.Context, id int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE time_entries SET invoice_id = NULL WHERE invoice_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE expenses SET invoice_id = NULL WHERE invoice_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM invoices WHERE id = ?`, id)
		return err
	})
}

// RecalcAmountPaid recomputes amount_paid from payments and updates status.
func (s *Store) RecalcAmountPaid(ctx context.Context, id int64) (*Invoice, error) {
	var paid float64
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(applied_amount), 0) FROM payments WHERE invoice_id = ?`, id).Scan(&paid); err != nil {
		return nil, err
	}
	inv, err := s.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	inv.AmountPaid = paid
	status := inv.Status
	var paidAt *string
	switch {
	case inv.Status == StatusCancelled || inv.Status == StatusDraft:
		// leave as is
	case paid >= inv.Total-0.005 && inv.Total > 0:
		status = StatusPaid
		now := Now()
		paidAt = &now
	case paid > 0:
		status = StatusPartial
		if inv.DueDate < Today() {
			status = StatusOverdue
		}
	default:
		if inv.Status == StatusPaid || inv.Status == StatusPartial {
			status = StatusSent
		}
		if inv.DueDate < Today() && status != StatusDraft {
			status = StatusOverdue
		}
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE invoices SET amount_paid = ?, status = ?, paid_at = ?, updated_at = ? WHERE id = ?`, paid, status, paidAt, Now(), id); err != nil {
		return nil, err
	}
	return s.GetInvoice(ctx, id)
}

// MarkOverdue flips open invoices past their due date to overdue. Returns ids changed.
func (s *Store) MarkOverdue(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM invoices WHERE status IN ('sent','viewed','partial') AND due_date < ?`, Today())
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `UPDATE invoices SET status = 'overdue', updated_at = ? WHERE id = ?`, Now(), id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

// NumberExists checks whether an invoice number is taken.
func (s *Store) NumberExists(ctx context.Context, number string, excludeID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM invoices WHERE number = ? AND id != ?`, number, excludeID).Scan(&n)
	return n > 0, err
}

// LinkTimeEntries attaches time entries to an invoice.
func (s *Store) LinkTimeEntries(ctx context.Context, invoiceID int64, entryIDs []int64) error {
	for _, id := range entryIDs {
		if _, err := s.DB.ExecContext(ctx, `UPDATE time_entries SET invoice_id = ? WHERE id = ?`, invoiceID, id); err != nil {
			return err
		}
	}
	return nil
}

// LinkExpenses attaches expenses to an invoice.
func (s *Store) LinkExpenses(ctx context.Context, invoiceID int64, ids []int64) error {
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `UPDATE expenses SET invoice_id = ? WHERE id = ?`, invoiceID, id); err != nil {
			return err
		}
	}
	return nil
}

// InvoicesDueForReminder returns overdue invoices whose days-overdue matches one of `days`.
func (s *Store) InvoicesDueForReminder(ctx context.Context, days []int) ([]Invoice, error) {
	if len(days) == 0 {
		return nil, nil
	}
	q := `SELECT ` + invoiceCols + ` FROM invoices i JOIN clients c ON c.id = i.client_id WHERE i.status IN ('sent','viewed','partial','overdue') AND (`
	args := []any{}
	for idx, d := range days {
		if idx > 0 {
			q += ` OR `
		}
		q += `i.due_date = date(?, ?)`
		args = append(args, Today(), fmt.Sprintf("-%d days", d))
	}
	q += `)`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}
