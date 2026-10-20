package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- Service connections (Pocket Core) ----
//
// A third-party service (Paperless-ngx) is reached in one of three ways,
// chosen per person under Settings → Paperless:
//
//	core   – Pocket Core hands out the service's address and the person's own
//	         key (the box's key only when the person is a Core admin); the SDK
//	         reuses it for about a minute (a switch-off in Core applies within
//	         that, a refusal at once) and rides out a Core restart; Test
//	         connection always asks afresh
//	direct – the URL and token saved here (how it always worked)
//	off    – the app does not talk to the service at all
//
// With no mode stored: direct when a token is saved (existing setups keep
// working), else Pocket Core when the box has one, else direct.
//
// The mode in effect is reported to Core (service:paperless per person) so its
// Links page shows Direct connections too: after registering (on start and
// every 30 minutes with the re-registration) and right after someone changes
// their mode or disconnects. Report only; failures are logged.

const (
	modeCore   = "core"
	modeDirect = "direct"
	modeOff    = "off"
)

// serviceUses are the third-party services listed in the Core manifest.
var serviceUses = []core.ServiceUse{
	{Type: "paperless", Reason: "Archive invoices and documents in Paperless and browse your archive"},
}

func validMode(m string) bool { return m == modeCore || m == modeDirect || m == modeOff }

// connMode is the mode in effect.
func connMode(mode, token string, coreOn bool) string {
	if validMode(mode) {
		return mode
	}
	if token == "" && coreOn {
		return modeCore
	}
	return modeDirect
}

// serviceReason turns Core's refusal into a sentence for the person.
func serviceReason(name, reason string) string {
	switch reason {
	case "off_person":
		return "you switched Pocket Invoicing's use of " + name + " off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "Pocket Invoicing's use of " + name + " is switched off in Pocket Core (Links)"
	case "no_account":
		return "you have no " + name + " account of your own in Pocket Core yet (an admin or a connector can add one)"
	case "not_in_audience":
		return "you do not have access to " + name + " in Pocket Core"
	case "target_missing":
		return "there is no " + name + " service in Pocket Core"
	case "not_declared":
		return "Pocket Core has not seen this Pocket Invoicing version yet; it re-registers within a minute of starting"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	}
	return reason
}

// serviceErr explains a ServiceFor / LinkStatus failure.
func serviceErr(name string, err error) error {
	var le *core.LinkError
	if errors.As(err, &le) {
		return errors.New(serviceReason(name, le.Reason))
	}
	return fmt.Errorf("%s through Pocket Core: %w", name, err)
}

// sourceText says whose key Core handed out.
func sourceText(source string) string {
	switch source {
	case "personal":
		return " (your own key)"
	case "box":
		return " (the box's key)"
	}
	return ""
}

// lastSource remembers, per person and service, whose key Core last handed
// out ("personal" | "box"), so the status line can say it without asking
// Core for the key again. Never the key itself.
type sourceMemo struct {
	mu sync.Mutex
	m  map[string]string
}

func (sm *sourceMemo) set(email, typ, source string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.m == nil {
		sm.m = map[string]string{}
	}
	sm.m[email+"\x00"+typ] = source
}

func (sm *sourceMemo) get(email, typ string) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.m[email+"\x00"+typ]
}

// serviceFor asks Core for the person's way into a service: ServiceFor (the
// SDK's one-minute cache) for normal work, ServiceForFresh when fresh (Test
// connection).
func (s *Server) serviceFor(ctx context.Context, email, typ, name string, fresh bool) (*core.ServiceCred, error) {
	if !s.core.Configured() {
		return nil, fmt.Errorf("%s through Pocket Core: there is no Pocket Core on this box; use a direct connection", name)
	}
	var cred *core.ServiceCred
	var err error
	if fresh {
		cred, err = s.core.ServiceForFresh(ctx, email, typ)
	} else {
		cred, err = s.core.ServiceFor(ctx, email, typ)
	}
	if err != nil {
		return nil, serviceErr(name, err)
	}
	if cred.URL == "" {
		return nil, fmt.Errorf("the %s service in Pocket Core has no URL", name)
	}
	s.sources.set(email, typ, cred.Source)
	return cred, nil
}

// serviceStatus says, without fetching a key, whether the person reaches the
// service through Core: via "core" or "" plus why not.
func (s *Server) serviceStatus(ctx context.Context, email, typ, name string) (via, detail string) {
	if !s.core.Configured() {
		return "", "there is no Pocket Core on this box"
	}
	list, err := s.core.LinkStatus(ctx, email, "service:"+typ)
	if err != nil {
		return "", serviceErr(name, err).Error()
	}
	if len(list) == 0 {
		// Core answers an empty list both for an unknown person and for an app version it has not seen yet
		return "", "Pocket Core does not know your email, or has not seen this Pocket Invoicing version yet (it re-registers within a minute of starting)"
	}
	for _, l := range list {
		if l.Scope == "account" && !l.On {
			return "", serviceReason(name, l.State)
		}
	}
	return modeCore, ""
}

// ---- Modes on Core's map ----

// paperlessModes is the Paperless mode in effect (what Settings → Paperless
// shows, defaults included) for each person, or just uid when uid > 0.
func (s *Server) paperlessModes(ctx context.Context, uid int64) ([]core.ConnMode, error) {
	s.seedPaperlessEnv(ctx) // the owner's mode follows the seeded token
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
		out = append(out, core.ConnMode{User: u.Email, Target: "service:paperless", Mode: s.paperlessMode(st)})
	}
	return out, nil
}

// reportModes tells Core how people reach Paperless (everyone, or just uid).
// Only with a Core; never fatal.
func (s *Server) reportModes(ctx context.Context, uid int64) {
	if !s.core.Configured() {
		return
	}
	s.modesMu.Lock() // one report at a time, so an older list never lands after a newer one
	defer s.modesMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	modes, err := s.paperlessModes(ctx, uid)
	if err == nil {
		err = s.core.ReportModes(ctx, modes)
	}
	if err != nil {
		s.log.Warn("pocket core: reporting connection modes failed", "err", err)
	}
}
