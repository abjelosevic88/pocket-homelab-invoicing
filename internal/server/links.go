package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it, and it sees
// exactly the person's own invoices, payments and time.
//
// worklog:read|write is the worklog contract of Pocket Time
// (docs/worklog.md in pocket-homelab-time, see worklog.go): Pocket Time shows
// the time entries kept here, and writes time tracked there for a client here,
// ready to bill.

var linkOffers = []core.Offer{
	{Scope: "invoices:read", Name: "See invoices and payments", Description: "List your invoices and the payments received, for your journal."},
	{Scope: "worklog:read", Name: "See your time entries", Description: "See the time you logged for your clients here (when, how long, for which client and what it was) and your client names, for Pocket Time. Never rates or amounts."},
	{Scope: "worklog:write", Name: "Add time to bill", Description: "Add time you tracked elsewhere for a client as an unbilled time entry here, and change or remove only the entries it added itself. Time already on an invoice is never changed."},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/invoices$`), "invoices:read"},
	{"GET", regexp.MustCompile(`^/api/v1/invoices/\d+$`), "invoices:read"},
	{"GET", regexp.MustCompile(`^/api/v1/payments$`), "invoices:read"},
	{"GET", regexp.MustCompile(`^/api/v1/worklog$`), "worklog:read"},
	{"GET", regexp.MustCompile(`^/api/v1/worklog/items$`), "worklog:read"},
	{"POST", regexp.MustCompile(`^/api/v1/worklog$`), "worklog:write"},
	{"DELETE", regexp.MustCompile(`^/api/v1/worklog/[^/]+$`), "worklog:write"},
}

var errLinkScope = errors.New("outside the link's scope")

func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token and maps it to the person it acts for. The
// second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "invoicing" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

type linkAppKey struct{}

// linkApp is the slug of the Pocket app acting through a Core link ("" = the person themselves).
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey{}).(string)
	return a
}
