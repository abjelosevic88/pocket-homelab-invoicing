package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Quote statuses.
const (
	QuoteDraft    = "draft"
	QuoteSent     = "sent"
	QuoteViewed   = "viewed"
	QuoteAccepted = "accepted"
	QuoteDeclined = "declined"
	QuoteExpired  = "expired"
	QuoteInvoiced = "invoiced"
)

// Quote is an offer sent to a client before the work. Its lines have the same
// shape as invoice lines, so accepting it can start an invoice with them.
type Quote struct {
	UserID        int64         `json:"-"`
	ID            int64         `json:"id"`
	Number        string        `json:"number"`
	ClientID      int64         `json:"client_id"`
	ClientName    string        `json:"client_name"`
	Status        string        `json:"status"`
	IssueDate     string        `json:"issue_date"`
	ValidUntil    string        `json:"valid_until"`
	Currency      string        `json:"currency"`
	BillingMode   string        `json:"billing_mode"`
	PONumber      string        `json:"po_number"`
	DiscountType  string        `json:"discount_type"`
	DiscountValue float64       `json:"discount_value"`
	Subtotal      float64       `json:"subtotal"`
	DiscountTotal float64       `json:"discount_total"`
	TaxTotal      float64       `json:"tax_total"`
	Total         float64       `json:"total"`
	Notes         string        `json:"notes"`
	Terms         string        `json:"terms"`
	Footer        string        `json:"footer"`
	TemplateID    *int64        `json:"template_id"`
	PublicToken   string        `json:"public_token"`
	Revision      int           `json:"revision"`
	SentAt        *string       `json:"sent_at"`
	ViewedAt      *string       `json:"viewed_at"`
	AcceptedAt    *string       `json:"accepted_at"`
	AcceptedName  string        `json:"accepted_name"`
	AcceptedIP    string        `json:"accepted_ip"`
	DeclinedAt    *string       `json:"declined_at"`
	DeclineReason string        `json:"decline_reason"`
	InvoiceID     *int64        `json:"invoice_id"`
	InvoiceNumber string        `json:"invoice_number"`
	CreatedAt     string        `json:"created_at"`
	UpdatedAt     string        `json:"updated_at"`
	Items         []InvoiceItem `json:"items,omitempty"`
}

// QuoteRef points from an invoice back to the quote it was made from.
type QuoteRef struct {
	ID     int64  `json:"id"`
	Number string `json:"number"`
}

// Open reports whether the client can still answer the quote.
func (q *Quote) Open() bool { return q.Status == QuoteSent || q.Status == QuoteViewed }

// AsInvoice gives the quote the shape the PDF/HTML renderers take. The valid-until
// date travels as the due date; renderers print it under the quote's own label.
func (q *Quote) AsInvoice() *Invoice {
	return &Invoice{
		UserID: q.UserID, ID: q.ID, Number: q.Number, ClientID: q.ClientID, ClientName: q.ClientName, Status: q.Status,
		IssueDate: q.IssueDate, DueDate: q.ValidUntil, Currency: q.Currency, ExchangeRate: 1, BillingMode: q.BillingMode,
		PONumber: q.PONumber, DiscountType: q.DiscountType, DiscountValue: q.DiscountValue,
		Subtotal: q.Subtotal, DiscountTotal: q.DiscountTotal, TaxTotal: q.TaxTotal, Total: q.Total, Balance: q.Total,
		Notes: q.Notes, Terms: q.Terms, Footer: q.Footer, TemplateID: q.TemplateID, PublicToken: q.PublicToken,
		CustomFields: map[string]string{}, WorkedDays: []string{}, Items: q.Items,
	}
}

const quoteCols = `q.id, q.number, q.client_id, c.name, q.status, q.issue_date, q.valid_until, q.currency, q.billing_mode, q.po_number, q.discount_type, q.discount_value, q.subtotal, q.discount_total, q.tax_total, q.total, q.notes, q.terms, q.footer, q.template_id, q.public_token, q.revision, q.sent_at, q.viewed_at, q.accepted_at, q.accepted_name, q.accepted_ip, q.declined_at, q.decline_reason, q.invoice_id, COALESCE(i.number, ''), q.created_at, q.updated_at, q.user_id`

