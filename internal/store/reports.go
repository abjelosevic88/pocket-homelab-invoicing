package store

import "context"

// MonthlyRevenue is revenue (in base currency) grouped by month.
type MonthlyRevenue struct {
	Month    string  `json:"month"` // YYYY-MM
	Invoiced float64 `json:"invoiced"`
	Paid     float64 `json:"paid"`
	Expenses float64 `json:"expenses"`
	Count    int     `json:"count"`
}

// RevenueByMonth returns invoiced (by issue date) and paid (by payment date) amounts in base currency.
func (s *Store) RevenueByMonth(ctx context.Context, from, to string) ([]MonthlyRevenue, error) {
	months := map[string]*MonthlyRevenue{}
	get := func(m string) *MonthlyRevenue {
		if v, ok := months[m]; ok {
			return v
		}
		v := &MonthlyRevenue{Month: m}
		months[m] = v
		return v
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT substr(issue_date,1,7), SUM(total * exchange_rate), COUNT(1) FROM invoices WHERE status NOT IN ('draft','cancelled') AND issue_date >= ? AND issue_date <= ? GROUP BY 1`, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m string
		var v float64
		var n int
		if err := rows.Scan(&m, &v, &n); err != nil {
			rows.Close()
			return nil, err
		}
		r := get(m)
		r.Invoiced = v
		r.Count = n
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT substr(p.date,1,7), SUM(p.applied_amount * i.exchange_rate) FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE p.date >= ? AND p.date <= ? GROUP BY 1`, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m string
		var v float64
		if err := rows.Scan(&m, &v); err != nil {
			rows.Close()
			return nil, err
		}
		get(m).Paid = v
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT substr(date,1,7), SUM(amount * exchange_rate) FROM expenses WHERE date >= ? AND date <= ? GROUP BY 1`, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m string
		var v float64
		if err := rows.Scan(&m, &v); err != nil {
			rows.Close()
			return nil, err
		}
		get(m).Expenses = v
	}
	rows.Close()
	out := make([]MonthlyRevenue, 0, len(months))
	for _, v := range months {
		out = append(out, *v)
	}
	sortMonthly(out)
	return out, nil
}

func sortMonthly(v []MonthlyRevenue) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].Month < v[j-1].Month; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// ClientRevenue is revenue per client in base currency.
type ClientRevenue struct {
	ClientID    int64   `json:"client_id"`
	ClientName  string  `json:"client_name"`
	Currency    string  `json:"currency"`
	Invoiced    float64 `json:"invoiced"`
	Paid        float64 `json:"paid"`
	Outstanding float64 `json:"outstanding"`
	Count       int     `json:"count"`
}

// RevenueByClient aggregates per client.
func (s *Store) RevenueByClient(ctx context.Context, from, to string) ([]ClientRevenue, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.name, c.currency,
		COALESCE(SUM(i.total * i.exchange_rate), 0),
		COALESCE(SUM(i.amount_paid * i.exchange_rate), 0),
		COALESCE(SUM(CASE WHEN i.status IN ('sent','viewed','partial','overdue') THEN (i.total - i.amount_paid) * i.exchange_rate ELSE 0 END), 0),
		COUNT(i.id)
		FROM clients c JOIN invoices i ON i.client_id = c.id AND i.status NOT IN ('draft','cancelled') AND i.issue_date >= ? AND i.issue_date <= ?
		GROUP BY c.id ORDER BY 4 DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClientRevenue{}
	for rows.Next() {
		var r ClientRevenue
		if err := rows.Scan(&r.ClientID, &r.ClientName, &r.Currency, &r.Invoiced, &r.Paid, &r.Outstanding, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AgingBucket is an outstanding-balance bucket.
type AgingBucket struct {
	Bucket string  `json:"bucket"`
	Amount float64 `json:"amount"`
	Count  int     `json:"count"`
}

// Aging returns outstanding balances bucketed by days overdue (base currency).
func (s *Store) Aging(ctx context.Context) ([]AgingBucket, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT
		CASE WHEN julianday(?) - julianday(due_date) <= 0 THEN 'current'
		     WHEN julianday(?) - julianday(due_date) <= 30 THEN '1-30'
		     WHEN julianday(?) - julianday(due_date) <= 60 THEN '31-60'
		     WHEN julianday(?) - julianday(due_date) <= 90 THEN '61-90'
		     ELSE '90+' END AS bucket,
		SUM((total - amount_paid) * exchange_rate), COUNT(1)
		FROM invoices WHERE status IN ('sent','viewed','partial','overdue') GROUP BY bucket`, Today(), Today(), Today(), Today())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]AgingBucket{}
	for rows.Next() {
		var b AgingBucket
		if err := rows.Scan(&b.Bucket, &b.Amount, &b.Count); err != nil {
			return nil, err
		}
		m[b.Bucket] = b
	}
	out := []AgingBucket{}
	for _, k := range []string{"current", "1-30", "31-60", "61-90", "90+"} {
		b := m[k]
		b.Bucket = k
		out = append(out, b)
	}
	return out, rows.Err()
}

