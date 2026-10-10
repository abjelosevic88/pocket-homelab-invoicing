// Package server wires the HTTP API, auth and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/config"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/docx"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/paperless"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/webhook"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	pdf      pdf.Engine
	docx     docx.Converter
	rates    currency.Provider // default from config; Settings can override, see provider()
	provMu   sync.Mutex
	provName string
	prov     currency.Provider
	hooks    *webhook.Dispatcher
	plMu     sync.Mutex
	plKey    string
	pl       *paperless.Client
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64
	pdfCount atomic.Int64
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	return &Server{
		cfg:     cfg,
		store:   st,
		log:     log,
		pdf:     pdf.NewEngine(cfg.PDFEngine, cfg.ChromiumPath, cfg.GotenbergURL),
		docx:    docx.NewConverter(cfg.DocxConverter, cfg.GotenbergURL, cfg.LibreOfficePath),
		rates:   currency.New(cfg.ExchangeRateProvider),
		hooks:   webhook.New(st, log),
		webFS:   webFS,
		started: time.Now(),
	}
}

// provider returns the exchange rate provider chosen in Settings, falling back to the
// EXCHANGE_RATE_PROVIDER environment default.
func (s *Server) provider(ctx context.Context) currency.Provider {
	name := ""
	if st, err := s.store.GetSettings(ctx); err == nil {
		name = strings.ToLower(strings.TrimSpace(st.ExchangeRateProvider))
	}
	if name == "" {
		return s.rates
	}
	s.provMu.Lock()
	defer s.provMu.Unlock()
	if s.prov == nil || s.provName != name {
		s.prov, s.provName = currency.New(name), name
	}
	return s.prov
}