const quoteFrom = ` FROM quotes q JOIN clients c ON c.id = q.client_id LEFT JOIN invoices i ON i.id = q.invoice_id`

func scanQuote(row interface{ Scan(...any) error }) (*Quote, error) {
	var q Quote
	if err := row.Scan(&q.ID, &q.Number, &q.ClientID, &q.ClientName, &q.Status, &q.IssueDate, &q.ValidUntil, &q.Currency, &q.BillingMode, &q.PONumber, &q.DiscountType, &q.DiscountValue, &q.Subtotal, &q.DiscountTotal, &q.TaxTotal, &q.Total, &q.Notes, &q.Terms, &q.Footer, &q.TemplateID, &q.PublicToken, &q.Revision, &q.SentAt, &q.ViewedAt, &q.AcceptedAt, &q.AcceptedName, &q.AcceptedIP, &q.DeclinedAt, &q.DeclineReason, &q.InvoiceID, &q.InvoiceNumber, &q.CreatedAt, &q.UpdatedAt, &q.UserID); err != nil {
		return nil, err
	}
	return &q, nil
}

// QuoteFilter narrows ListQuotes.
type QuoteFilter struct {
	Status   string // a status, "open" (sent or viewed) or "" / "all"
	ClientID int64
	Search   string
	Limit    int
	Offset   int
}

// ListQuotes returns the user's quotes (no items), newest first.
func (s *Store) ListQuotes(ctx context.Context, uid int64, f QuoteFilter) ([]Quote, int, error) {
	where := quoteFrom + ` WHERE q.user_id = ?`
	args := []any{uid}
	switch f.Status {
	case "", "all":
	case "open":
		where += ` AND q.status IN ('sent','viewed')`
	default:
		where += ` AND q.status = ?`
		args = append(args, f.Status)
	}
	if f.ClientID > 0 {
		where += ` AND q.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.Search = strings.TrimSpace(f.Search); f.Search != "" {
		like := "%" + f.Search + "%"
		where += ` AND (q.number LIKE ? OR c.name LIKE ? OR q.po_number LIKE ?)`
		args = append(args, like, like, like)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sqlq := `SELECT ` + quoteCols + where + ` ORDER BY q.issue_date DESC, q.id DESC`
	if f.Limit > 0 {
		sqlq += fmt.Sprintf(` LIMIT %d OFFSET %d`, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, sqlq, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Quote{}
	for rows.Next() {
		q, err := scanQuote(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *q)
	}
	return out, total, rows.Err()
}

// GetQuote loads one of the user's quotes with its lines.
func (s *Store) GetQuote(ctx context.Context, uid, id int64) (*Quote, error) {
	q, err := scanQuote(s.DB.QueryRowContext(ctx, `SELECT `+quoteCols+quoteFrom+` WHERE q.id = ? AND q.user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	return s.hydrateQuote(ctx, q)
}

// GetQuoteByToken loads a quote by its public token (any owner; UserID tells whose).
func (s *Store) GetQuoteByToken(ctx context.Context, token string) (*Quote, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	q, err := scanQuote(s.DB.QueryRowContext(ctx, `SELECT `+quoteCols+quoteFrom+` WHERE q.public_token = ?`, token))
	if err != nil {
		return nil, err
	}
	return s.hydrateQuote(ctx, q)
}

func (s *Store) hydrateQuote(ctx context.Context, q *Quote) (*Quote, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, quote_id, position, description, unit, quantity, unit_price, tax_rate, discount, line_total FROM quote_items WHERE quote_id = ? ORDER BY position, id`, q.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	q.Items = []InvoiceItem{}
	for rows.Next() {
		var it InvoiceItem
		if err := rows.Scan(&it.ID, &it.InvoiceID, &it.Position, &it.Description, &it.Unit, &it.Quantity, &it.UnitPrice, &it.TaxRate, &it.Discount, &it.LineTotal); err != nil {
			return nil, err
		}
		q.Items = append(q.Items, it)
	}
	return q, rows.Err()
}

func insertQuoteItems(ctx context.Context, tx *sql.Tx, q *Quote) error {
	for i := range q.Items {
		it := &q.Items[i]
		it.InvoiceID, it.Position = q.ID, i
		res, err := tx.ExecContext(ctx, `INSERT INTO quote_items (quote_id, position, description, unit, quantity, unit_price, tax_rate, discount, line_total) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			q.ID, i, it.Description, it.Unit, it.Quantity, it.UnitPrice, it.TaxRate, it.Discount, it.LineTotal)
		if err != nil {
			return err
		}
		it.ID, _ = res.LastInsertId()
	}
	return nil
}