// TaxSummary is tax collected per rate (base currency).
type TaxSummary struct {
	Rate    float64 `json:"rate"`
	Taxable float64 `json:"taxable"`
	Tax     float64 `json:"tax"`
}

// TaxByRate aggregates tax per rate for issued invoices in range.
func (s *Store) TaxByRate(ctx context.Context, from, to string) ([]TaxSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT it.tax_rate,
		SUM(it.line_total * (1 - CASE WHEN i.subtotal > 0 THEN i.discount_total / i.subtotal ELSE 0 END) * i.exchange_rate),
		SUM(it.line_total * (1 - CASE WHEN i.subtotal > 0 THEN i.discount_total / i.subtotal ELSE 0 END) * it.tax_rate / 100 * i.exchange_rate)
		FROM invoice_items it JOIN invoices i ON i.id = it.invoice_id
		WHERE i.status NOT IN ('draft','cancelled') AND i.issue_date >= ? AND i.issue_date <= ?
		GROUP BY it.tax_rate ORDER BY it.tax_rate`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaxSummary{}
	for rows.Next() {
		var t TaxSummary
		if err := rows.Scan(&t.Rate, &t.Taxable, &t.Tax); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CurrencySummary is invoiced/outstanding per invoice currency (native amounts).
type CurrencySummary struct {
	Currency    string  `json:"currency"`
	Invoiced    float64 `json:"invoiced"`
	Paid        float64 `json:"paid"`
	Outstanding float64 `json:"outstanding"`
	InBase      float64 `json:"in_base"`
	Count       int     `json:"count"`
}

// ByCurrency aggregates per invoice currency.
func (s *Store) ByCurrency(ctx context.Context, from, to string) ([]CurrencySummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT currency, SUM(total), SUM(amount_paid),
		SUM(CASE WHEN status IN ('sent','viewed','partial','overdue') THEN total - amount_paid ELSE 0 END),
		SUM(total * exchange_rate), COUNT(1)
		FROM invoices WHERE status NOT IN ('draft','cancelled') AND issue_date >= ? AND issue_date <= ? GROUP BY currency ORDER BY 5 DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CurrencySummary{}
	for rows.Next() {
		var c CurrencySummary
		if err := rows.Scan(&c.Currency, &c.Invoiced, &c.Paid, &c.Outstanding, &c.InBase, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TimeSummary is tracked time per client/project.
type TimeSummary struct {
	ClientID   int64  `json:"client_id"`
	ClientName string `json:"client_name"`
	Project    string `json:"project"`
	Minutes    int    `json:"minutes"`
	Billable   int    `json:"billable_minutes"`
	Unbilled   int    `json:"unbilled_minutes"`
	Entries    int    `json:"entries"`
}

// TimeByClient aggregates time entries.
func (s *Store) TimeByClient(ctx context.Context, from, to string) ([]TimeSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.name, t.project,
		SUM(t.duration_minutes),
		SUM(CASE WHEN t.billable = 1 THEN t.duration_minutes ELSE 0 END),
		SUM(CASE WHEN t.billable = 1 AND t.invoice_id IS NULL THEN t.duration_minutes ELSE 0 END),
		COUNT(1)
		FROM time_entries t JOIN clients c ON c.id = t.client_id
		WHERE t.ended_at IS NOT NULL AND t.started_at >= ? AND t.started_at < date(?, '+1 day')
		GROUP BY c.id, t.project ORDER BY c.name, t.project`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeSummary{}
	for rows.Next() {
		var t TimeSummary
		if err := rows.Scan(&t.ClientID, &t.ClientName, &t.Project, &t.Minutes, &t.Billable, &t.Unbilled, &t.Entries); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DashboardStats are headline numbers in base currency.
type DashboardStats struct {
	Outstanding      float64 `json:"outstanding"`
	OutstandingCount int     `json:"outstanding_count"`
	Overdue          float64 `json:"overdue"`
	OverdueCount     int     `json:"overdue_count"`
	PaidThisMonth    float64 `json:"paid_this_month"`
	PaidThisYear     float64 `json:"paid_this_year"`
	InvoicedThisYear float64 `json:"invoiced_this_year"`
	DraftCount       int     `json:"draft_count"`
	UnbilledMinutes  int     `json:"unbilled_minutes"`
	UnbilledExpenses float64 `json:"unbilled_expenses"`
	ActiveClients    int     `json:"active_clients"`
}

// Dashboard computes headline stats.
func (s *Store) Dashboard(ctx context.Context) (DashboardStats, error) {
	var d DashboardStats
	today := Today()
	month := today[:7]
	year := today[:4]
	q := func(dest []any, query string, args ...any) error {
		return s.DB.QueryRowContext(ctx, query, args...).Scan(dest...)
	}
	if err := q([]any{&d.Outstanding, &d.OutstandingCount}, `SELECT COALESCE(SUM((total - amount_paid) * exchange_rate),0), COUNT(1) FROM invoices WHERE status IN ('sent','viewed','partial','overdue')`); err != nil {
		return d, err
	}
	if err := q([]any{&d.Overdue, &d.OverdueCount}, `SELECT COALESCE(SUM((total - amount_paid) * exchange_rate),0), COUNT(1) FROM invoices WHERE status IN ('sent','viewed','partial','overdue') AND due_date < ?`, today); err != nil {
		return d, err
	}
	if err := q([]any{&d.PaidThisMonth}, `SELECT COALESCE(SUM(p.applied_amount * i.exchange_rate),0) FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE substr(p.date,1,7) = ?`, month); err != nil {
		return d, err
	}
	if err := q([]any{&d.PaidThisYear}, `SELECT COALESCE(SUM(p.applied_amount * i.exchange_rate),0) FROM payments p JOIN invoices i ON i.id = p.invoice_id WHERE substr(p.date,1,4) = ?`, year); err != nil {
		return d, err
	}
	if err := q([]any{&d.InvoicedThisYear}, `SELECT COALESCE(SUM(total * exchange_rate),0) FROM invoices WHERE status NOT IN ('draft','cancelled') AND substr(issue_date,1,4) = ?`, year); err != nil {
		return d, err
	}
	if err := q([]any{&d.DraftCount}, `SELECT COUNT(1) FROM invoices WHERE status = 'draft'`); err != nil {
		return d, err
	}
	if err := q([]any{&d.UnbilledMinutes}, `SELECT COALESCE(SUM(duration_minutes),0) FROM time_entries WHERE billable = 1 AND invoice_id IS NULL AND ended_at IS NOT NULL`); err != nil {
		return d, err
	}
	if err := q([]any{&d.UnbilledExpenses}, `SELECT COALESCE(SUM(amount * exchange_rate),0) FROM expenses WHERE billable = 1 AND invoice_id IS NULL`); err != nil {
		return d, err
	}
	if err := q([]any{&d.ActiveClients}, `SELECT COUNT(1) FROM clients WHERE archived = 0`); err != nil {
		return d, err
	}
	return d, nil
}