// Store exposes the store (used by the scheduler).
func (s *Server) Store() *store.Store { return s.store }

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(120 * time.Second))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	// Public (unauthenticated) invoice views
	r.Get("/i/{token}", s.handlePublicInvoice)
	r.Get("/i/{token}/pdf", s.handlePublicInvoicePDF)
	r.Get("/i/{token}/files/{aid}", s.handlePublicAttachment)
	r.Get("/api/public/invoices/{token}", s.handlePublicInvoiceJSON)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/activity", s.handleActivity)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Post("/settings/logo", s.handleUploadLogo)
			r.Delete("/settings/logo", s.handleDeleteLogo)
			r.Post("/settings/test-email", s.handleTestEmail)
			r.Post("/settings/test-paperless", s.handleTestPaperless)
			r.Get("/settings/system", s.handleSystemInfo)

			r.Get("/currencies", s.handleListCurrencies)
			r.Put("/currencies/{code}", s.handleUpdateCurrency)
			r.Get("/rates", s.handleListRates)
			r.Put("/rates", s.handlePutRate)
			r.Delete("/rates/{quote}", s.handleDeleteRate)
			r.Post("/rates/refresh", s.handleRefreshRates)
			r.Get("/rates/convert", s.handleConvert)

			r.Get("/tax-rates", s.handleListTaxRates)
			r.Post("/tax-rates", s.handleSaveTaxRate)
			r.Put("/tax-rates/{id}", s.handleSaveTaxRate)
			r.Delete("/tax-rates/{id}", s.handleDeleteTaxRate)

			r.Get("/products", s.handleListProducts)
			r.Post("/products", s.handleSaveProduct)
			r.Put("/products/{id}", s.handleSaveProduct)
			r.Delete("/products/{id}", s.handleDeleteProduct)

			r.Get("/templates", s.handleListTemplates)
			r.Get("/templates/default-html", s.handleDefaultTemplateHTML)
			r.Get("/templates/placeholders", s.handlePlaceholders)
			r.Post("/templates/docx", s.handleUploadDocxTemplate)
			r.Post("/templates/{id}/docx", s.handleUploadDocxTemplate)
			r.Get("/templates/{id}/docx", s.handleDownloadDocxTemplate)
			r.Post("/templates", s.handleSaveTemplate)
			r.Put("/templates/{id}", s.handleSaveTemplate)
			r.Delete("/templates/{id}", s.handleDeleteTemplate)
			r.Get("/templates/{id}/preview", s.handlePreviewTemplate)
			r.Get("/templates/{id}/preview.pdf", s.handlePreviewTemplatePDF)

			r.Get("/clients", s.handleListClients)
			r.Post("/clients", s.handleCreateClient)
			r.Get("/clients/{id}", s.handleGetClient)
			r.Put("/clients/{id}", s.handleUpdateClient)
			r.Delete("/clients/{id}", s.handleDeleteClient)

			r.Get("/invoices", s.handleListInvoices)
			r.Post("/invoices", s.handleCreateInvoice)
			r.Get("/invoices/next-number", s.handleNextNumber)
			r.Get("/invoices/check-number", s.handleCheckNumber)
			r.Post("/invoices/from-time", s.handleInvoiceFromTime)
			r.Get("/invoices/{id}", s.handleGetInvoice)
			r.Put("/invoices/{id}", s.handleUpdateInvoice)
			r.Delete("/invoices/{id}", s.handleDeleteInvoice)
			r.Post("/invoices/{id}/status", s.handleInvoiceStatus)
			r.Post("/invoices/{id}/send", s.handleSendInvoice)
			r.Post("/invoices/{id}/duplicate", s.handleDuplicateInvoice)
			r.Get("/invoices/{id}/pdf", s.handleInvoicePDF)
			r.Get("/invoices/{id}/html", s.handleInvoiceHTML)
			r.Get("/invoices/{id}/docx", s.handleInvoiceDocx)
			r.Get("/invoices/{id}/emails", s.handleInvoiceEmails)
			r.Get("/invoices/{id}/attachments", s.handleListAttachments)
			r.Post("/invoices/{id}/attachments", s.handleUploadAttachment)
			r.Get("/attachments/{aid}", s.handleDownloadAttachment)
			r.Delete("/attachments/{aid}", s.handleDeleteAttachment)
			r.Post("/attachments/{aid}/paperless", s.handleAttachmentToPaperless)
			r.Post("/invoices/{id}/paperless", s.handleInvoiceToPaperless)

			r.Get("/documents", s.handleListDocuments)
			r.Post("/documents", s.handleUploadDocuments)
			r.Get("/documents/categories", s.handleDocumentCategories)
			r.Get("/documents/{id}", s.handleGetDocument)
			r.Put("/documents/{id}", s.handleUpdateDocument)
			r.Delete("/documents/{id}", s.handleDeleteDocument)
			r.Get("/documents/{id}/file", s.handleDocumentFile)
			r.Post("/documents/{id}/file", s.handleReplaceDocumentFile)
			r.Post("/documents/{id}/paperless", s.handleDocumentToPaperless)

			r.Get("/paperless/status", s.handlePaperlessStatus)
			r.Get("/paperless/documents", s.handlePaperlessSearch)
			r.Get("/paperless/names", s.handlePaperlessNames)
			r.Get("/paperless/correspondents", s.handlePaperlessCorrespondents)
			r.Post("/paperless/correspondents", s.handleCreateCorrespondent)
			r.Post("/paperless/correspondents/sync", s.handleSyncCorrespondents)
			r.Get("/paperless/documents/{pid}/file", s.handlePaperlessFile)
			r.Get("/paperless/documents/{pid}/thumb", s.handlePaperlessFile)
			r.Post("/paperless/documents/{pid}/import", s.handlePaperlessImport)
			r.Delete("/paperless/links/{id}", s.handleUnlinkPaperless)

			r.Get("/payments", s.handleListPayments)
			r.Post("/payments", s.handleCreatePayment)
			r.Delete("/payments/{id}", s.handleDeletePayment)

			r.Get("/recurring", s.handleListRecurring)
			r.Post("/recurring", s.handleSaveRecurring)
			r.Get("/recurring/{id}", s.handleGetRecurring)
			r.Put("/recurring/{id}", s.handleSaveRecurring)
			r.Delete("/recurring/{id}", s.handleDeleteRecurring)
			r.Post("/recurring/{id}/run", s.handleRunRecurring)

			r.Get("/time", s.handleListTime)
			r.Post("/time", s.handleSaveTime)
			r.Get("/time/running", s.handleRunningTimer)
			r.Post("/time/start", s.handleStartTimer)
			r.Post("/time/stop", s.handleStopTimer)
			r.Put("/time/{id}", s.handleSaveTime)
			r.Delete("/time/{id}", s.handleDeleteTime)

			r.Get("/expenses", s.handleListExpenses)
			r.Post("/expenses", s.handleSaveExpense)
			r.Put("/expenses/{id}", s.handleSaveExpense)
			r.Delete("/expenses/{id}", s.handleDeleteExpense)

			r.Get("/reports/revenue", s.handleReportRevenue)
			r.Get("/reports/clients", s.handleReportClients)
			r.Get("/reports/aging", s.handleReportAging)
			r.Get("/reports/tax", s.handleReportTax)
			r.Get("/reports/currencies", s.handleReportCurrencies)
			r.Get("/reports/time", s.handleReportTime)
			r.Get("/reports/income-tax", s.handleReportIncomeTax)
			r.Get("/reports/years", s.handleReportYears)
			r.Get("/reports/export.csv", s.handleExportCSV)
			r.Get("/reports/accountant-package.zip", s.handleAccountantPackage)

			r.Get("/calendar/working-days", s.handleWorkingDays)
			r.Get("/calendar/presets", s.handleHolidayPresets)
			r.Get("/month-end", s.handleMonthEndPlan)
			r.Post("/month-end", s.handleMonthEndCreate)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/webhooks", s.handleListWebhooks)
			r.Post("/webhooks", s.handleSaveWebhook)
			r.Put("/webhooks/{id}", s.handleSaveWebhook)
			r.Delete("/webhooks/{id}", s.handleDeleteWebhook)
			r.Post("/webhooks/test", s.handleTestWebhook)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/scheduler/run", s.handleRunScheduler)
		})
	})

	r.Get("/uploads/*", s.handleUploads)
	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func qInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func qInt64(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return n
}