// CreateQuote inserts a quote and its lines.
func (s *Store) CreateQuote(ctx context.Context, uid int64, q *Quote) error {
	q.UserID = uid
	if q.PublicToken == "" {
		q.PublicToken = RandomToken(16)
	}
	if q.Revision < 1 {
		q.Revision = 1
	}
	return s.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO quotes (user_id, number, client_id, status, issue_date, valid_until, currency, billing_mode, po_number, discount_type, discount_value, subtotal, discount_total, tax_total, total, notes, terms, footer, template_id, public_token, revision, sent_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uid, q.Number, q.ClientID, q.Status, q.IssueDate, q.ValidUntil, q.Currency, q.BillingMode, q.PONumber, q.DiscountType, q.DiscountValue, q.Subtotal, q.DiscountTotal, q.TaxTotal, q.Total, q.Notes, q.Terms, q.Footer, q.TemplateID, q.PublicToken, q.Revision, q.SentAt)
		if err != nil {
			return err
		}
		q.ID, _ = res.LastInsertId()
		return insertQuoteItems(ctx, tx, q)
	})
}

// UpdateQuote replaces a quote's content, status and lines (answers and the
// invoice link are written by their own calls).
func (s *Store) UpdateQuote(ctx context.Context, uid int64, q *Quote) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE quotes SET number=?, client_id=?, status=?, issue_date=?, valid_until=?, currency=?, billing_mode=?, po_number=?, discount_type=?, discount_value=?, subtotal=?, discount_total=?, tax_total=?, total=?, notes=?, terms=?, footer=?, template_id=?, revision=?, sent_at=?, viewed_at=?, accepted_at=?, accepted_name=?, accepted_ip=?, declined_at=?, decline_reason=?, updated_at=? WHERE id=? AND user_id=?`,
			q.Number, q.ClientID, q.Status, q.IssueDate, q.ValidUntil, q.Currency, q.BillingMode, q.PONumber, q.DiscountType, q.DiscountValue, q.Subtotal, q.DiscountTotal, q.TaxTotal, q.Total, q.Notes, q.Terms, q.Footer, q.TemplateID, q.Revision, q.SentAt, q.ViewedAt, q.AcceptedAt, q.AcceptedName, q.AcceptedIP, q.DeclinedAt, q.DeclineReason, Now(), q.ID, uid)
		if err := notFoundIfNone(res, err); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM quote_items WHERE quote_id = ?`, q.ID); err != nil {
			return err
		}
		return insertQuoteItems(ctx, tx, q)
	})
}

