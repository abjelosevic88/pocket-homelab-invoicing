package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// ---- webhooks ----

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListWebhooks(r.Context(), false)
	if err != nil {
		s.fail(w, err, "list webhooks")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveWebhook(w http.ResponseWriter, r *http.Request) {
	var h store.Webhook
	if id := idParam(r, "id"); id > 0 {
		h.ID = id
		h.Enabled = true
	} else {
		h.Enabled = true
	}
	if err := decode(r, &h); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	h.ID = idParam(r, "id")
	if !strings.HasPrefix(h.URL, "http://") && !strings.HasPrefix(h.URL, "https://") {
		writeErr(w, http.StatusBadRequest, "url must start with http:// or https://")
		return
	}
	if strings.TrimSpace(h.Events) == "" {
		h.Events = "*"
	}
	if err := s.store.SaveWebhook(r.Context(), &h); err != nil {
		s.fail(w, err, "save webhook")
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWebhook(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete webhook")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	s.hooks.Emit("test", map[string]any{"message": "Hello from Pocket Invoicing", "sent_at": time.Now().UTC().Format(time.RFC3339)})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- public invoice views ----

func (s *Server) publicInvoice(w http.ResponseWriter, r *http.Request) *store.Invoice {
	token := r.URL.Path[strings.Index(r.URL.Path, "/i/")+3:]
	token = strings.TrimPrefix(token, "nvoices/")
	if i := strings.Index(token, "/"); i >= 0 {
		token = token[:i]
	}
	token = strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/public/invoices/"), "/i/")
	if i := strings.Index(token, "/"); i >= 0 {
		token = token[:i]
	}
	inv, err := s.store.GetInvoiceByToken(r.Context(), token)
	if err != nil || inv.Status == store.StatusDraft {
		http.NotFound(w, r)
		return nil
	}
	if inv.ViewedAt == nil {
		now := store.Now()
		status := inv.Status
		if status == store.StatusSent {
			status = store.StatusViewed
		}
		_ = s.store.UpdateInvoiceStatus(r.Context(), inv.ID, status, nil, &now, nil)
		s.store.LogActivity(r.Context(), "invoice", inv.ID, "viewed", "Opened via public link")
		inv.ViewedAt = &now
		inv.Status = status
		s.hooks.Emit("invoice.viewed", inv)
	}
	return inv
}

func (s *Server) handlePublicInvoice(w http.ResponseWriter, r *http.Request) {
	inv := s.publicInvoice(w, r)
	if inv == nil {
		return
	}
	doc, tpl, _, _, err := s.buildDocument(r.Context(), inv, inv.TemplateID)
	if err != nil {
		s.fail(w, err, "build")
		return
	}
	htmlBytes, err := s.renderPublicHTML(doc, tpl.HTML, inv)
	if err != nil {
		s.fail(w, err, "render")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	_, _ = w.Write(htmlBytes)
}

func (s *Server) handlePublicInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv := s.publicInvoice(w, r)
	if inv == nil {
		return
	}
	s.servePDF(w, r, inv)
}

func (s *Server) handlePublicInvoiceJSON(w http.ResponseWriter, r *http.Request) {
	inv := s.publicInvoice(w, r)
	if inv == nil {
		return
	}
	inv.Payments = nil
	writeJSON(w, http.StatusOK, inv)
}

// ---- backup / export ----

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(s.cfg.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fail(w, err, "mkdir backups")
		return
	}
	name := fmt.Sprintf("pocket-invoicing-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	defer os.Remove(dst)
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	http.ServeFile(w, r, dst)
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx)
	st.SMTPPassword = ""
	clients, _ := s.store.ListClients(ctx, true, "")
	invoices, _, _ := s.store.ListInvoices(ctx, store.InvoiceFilter{})
	full := make([]store.Invoice, 0, len(invoices))
	for _, inv := range invoices {
		if f, err := s.store.GetInvoice(ctx, inv.ID); err == nil {
			full = append(full, *f)
		}
	}
	payments, _ := s.store.ListPayments(ctx, store.PaymentFilter{})
	recurring, _ := s.store.ListRecurring(ctx)
	timeEntries, _ := s.store.ListTimeEntries(ctx, store.TimeFilter{})
	expenses, _ := s.store.ListExpenses(ctx, store.ExpenseFilter{})
	products, _ := s.store.ListProducts(ctx, true)
	taxes, _ := s.store.ListTaxRates(ctx)
	templates, _ := s.store.ListTemplates(ctx)
	currencies, _ := s.store.ListCurrencies(ctx, false)
	rates, _ := s.store.ListRates(ctx, st.BaseCurrency)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-invoicing-export-%s.json"`, time.Now().Format("20060102")))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339), "version": Version,
		"settings": st, "clients": clients, "invoices": full, "payments": payments, "recurring": recurring,
		"time_entries": timeEntries, "expenses": expenses, "products": products, "tax_rates": taxes, "templates": templates,
		"currencies": currencies, "exchange_rates": rates,
	})
}