func dateRange(r *http.Request) (string, string) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	now := time.Now()
	if from == "" {
		from = now.AddDate(-1, 0, 0).Format("2006-01-02")
	}
	if to == "" {
		to = now.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return from, to
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, _ := s.store.Dashboard(ctx)
	var invoices, clients, payments int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM invoices`).Scan(&invoices)
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM clients`).Scan(&clients)
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM payments`).Scan(&payments)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_invoicing_info Build info.\n# TYPE pocket_invoicing_info gauge\npocket_invoicing_info{version=%q,pdf_engine=%q} 1\n", Version, s.pdf.Name())
	fmt.Fprintf(w, "# HELP pocket_invoicing_uptime_seconds Process uptime.\n# TYPE pocket_invoicing_uptime_seconds gauge\npocket_invoicing_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_invoicing_http_requests_total HTTP requests served.\n# TYPE pocket_invoicing_http_requests_total counter\npocket_invoicing_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_invoicing_http_errors_total HTTP 5xx responses.\n# TYPE pocket_invoicing_http_errors_total counter\npocket_invoicing_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_invoicing_pdf_renders_total PDFs rendered.\n# TYPE pocket_invoicing_pdf_renders_total counter\npocket_invoicing_pdf_renders_total %d\n", s.pdfCount.Load())
	fmt.Fprintf(w, "# HELP pocket_invoicing_invoices_total Invoices in database.\n# TYPE pocket_invoicing_invoices_total gauge\npocket_invoicing_invoices_total %d\n", invoices)
	fmt.Fprintf(w, "# HELP pocket_invoicing_clients_total Clients in database.\n# TYPE pocket_invoicing_clients_total gauge\npocket_invoicing_clients_total %d\n", clients)
	fmt.Fprintf(w, "# HELP pocket_invoicing_payments_total Payments in database.\n# TYPE pocket_invoicing_payments_total gauge\npocket_invoicing_payments_total %d\n", payments)
	fmt.Fprintf(w, "# HELP pocket_invoicing_outstanding_base Outstanding balance in base currency.\n# TYPE pocket_invoicing_outstanding_base gauge\npocket_invoicing_outstanding_base %.2f\n", stats.Outstanding)
	fmt.Fprintf(w, "# HELP pocket_invoicing_overdue_base Overdue balance in base currency.\n# TYPE pocket_invoicing_overdue_base gauge\npocket_invoicing_overdue_base %.2f\n", stats.Overdue)
	fmt.Fprintf(w, "# HELP pocket_invoicing_overdue_count Number of overdue invoices.\n# TYPE pocket_invoicing_overdue_count gauge\npocket_invoicing_overdue_count %d\n", stats.OverdueCount)
	fmt.Fprintf(w, "# HELP pocket_invoicing_unbilled_minutes Unbilled tracked minutes.\n# TYPE pocket_invoicing_unbilled_minutes gauge\npocket_invoicing_unbilled_minutes %d\n", stats.UnbilledMinutes)
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/uploads/"))
	p := filepath.Join(s.cfg.DataDir, "uploads", name)
	if _, err := os.Stat(p); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, p)
}

// readLogo returns the logo bytes and image type if set.
func (s *Server) readLogo(ctx context.Context, st store.Settings) ([]byte, string, string) {
	if st.LogoPath == "" {
		return nil, "", ""
	}
	b, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "uploads", filepath.Base(st.LogoPath)))
	if err != nil {
		return nil, "", ""
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(st.LogoPath), "."))
	mime := "image/png"
	typ := "PNG"
	switch ext {
	case "jpg", "jpeg":
		mime, typ = "image/jpeg", "JPG"
	case "gif":
		mime, typ = "image/gif", "GIF"
	case "svg":
		mime, typ = "image/svg+xml", ""
	}
	return b, typ, mime
}
