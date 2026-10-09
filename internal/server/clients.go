package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListClients(r.Context(), r.URL.Query().Get("archived") == "1", r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, err, "list clients")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetClient(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get client")
		return
	}
	invoices, _, _ := s.store.ListInvoices(r.Context(), store.InvoiceFilter{ClientID: c.ID, Limit: 100})
	recurring, _ := s.store.ListRecurring(r.Context())
	var rec []store.RecurringInvoice
	for _, rr := range recurring {
		if rr.ClientID == c.ID {
			rec = append(rec, rr)
		}
	}
	unbilled, _ := s.store.ListTimeEntries(r.Context(), store.TimeFilter{ClientID: c.ID, Unbilled: true})
	var unbilledMinutes int
	for _, e := range unbilled {
		unbilledMinutes += e.DurationMinutes
	}
	writeJSON(w, http.StatusOK, map[string]any{"client": c, "invoices": invoices, "recurring": rec, "unbilled_minutes": unbilledMinutes, "unbilled_entries": len(unbilled)})
}

func (s *Server) readClient(r *http.Request, c *store.Client) error {
	if err := decode(r, c); err != nil {
		return err
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	if c.BillingMode == "" {
		c.BillingMode = "hourly"
	}
	return nil
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var c store.Client
	if err := s.readClient(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if c.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	st, _ := s.store.GetSettings(r.Context())
	if c.Currency == "" {
		c.Currency = st.BaseCurrency
	}
	if c.PaymentTermsDays == 0 {
		c.PaymentTermsDays = st.DefaultDueDays
	}
	s.fillCorrespondentName(r.Context(), &c)
	if err := s.store.CreateClient(r.Context(), &c); err != nil {
		s.fail(w, err, "create client")
		return
	}
	s.store.LogActivity(r.Context(), "client", c.ID, "created", "Client "+c.Name+" created")
	s.hooks.Emit("client.created", c)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	existing, err := s.store.GetClient(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get client")
		return
	}
	c := *existing
	if err := s.readClient(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	c.ID = existing.ID
	if c.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	s.fillCorrespondentName(r.Context(), &c)
	if err := s.store.UpdateClient(r.Context(), &c); err != nil {
		s.fail(w, err, "update client")
		return
	}
	s.hooks.Emit("client.updated", c)
	updated, _ := s.store.GetClient(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	id := idParam(r, "id")
	deleted, err := s.store.DeleteClient(r.Context(), id)
	if err != nil {
		s.fail(w, err, "delete client")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted, "archived": !deleted})
}

// fillCorrespondentName caches the Paperless correspondent name on the client record.
func (s *Server) fillCorrespondentName(ctx context.Context, c *store.Client) {
	if c.PaperlessCorrespondentID <= 0 {
		c.PaperlessCorrespondentID, c.PaperlessCorrespondent = 0, ""
		return
	}
	if cl, _ := s.paperlessClient(ctx); cl != nil {
		if n := cl.Name(ctx, "correspondents", c.PaperlessCorrespondentID); n != "" {
			c.PaperlessCorrespondent = n
		}
	}
}
