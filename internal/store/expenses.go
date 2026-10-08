package store

import "context"

const expenseCols = `e.id, e.client_id, COALESCE(c.name, ''), e.date, e.category, e.description, e.amount, e.currency, e.exchange_rate, e.billable, e.invoice_id, e.created_at`

// ExpenseFilter narrows ListExpenses.
type ExpenseFilter struct {
	ClientID int64
	Unbilled bool
	From, To string
}

// ListExpenses lists expenses.
func (s *Store) ListExpenses(ctx context.Context, f ExpenseFilter) ([]Expense, error) {
	q := `SELECT ` + expenseCols + ` FROM expenses e LEFT JOIN clients c ON c.id = e.client_id WHERE 1=1`
	args := []any{}
	if f.ClientID > 0 {
		q += ` AND e.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.Unbilled {
		q += ` AND e.billable = 1 AND e.invoice_id IS NULL`
	}
	if f.From != "" {
		q += ` AND e.date >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND e.date <= ?`
		args = append(args, f.To)
	}
	q += ` ORDER BY e.date DESC, e.id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Expense{}
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.ID, &e.ClientID, &e.ClientName, &e.Date, &e.Category, &e.Description, &e.Amount, &e.Currency, &e.ExchangeRate, &e.Billable, &e.InvoiceID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetExpense returns one expense.
func (s *Store) GetExpense(ctx context.Context, id int64) (*Expense, error) {
	var e Expense
	err := s.DB.QueryRowContext(ctx, `SELECT `+expenseCols+` FROM expenses e LEFT JOIN clients c ON c.id = e.client_id WHERE e.id = ?`, id).Scan(&e.ID, &e.ClientID, &e.ClientName, &e.Date, &e.Category, &e.Description, &e.Amount, &e.Currency, &e.ExchangeRate, &e.Billable, &e.InvoiceID, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// SaveExpense creates or updates an expense.
func (s *Store) SaveExpense(ctx context.Context, e *Expense) error {
	if e.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO expenses (client_id, date, category, description, amount, currency, exchange_rate, billable, invoice_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ClientID, e.Date, e.Category, e.Description, e.Amount, e.Currency, e.ExchangeRate, e.Billable, e.InvoiceID)
		if err != nil {
			return err
		}
		e.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE expenses SET client_id=?, date=?, category=?, description=?, amount=?, currency=?, exchange_rate=?, billable=?, invoice_id=? WHERE id=?`,
		e.ClientID, e.Date, e.Category, e.Description, e.Amount, e.Currency, e.ExchangeRate, e.Billable, e.InvoiceID, e.ID)
	return err
}

// DeleteExpense removes an expense.
func (s *Store) DeleteExpense(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM expenses WHERE id = ?`, id)
	return err
}
