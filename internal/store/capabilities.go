package store

import (
	"context"
	"fmt"
	"strings"
)

// Reads for the routes Pocket Search, Pocket Day and Pocket Hub call
// (server/capabilities.go). Each one is the person's own rows only and changes
// nothing.

// likeWord is a LIKE pattern that finds the word anywhere, with % and _ taken
// literally (used with ESCAPE '\').
func likeWord(w string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(w) + "%"
}

// wordsWhere is "every word is found in one of these" as SQL: one bracket per
// word, the columns or-ed inside it.
func wordsWhere(words []string, cols ...string) (string, []any) {
	var sb strings.Builder
	var args []any
	for _, w := range words {
		like := likeWord(w)
		sb.WriteString(" AND (")
		for i, c := range cols {
			if i > 0 {
				sb.WriteString(" OR ")
			}
			sb.WriteString(c + ` LIKE ? ESCAPE '\'`)
			args = append(args, like)
		}
		sb.WriteString(")")
	}
	return sb.String(), args
}

// SearchInvoices finds the person's invoices whose number, client, PO number,
// notes or a line's description has every word, newest first.
func (s *Store) SearchInvoices(ctx context.Context, uid int64, words []string, limit int) ([]Invoice, error) {
	cond, args := wordsWhere(words, "i.number", "c.name", "i.po_number", "i.notes",
		"(SELECT group_concat(it.description, ' ') FROM invoice_items it WHERE it.invoice_id = i.id)")
	rows, err := s.DB.QueryContext(ctx, `SELECT `+invoiceCols+` FROM invoices i JOIN clients c ON c.id = i.client_id WHERE i.user_id = ?`+cond+
		fmt.Sprintf(` ORDER BY i.issue_date DESC, i.id DESC LIMIT %d`, limit), append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// SearchQuotes finds the person's quotes the same way as SearchInvoices.
func (s *Store) SearchQuotes(ctx context.Context, uid int64, words []string, limit int) ([]Quote, error) {
	cond, args := wordsWhere(words, "q.number", "c.name", "q.po_number", "q.notes",
		"(SELECT group_concat(it.description, ' ') FROM quote_items it WHERE it.quote_id = q.id)")
	rows, err := s.DB.QueryContext(ctx, `SELECT `+quoteCols+quoteFrom+` WHERE q.user_id = ?`+cond+
		fmt.Sprintf(` ORDER BY q.issue_date DESC, q.id DESC LIMIT %d`, limit), append([]any{uid}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Quote{}
	for rows.Next() {
		q, err := scanQuote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *q)
	}
	return out, rows.Err()
}

// InvoicesDueOn lists the person's invoices due on a day that were issued
// (not drafts, not cancelled), in number order.
func (s *Store) InvoicesDueOn(ctx context.Context, uid int64, day string, limit int) ([]Invoice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+invoiceCols+` FROM invoices i JOIN clients c ON c.id = i.client_id
		WHERE i.user_id = ? AND i.due_date = ? AND i.status NOT IN ('draft','cancelled') ORDER BY i.number, i.id LIMIT ?`, uid, day, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// OpenInvoicesByDue lists the person's unpaid invoices (sent, viewed, partly
// paid or overdue), the one due first first: the late ones lead.
func (s *Store) OpenInvoicesByDue(ctx context.Context, uid int64, limit int) ([]Invoice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+invoiceCols+` FROM invoices i JOIN clients c ON c.id = i.client_id
		WHERE i.user_id = ? AND i.status IN ('sent','viewed','partial','overdue') ORDER BY i.due_date, i.id LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// QuoteEvent is a quote sent or accepted at a moment (At, as stored: UTC).
type QuoteEvent struct {
	Quote
	Event string // sent | accepted
	At    string
}

// QuoteEventsBetween lists the person's quotes sent or accepted between two
// UTC moments ("2006-01-02 15:04:05", from included, to not), in time order.
// A quote counts as sent on the day it was first sent.
func (s *Store) QuoteEventsBetween(ctx context.Context, uid int64, from, to string, limit int) ([]QuoteEvent, error) {
	out := []QuoteEvent{}
	for _, ev := range []string{"sent", "accepted"} {
		col := "q." + ev + "_at"
		rows, err := s.DB.QueryContext(ctx, `SELECT `+quoteCols+`, `+col+quoteFrom+`
			WHERE q.user_id = ? AND `+col+` IS NOT NULL AND datetime(`+col+`) >= ? AND datetime(`+col+`) < ? ORDER BY `+col+` LIMIT ?`, uid, from, to, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e QuoteEvent
			var at string
			q, err := scanQuote(scanTail{rows, &at})
			if err != nil {
				rows.Close()
				return nil, err
			}
			e.Quote, e.Event, e.At = *q, ev, at
			out = append(out, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// scanTail scans a row with extra columns after the ones a scan function knows.
type scanTail struct {
	row  interface{ Scan(...any) error }
	tail *string
}

func (t scanTail) Scan(dest ...any) error { return t.row.Scan(append(dest, t.tail)...) }
