package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

type ctxKey int

const userKey ctxKey = 1

const sessionCookie = "pi_session"

func userFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey).(*store.User)
	return u
}

// requireAuth resolves the current user from a session cookie, bearer token,
// or trusted proxy header depending on AUTH_MODE.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.resolveUser(r)
		if err != nil || u == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func (s *Server) resolveUser(r *http.Request) (*store.User, error) {
	ctx := r.Context()
	// 1. API token
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return s.store.GetUserByAPIToken(ctx, strings.TrimSpace(h[7:]))
	}
	if t := r.Header.Get("X-API-Key"); t != "" {
		return s.store.GetUserByAPIToken(ctx, t)
	}
	switch s.cfg.AuthMode {
	case "none":
		return s.firstOrSystemUser(ctx, "admin@localhost", "Admin")
	case "proxy":
		name := r.Header.Get(s.cfg.AuthProxyHeader)
		if name == "" {
			return nil, http.ErrNoCookie
		}
		email := r.Header.Get(s.cfg.AuthProxyEmailHeader)
		if email == "" {
			email = name
			if !strings.Contains(email, "@") {
				email += "@proxy"
			}
		}
		return s.firstOrSystemUser(ctx, email, name)
	}
	// 2. Session cookie
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, err
	}
	return s.store.GetSessionUser(ctx, c.Value)
}

// firstOrSystemUser returns the user with the given email, creating it if needed.
func (s *Server) firstOrSystemUser(ctx context.Context, email, name string) (*store.User, error) {
	if u, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return u, nil
	}
	u := &store.User{Email: email, Name: name, Role: "admin"}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	s.log.Info("auto-provisioned user", "email", email, "mode", s.cfg.AuthMode)
	return u, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, id string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, err, "count users")
		return
	}
	st, _ := s.store.GetSettings(r.Context())
	u, _ := s.resolveUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup":    n == 0 && s.cfg.AuthMode == "local",
		"setup_complete": st.SetupComplete,
		"auth_mode":      s.cfg.AuthMode,
		"authenticated":  u != nil,
		"user":           u,
		"version":        Version,
		"company_name":   st.CompanyName,
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		s.fail(w, err, "count users")
		return
	}
	if n > 0 {
		writeErr(w, http.StatusForbidden, "setup already completed")
		return
	}
	var in struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		Name         string `json:"name"`
		CompanyName  string `json:"company_name"`
		BaseCurrency string `json:"base_currency"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if in.Email == "" || len(in.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "email and a password of at least 8 characters are required")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	u := &store.User{Email: in.Email, Name: in.Name, PasswordHash: string(hash), Role: "admin"}
	if err := s.store.CreateUser(ctx, u); err != nil {
		s.fail(w, err, "create user")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	if in.CompanyName != "" {
		st.CompanyName = in.CompanyName
	}
	if in.BaseCurrency != "" {
		st.BaseCurrency = strings.ToUpper(in.BaseCurrency)
		_ = s.store.SetCurrencyEnabled(ctx, st.BaseCurrency, true)
	}
	st.SetupComplete = true
	if err := s.store.SaveSettings(ctx, st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	id, err := s.store.CreateSession(ctx, u.ID, s.cfg.SessionTTL)
	if err != nil {
		s.fail(w, err, "create session")
		return
	}
	s.setSessionCookie(w, id, s.cfg.SessionTTL)
	s.log.Info("initial setup completed", "email", u.Email)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.store.GetUserByEmail(r.Context(), strings.TrimSpace(strings.ToLower(in.Email)))
	if err != nil || u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		time.Sleep(300 * time.Millisecond) // slow down brute force
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	id, err := s.store.CreateSession(r.Context(), u.ID, s.cfg.SessionTTL)
	if err != nil {
		s.fail(w, err, "create session")
		return
	}
	s.setSessionCookie(w, id, s.cfg.SessionTTL)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), c.Value)
	}
	s.setSessionCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var in struct {
		Name            string `json:"name"`
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != "" {
		u.Name = in.Name
	}
	if in.Email != "" {
		u.Email = strings.ToLower(strings.TrimSpace(in.Email))
	}
	if in.NewPassword != "" {
		if u.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.CurrentPassword)) != nil {
			writeErr(w, http.StatusBadRequest, "current password is incorrect")
			return
		}
		if len(in.NewPassword) < 8 {
			writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
		u.PasswordHash = string(h)
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		s.fail(w, err, "update user")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// ---- users admin ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.fail(w, err, "list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Email == "" || len(in.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "email and a password of at least 8 characters are required")
		return
	}
	if in.Role == "" {
		in.Role = "admin"
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	u := &store.User{Email: strings.ToLower(strings.TrimSpace(in.Email)), Name: in.Name, PasswordHash: string(h), Role: in.Role}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusBadRequest, "could not create user: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUser(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get user")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Email != "" {
		u.Email = strings.ToLower(strings.TrimSpace(in.Email))
	}
	if in.Name != "" {
		u.Name = in.Name
	}
	if in.Role != "" {
		u.Role = in.Role
	}
	if in.Password != "" {
		h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		u.PasswordHash = string(h)
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		s.fail(w, err, "update user")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	id := idParam(r, "id")
	if me.ID == id {
		writeErr(w, http.StatusBadRequest, "you cannot delete yourself")
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		s.fail(w, err, "delete user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- API tokens ----

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.ListAPITokens(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.fail(w, err, "list tokens")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	_ = decode(r, &in)
	if in.Name == "" {
		in.Name = "token"
	}
	plain, t, err := s.store.CreateAPIToken(r.Context(), userFrom(r.Context()).ID, in.Name)
	if err != nil {
		s.fail(w, err, "create token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "meta": t})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAPIToken(r.Context(), userFrom(r.Context()).ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "delete token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
