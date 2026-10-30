package server

import (
	"context"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- Connection modes ----
//
// A sibling Pocket app (Pocket Documents, Pocket Time) is reached in one of
// three ways, chosen per person (see pocketdocs.go, pockettime.go): core (a
// Pocket Core app link), direct (a URL and the person's own token saved here)
// or off.
//
// The mode in effect is reported to Core ("documents" and "time" per person) so its Links
// page shows Direct connections too: after registering (on start and every 30
// minutes with the re-registration) and right after someone changes their mode
// or disconnects. Report only; failures are logged.

const (
	modeCore   = "core"
	modeDirect = "direct"
	modeOff    = "off"
)

// linkUses is what Invoicing does in other Pocket apps for a person (manifest Uses).
func linkUses() []core.Use {
	return []core.Use{
		{App: "documents", Scopes: documentsScopes, Reason: "File issued invoices, their attachments and company documents as documents of the client, and show a client's documents"},
		{App: "time", Scopes: timeScopes, Reason: "Your timer here is the same timer as in Pocket Time"},
	}
}

func validMode(m string) bool { return m == modeCore || m == modeDirect || m == modeOff }

// connMode is the mode in effect: the chosen one, else direct when a token
// was saved, else Pocket Core when the box has one.
func connMode(mode, token string, coreOn bool) string {
	if validMode(mode) {
		return mode
	}
	if token == "" && coreOn {
		return modeCore
	}
	return modeDirect
}

// connModes is the Pocket Documents and Pocket Time mode in effect (what Settings shows,
// defaults included) for each person, or just uid when uid > 0.
func (s *Server) connModes(ctx context.Context, uid int64) ([]core.ConnMode, error) {
	var users []store.User
	if uid > 0 {
		u, err := s.store.GetUser(ctx, uid)
		if err != nil {
			return nil, err
		}
		users = []store.User{*u}
	} else {
		list, err := s.store.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		users = list
	}
	out := make([]core.ConnMode, 0, len(users))
	for _, u := range users {
		st, err := s.store.GetSettings(ctx, u.ID)
		if err != nil {
			continue
		}
		out = append(out, core.ConnMode{User: u.Email, Target: "documents", Mode: s.documentsMode(st)},
			core.ConnMode{User: u.Email, Target: "time", Mode: s.timeMode(st)})
	}
	return out, nil
}

// reportModes tells Core how people reach Pocket Documents and Pocket Time (everyone, or just
// uid). Only with a Core; never fatal.
func (s *Server) reportModes(ctx context.Context, uid int64) {
	if !s.core.Configured() {
		return
	}
	s.modesMu.Lock() // one report at a time, so an older list never lands after a newer one
	defer s.modesMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	modes, err := s.connModes(ctx, uid)
	if err == nil {
		err = s.core.ReportModes(ctx, modes)
	}
	if err != nil {
		s.log.Warn("pocket core: reporting connection modes failed", "err", err)
	}
}
