package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// CountUsers returns the number of users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a user.
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO users (email, name, password_hash, role) VALUES (?, ?, ?, ?)`, u.Email, u.Name, u.PasswordHash, u.Role)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return s.AdoptOrphans(ctx)
}

// UpdateUser updates name/email/password/role.
func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET email = ?, name = ?, password_hash = ?, role = ?, updated_at = ? WHERE id = ?`, u.Email, u.Name, u.PasswordHash, u.Role, Now(), u.ID)
	return err
}

// DeleteUser removes a user together with their logs, settings, sessions and tokens.
// Callers refuse first when the user still owns business records (OwnedRecords).
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM activity_log WHERE user_id = ?`, `DELETE FROM email_log WHERE user_id = ?`, `DELETE FROM user_settings WHERE user_id = ?`, `DELETE FROM users WHERE id = ?`} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

const userCols = `id, email, name, password_hash, role, created_at`

// GetUserByEmail finds a user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

// GetUser finds a user by id.
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// ListUsers returns all users.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// RandomToken returns n random bytes hex encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// HashToken returns the sha256 hex digest of a token.
func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// CreateSession creates a session for a user and returns its id.
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	id := RandomToken(32)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`, HashToken(id), userID, time.Now().Add(ttl).UTC().Format(time.RFC3339))
	return id, err
}

// GetSessionUser resolves a session id to a user.
func (s *Store) GetSessionUser(ctx context.Context, sessionID string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT u.id, u.email, u.name, u.password_hash, u.role, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ? AND s.expires_at > ?`, HashToken(sessionID), time.Now().UTC().Format(time.RFC3339)))
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, HashToken(sessionID))
	return err
}

// PurgeSessions removes expired sessions.
func (s *Store) PurgeSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}

// CreateAPIToken creates a token and returns the plaintext (shown once).
func (s *Store) CreateAPIToken(ctx context.Context, userID int64, name string) (string, *APIToken, error) {
	plain := "pi_" + RandomToken(24)
	prefix := plain[:10]
	res, err := s.DB.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash, prefix) VALUES (?, ?, ?, ?)`, userID, name, HashToken(plain), prefix)
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return plain, &APIToken{ID: id, UserID: userID, Name: name, Prefix: prefix, CreatedAt: Now()}, nil
}

// GetUserByAPIToken resolves a bearer token.
func (s *Store) GetUserByAPIToken(ctx context.Context, token string) (*User, error) {
	h := HashToken(token)
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT u.id, u.email, u.name, u.password_hash, u.role, u.created_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`, h))
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?`, Now(), h)
	return u, nil
}

// ListAPITokens lists tokens for a user.
func (s *Store) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, name, prefix, last_used_at, created_at FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.LastUsedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken removes a token.
func (s *Store) DeleteAPIToken(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ContentTables lists every table whose rows belong to one user (user_id column).
// invoice_items is not listed: its rows inherit the owner of their invoice.
var ContentTables = []string{"clients", "products", "tax_rates", "invoice_templates", "invoices", "payments", "recurring_invoices", "time_entries", "expenses", "webhooks", "activity_log", "email_log", "attachments", "documents", "paperless_links", "documents_links"}

// OwnerID returns the box owner: the first admin account, else the first account, else 0.
func (s *Store) OwnerID(ctx context.Context) int64 {
	var id sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1), (SELECT MIN(id) FROM users))`).Scan(&id)
	return id.Int64
}

// AdoptOrphans gives rows without an owner (created before any account existed, e.g. demo
// data on a fresh install) and a not-yet-split settings blob to the owner account. Idempotent;
// a no-op when there is no account yet or nothing is ownerless.
func (s *Store) AdoptOrphans(ctx context.Context) error {
	owner := s.OwnerID(ctx)
	if owner == 0 {
		return nil
	}
	for _, t := range ContentTables {
		if _, err := s.DB.ExecContext(ctx, `UPDATE `+t+` SET user_id = ? WHERE user_id IS NULL`, owner); err != nil {
			return fmt.Errorf("adopt %s: %w", t, err)
		}
	}
	// A global settings row that still carries personal keys (written before the first
	// account existed) moves to the owner, unless the owner already has settings.
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'app'`).Scan(&raw); err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	if _, personal := m["company_name"]; !personal {
		return nil
	}
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM user_settings WHERE user_id = ?`, owner).Scan(&n)
	if n == 0 {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO user_settings (user_id, value) VALUES (?, ?)`, owner, raw); err != nil {
			return err
		}
	}
	box, err := s.getBox(ctx)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(box)
	return s.SetKV(ctx, "app", string(b))
}

// OwnedRecords counts the content rows a user owns (used to refuse deleting such a user).
func (s *Store) OwnedRecords(ctx context.Context, uid int64) (int, error) {
	total := 0
	for _, t := range ContentTables {
		if t == "activity_log" || t == "email_log" {
			continue
		}
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+t+` WHERE user_id = ?`, uid).Scan(&n); err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}