// MarkQuoteSent moves a draft to sent (other statuses keep theirs) and stamps sent_at once.
func (s *Store) MarkQuoteSent(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE quotes SET status = CASE WHEN status = 'draft' THEN 'sent' ELSE status END, sent_at = COALESCE(sent_at, ?), updated_at = ? WHERE id = ? AND user_id = ?`, Now(), Now(), id, uid)
	return notFoundIfNone(res, err)
}

// MarkQuoteViewed records the first time the client opened the public link.
func (s *Store) MarkQuoteViewed(ctx context.Context, uid, id int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE quotes SET viewed_at = ?, status = CASE WHEN status = 'sent' THEN 'viewed' ELSE status END, updated_at = ? WHERE id = ? AND user_id = ? AND viewed_at IS NULL`, Now(), Now(), id, uid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AcceptQuote records the acceptance when the quote is in one of the from
// statuses. It is atomic: two clicks at once accept it only once (ok=false
// for the second).
func (s *Store) AcceptQuote(ctx context.Context, uid, id int64, name, po, ip string, from ...string) (bool, error) {
	if len(from) == 0 {
		from = []string{QuoteSent, QuoteViewed}
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	args := []any{Now(), name, ip, Now(), po, po, id, uid}
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE quotes SET status = 'accepted', accepted_at = ?, accepted_name = ?, accepted_ip = ?, declined_at = NULL, decline_reason = '', updated_at = ?,
		po_number = CASE WHEN ? <> '' THEN ? ELSE po_number END WHERE id = ? AND user_id = ? AND status IN (`+ph+`)`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeclineQuote records a refusal while the quote is open (or, for the person
// marking it by hand, from the given statuses).
func (s *Store) DeclineQuote(ctx context.Context, uid, id int64, reason string, from ...string) (bool, error) {
	if len(from) == 0 {
		from = []string{QuoteSent, QuoteViewed}
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	args := []any{Now(), reason, Now(), id, uid}
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE quotes SET status = 'declined', declined_at = ?, decline_reason = ?, updated_at = ? WHERE id = ? AND user_id = ? AND status IN (`+ph+`)`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// LinkQuoteInvoice records the invoice made from an accepted quote.
func (s *Store) LinkQuoteInvoice(ctx context.Context, uid, id, invoiceID int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE quotes SET status = 'invoiced', invoice_id = ?, updated_at = ? WHERE id = ? AND user_id = ?`, invoiceID, Now(), id, uid)
	return notFoundIfNone(res, err)
}

// SetQuoteStatus sets a status with no other bookkeeping (back to draft clears the answers).
func (s *Store) SetQuoteStatus(ctx context.Context, uid, id int64, status string) error {
	q := `UPDATE quotes SET status = ?, updated_at = ? WHERE id = ? AND user_id = ?`
	if status == QuoteDraft {
		q = `UPDATE quotes SET status = ?, updated_at = ?, sent_at = NULL, viewed_at = NULL, accepted_at = NULL, accepted_name = '', accepted_ip = '', declined_at = NULL, decline_reason = '' WHERE id = ? AND user_id = ?`
	}
	res, err := s.DB.ExecContext(ctx, q, status, Now(), id, uid)
	return notFoundIfNone(res, err)
}

// DeleteQuote removes a quote and its lines (an invoice made from it stays).
func (s *Store) DeleteQuote(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM quotes WHERE id = ? AND user_id = ?`, id, uid)
	return notFoundIfNone(res, err)
}

// MarkQuotesExpired flips open quotes past their valid-until date to expired. Returns ids changed.
func (s *Store) MarkQuotesExpired(ctx context.Context, uid int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM quotes WHERE user_id = ? AND status IN ('sent','viewed') AND valid_until < ?`, uid, Today())
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
		if _, err := s.DB.ExecContext(ctx, `UPDATE quotes SET status = 'expired', updated_at = ? WHERE id = ? AND user_id = ? AND status IN ('sent','viewed')`, Now(), id, uid); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

// QuoteNumberExists checks whether one of the user's quotes already uses a number.
func (s *Store) QuoteNumberExists(ctx context.Context, uid int64, number string, excludeID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM quotes WHERE user_id = ? AND number = ? AND id != ?`, uid, number, excludeID).Scan(&n)
	return n > 0, err
}

// QuoteForInvoice returns the quote an invoice was made from, if any.
func (s *Store) QuoteForInvoice(ctx context.Context, uid, invoiceID int64) *QuoteRef {
	var r QuoteRef
	if err := s.DB.QueryRowContext(ctx, `SELECT id, number FROM quotes WHERE user_id = ? AND invoice_id = ? ORDER BY id DESC LIMIT 1`, uid, invoiceID).Scan(&r.ID, &r.Number); err != nil {
		return nil
	}
	return &r
}
