package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const timeCols = `t.id, t.client_id, c.name, t.project, t.description, t.started_at, t.ended_at, t.duration_minutes, t.billable, t.rate, t.invoice_id, t.created_at`

func scanTime(row interface{ Scan(...any) error }) (*TimeEntry, error) {
	var t TimeEntry
	if err := row.Scan(&t.ID, &t.ClientID, &t.ClientName, &t.Project, &t.Description, &t.StartedAt, &t.EndedAt, &t.DurationMinutes, &t.Billable, &t.Rate, &t.InvoiceID, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// TimeFilter narrows ListTimeEntries.
type TimeFilter struct {
	ClientID int64
	Unbilled bool
	From, To string
	Limit    int
}

// ListTimeEntries lists entries.
func (s *Store) ListTimeEntries(ctx context.Context, f TimeFilter) ([]TimeEntry, error) {
	q := `SELECT ` + timeCols + ` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE 1=1`
	args := []any{}
	if f.ClientID > 0 {
		q += ` AND t.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.Unbilled {
		q += ` AND t.invoice_id IS NULL AND t.billable = 1 AND t.ended_at IS NOT NULL`
	}
	if f.From != "" {
		q += ` AND t.started_at >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND t.started_at < date(?, '+1 day')`
		args = append(args, f.To)
	}
	q += ` ORDER BY t.started_at DESC, t.id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeEntry{}
	for rows.Next() {
		t, err := scanTime(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTimeEntry returns one entry.
func (s *Store) GetTimeEntry(ctx context.Context, id int64) (*TimeEntry, error) {
	return scanTime(s.DB.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.id = ?`, id))
}

// RunningTimer returns the currently running entry, if any.
func (s *Store) RunningTimer(ctx context.Context) (*TimeEntry, error) {
	t, err := scanTime(s.DB.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.ended_at IS NULL ORDER BY t.started_at DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// SaveTimeEntry creates or updates an entry.
func (s *Store) SaveTimeEntry(ctx context.Context, t *TimeEntry) error {
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO time_entries (client_id, project, description, started_at, ended_at, duration_minutes, billable, rate, invoice_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ClientID, t.Project, t.Description, t.StartedAt, t.EndedAt, t.DurationMinutes, t.Billable, t.Rate, t.InvoiceID)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE time_entries SET client_id=?, project=?, description=?, started_at=?, ended_at=?, duration_minutes=?, billable=?, rate=?, invoice_id=? WHERE id=?`,
		t.ClientID, t.Project, t.Description, t.StartedAt, t.EndedAt, t.DurationMinutes, t.Billable, t.Rate, t.InvoiceID, t.ID)
	return err
}

// DeleteTimeEntry removes an entry.
func (s *Store) DeleteTimeEntry(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ?`, id)
	return err
}

// StopRunningTimers ends any running timer at now.
func (s *Store) StopRunningTimers(ctx context.Context, rounding int) ([]TimeEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	var running []TimeEntry
	for rows.Next() {
		t, err := scanTime(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		running = append(running, *t)
	}
	rows.Close()
	now := time.Now().UTC()
	for i := range running {
		t := &running[i]
		start, _ := time.Parse(time.RFC3339, t.StartedAt)
		end := now.Format(time.RFC3339)
		t.EndedAt = &end
		t.DurationMinutes = RoundMinutes(int(now.Sub(start).Minutes()+0.5), rounding)
		if err := s.SaveTimeEntry(ctx, t); err != nil {
			return nil, err
		}
	}
	return running, nil
}

// RoundMinutes rounds minutes up to the nearest `rounding` block (minimum one block).
func RoundMinutes(minutes, rounding int) int {
	if rounding <= 1 {
		if minutes < 1 {
			return 1
		}
		return minutes
	}
	if minutes <= 0 {
		return rounding
	}
	blocks := (minutes + rounding - 1) / rounding
	return blocks * rounding
}
