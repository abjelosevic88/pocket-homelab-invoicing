package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const timeCols = `t.id, t.client_id, c.name, t.project, t.description, t.started_at, t.ended_at, t.duration_minutes, t.billable, t.rate, t.invoice_id, t.created_at, t.source, t.external_id`

func scanTime(row interface{ Scan(...any) error }) (*TimeEntry, error) {
	var t TimeEntry
	if err := row.Scan(&t.ID, &t.ClientID, &t.ClientName, &t.Project, &t.Description, &t.StartedAt, &t.EndedAt, &t.DurationMinutes, &t.Billable, &t.Rate, &t.InvoiceID, &t.CreatedAt, &t.Source, &t.ExternalID); err != nil {
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
func (s *Store) ListTimeEntries(ctx context.Context, uid int64, f TimeFilter) ([]TimeEntry, error) {
	q := `SELECT ` + timeCols + ` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.user_id = ?`
	args := []any{uid}
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
func (s *Store) GetTimeEntry(ctx context.Context, uid, id int64) (*TimeEntry, error) {
	return scanTime(s.DB.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.id = ? AND t.user_id = ?`, id, uid))
}

// RunningTimer returns the currently running entry, if any.
func (s *Store) RunningTimer(ctx context.Context, uid int64) (*TimeEntry, error) {
	t, err := scanTime(s.DB.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.user_id = ? AND t.ended_at IS NULL ORDER BY t.started_at DESC LIMIT 1`, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// SaveTimeEntry creates or updates an entry. Source and ExternalID are set
// when it is created (default "app") and never changed afterwards.
func (s *Store) SaveTimeEntry(ctx context.Context, uid int64, t *TimeEntry) error {
	if t.ID == 0 {
		if t.Source == "" {
			t.Source = "app"
		}
		res, err := s.DB.ExecContext(ctx, `INSERT INTO time_entries (user_id, client_id, project, description, started_at, ended_at, duration_minutes, billable, rate, invoice_id, source, external_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			NullInt(uid), t.ClientID, t.Project, t.Description, t.StartedAt, t.EndedAt, t.DurationMinutes, t.Billable, t.Rate, t.InvoiceID, t.Source, t.ExternalID)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE time_entries SET client_id=?, project=?, description=?, started_at=?, ended_at=?, duration_minutes=?, billable=?, rate=?, invoice_id=? WHERE id=? AND user_id=?`,
		t.ClientID, t.Project, t.Description, t.StartedAt, t.EndedAt, t.DurationMinutes, t.Billable, t.Rate, t.InvoiceID, t.ID, uid)
	return notFoundIfNone(res, err)
}

// DeleteTimeEntry removes an entry.
func (s *Store) DeleteTimeEntry(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM time_entries WHERE id = ? AND user_id = ?`, id, uid)
	return notFoundIfNone(res, err)
}

// StopRunningTimers ends any running timer at now.
func (s *Store) StopRunningTimers(ctx context.Context, uid int64, rounding int) ([]TimeEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.user_id = ? AND t.ended_at IS NULL`, uid)
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
		if err := s.SaveTimeEntry(ctx, uid, t); err != nil {
			return nil, err
		}
	}
	return running, nil
}

// TimeByExternalID finds the person's entry another app wrote with this id.
func (s *Store) TimeByExternalID(ctx context.Context, uid int64, externalID string) (*TimeEntry, error) {
	return scanTime(s.DB.QueryRowContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id WHERE t.user_id = ? AND t.external_id = ? AND t.external_id <> ''`, uid, externalID))
}

// FinishedTimeAround lists the person's finished entries that started between
// from-1 day and to+1 day (UTC strings); callers narrow it to local dates.
func (s *Store) FinishedTimeAround(ctx context.Context, uid int64, from, to string) ([]TimeEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+timeCols+` FROM time_entries t JOIN clients c ON c.id = t.client_id
		WHERE t.user_id = ? AND t.ended_at IS NOT NULL AND t.started_at >= date(?, '-1 day') AND t.started_at < date(?, '+2 day')
		ORDER BY t.started_at, t.id`, uid, from, to)
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

// TimeClient is a client time can be for, with its last time and unbilled minutes.
type TimeClient struct {
	ID              int64
	Name            string
	LastAt          string
	UnbilledMinutes int
}

// TimeClients lists the person's active clients for a time picker: the ones
// with the latest time first, then by name; q searches the name.
func (s *Store) TimeClients(ctx context.Context, uid int64, q string, limit int) ([]TimeClient, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.name,
		COALESCE((SELECT MAX(t.started_at) FROM time_entries t WHERE t.client_id = c.id AND t.user_id = c.user_id), '') AS last_at,
		COALESCE((SELECT SUM(t.duration_minutes) FROM time_entries t WHERE t.client_id = c.id AND t.user_id = c.user_id AND t.billable = 1 AND t.invoice_id IS NULL AND t.ended_at IS NOT NULL), 0)
		FROM clients c WHERE c.user_id = ? AND c.archived = 0 AND (? = '' OR c.name LIKE '%' || ? || '%')
		ORDER BY last_at DESC, c.name COLLATE NOCASE LIMIT ?`, uid, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeClient{}
	for rows.Next() {
		var c TimeClient
		if err := rows.Scan(&c.ID, &c.Name, &c.LastAt, &c.UnbilledMinutes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InvoiceNumber is the number of one of the person's invoices ("" if none).
func (s *Store) InvoiceNumber(ctx context.Context, uid, id int64) string {
	var n string
	_ = s.DB.QueryRowContext(ctx, `SELECT number FROM invoices WHERE id = ? AND user_id = ?`, id, uid).Scan(&n)
	return n
}

// SharedTimer is what a timer started here for a client keeps while it runs
// in Pocket Time (which has no free-text project or rate of its own).
type SharedTimer struct {
	TimeEntryID int64
	ClientID    int64
	Project     string
	Rate        *float64
}

// SaveSharedTimer remembers a timer started in Pocket Time (and forgets ones older than 60 days).
func (s *Store) SaveSharedTimer(ctx context.Context, uid int64, t SharedTimer) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM shared_timers WHERE user_id = ? AND created_at < ?`, uid, time.Now().UTC().AddDate(0, 0, -60).Format(time.RFC3339)); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO shared_timers (user_id, time_entry_id, client_id, project, rate) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, time_entry_id) DO UPDATE SET client_id = excluded.client_id, project = excluded.project, rate = excluded.rate`, uid, t.TimeEntryID, t.ClientID, t.Project, t.Rate)
	return err
}

// GetSharedTimer returns what was kept for a timer in Pocket Time, or nil.
func (s *Store) GetSharedTimer(ctx context.Context, uid, timeEntryID int64) *SharedTimer {
	t := SharedTimer{TimeEntryID: timeEntryID}
	if err := s.DB.QueryRowContext(ctx, `SELECT client_id, project, rate FROM shared_timers WHERE user_id = ? AND time_entry_id = ?`, uid, timeEntryID).Scan(&t.ClientID, &t.Project, &t.Rate); err != nil {
		return nil
	}
	return &t
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
