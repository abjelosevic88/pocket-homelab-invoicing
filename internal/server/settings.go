package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/docx"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/mailer"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/money"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, maskSettings(st))
}

// maskSettings hides the SMTP password and the Paperless token: they are write-only, and a
// blank value on PUT keeps the stored one. `*_set` says whether one is stored.
func maskSettings(st store.Settings) map[string]any {
	b, _ := json.Marshal(st)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["smtp_password_set"] = st.SMTPPassword != ""
	m["smtp_password"] = ""
	m["paperless_token_set"] = st.PaperlessToken != ""
	m["paperless_token"] = ""
	return m
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	prevPwd := st.SMTPPassword
	prevToken := st.PaperlessToken
	prevBase := st.BaseCurrency
	if err := decode(r, &st); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if st.SMTPPassword == "" {
		st.SMTPPassword = prevPwd // password is never echoed back as cleared
	}
	if st.PaperlessToken == "" {
		st.PaperlessToken = prevToken
	}
	if st.PaperlessClearToken {
		st.PaperlessToken = ""
	}
	st.PaperlessClearToken = false
	st.PaperlessURL = strings.TrimRight(strings.TrimSpace(st.PaperlessURL), "/")
	st.PaperlessExternalURL = strings.TrimRight(strings.TrimSpace(st.PaperlessExternalURL), "/")
	switch st.PaperlessArchiveInvoices {
	case "off", "sent", "paid":
	default:
		st.PaperlessArchiveInvoices = "off"
	}
	st.BaseCurrency = strings.ToUpper(strings.TrimSpace(st.BaseCurrency))
	if st.BaseCurrency == "" {
		st.BaseCurrency = "EUR"
	}
	if st.InvoiceNumberFmt == "" {
		st.InvoiceNumberFmt = "INV-{YYYY}-{SEQ:4}"
	}
	if st.HoursPerDay <= 0 {
		st.HoursPerDay = 8
	}
	if st.DefaultDueDays < 0 {
		st.DefaultDueDays = 0 // 0 = due on receipt
	}
	if _, ok := money.Styles[st.NumberFormat]; !ok {
		st.NumberFormat = "1,234.56"
	}
	switch st.EmailAttachmentMode {
	case "generated", "uploaded", "both":
	default:
		st.EmailAttachmentMode = "generated"
	}
	cleaned := st.CustomFields[:0]
	for _, f := range st.CustomFields {
		f.Label = strings.TrimSpace(f.Label)
		if f.Label == "" {
			continue
		}
		if f.Key == "" {
			f.Key = slugify(f.Label)
		}
		cleaned = append(cleaned, f)
	}
	st.CustomFields = cleaned
	_ = s.store.SetCurrencyEnabled(ctx, st.BaseCurrency, true)
	if err := s.store.SaveSettings(ctx, st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if prevBase != "" && prevBase != st.BaseCurrency {
		// Stored rates are quoted against the base; rows for the old base would linger
		// invisibly and poison cross rates. Drop them and fetch fresh ones for the new base.
		n, err := s.store.PruneRates(ctx, st.BaseCurrency)
		s.log.Info("base currency changed, pruned exchange rates", "from", prevBase, "to", st.BaseCurrency, "removed", n, "err", err)
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if n, err := s.refreshRates(bg); err != nil {
				s.log.Warn("exchange rate refresh after base change failed", "err", err)
			} else {
				s.log.Info("exchange rates refreshed after base change", "updated", n)
			}
		}()
	}
	writeJSON(w, http.StatusOK, maskSettings(st))
}

func slugify(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_', r == '/':
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "field"
	}
	return out
}

func (s *Server) handleUploadLogo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}
	f, hdr, err := r.FormFile("logo")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing 'logo' file")
		return
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg":
	default:
		writeErr(w, http.StatusBadRequest, "logo must be PNG, JPG, GIF or SVG")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fail(w, err, "mkdir uploads")
		return
	}
	name := "logo" + ext
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		s.fail(w, err, "save logo")
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, f); err != nil {
		s.fail(w, err, "save logo")
		return
	}
	st, _ := s.store.GetSettings(ctx)
	if st.LogoPath != "" && st.LogoPath != name {
		_ = os.Remove(filepath.Join(dir, filepath.Base(st.LogoPath)))
	}
	st.LogoPath = name
	if err := s.store.SaveSettings(ctx, st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logo_path": name, "url": "/uploads/" + name + "?v=" + fmt.Sprint(time.Now().Unix())})
}

func (s *Server) handleDeleteLogo(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context())
	if st.LogoPath != "" {
		_ = os.Remove(filepath.Join(s.cfg.DataDir, "uploads", filepath.Base(st.LogoPath)))
	}
	st.LogoPath = ""
	if err := s.store.SaveSettings(r.Context(), st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context())
	var in struct {
		To string `json:"to"`
	}
	_ = decode(r, &in)
	if in.To == "" {
		in.To = userFrom(r.Context()).Email
	}
	cfg := mailer.Config{Host: st.SMTPHost, Port: st.SMTPPort, User: st.SMTPUser, Password: st.SMTPPassword, From: st.SMTPFrom, FromName: firstNonEmpty(st.SMTPFromName, st.CompanyName), TLS: st.SMTPTLS}
	err := mailer.Send(cfg, mailer.Message{To: []string{in.To}, Subject: "Pocket Invoicing test email", Body: "If you can read this, SMTP is configured correctly.\n\n— Pocket Invoicing"})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var dbSize int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		dbSize = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       Version,
		"go":            runtime.Version(),
		"os":            runtime.GOOS + "/" + runtime.GOARCH,
		"pdf_engine":    s.pdf.Name(),
		"auth_mode":     s.cfg.AuthMode,
		"rate_provider": s.rates.Name(),
		"data_dir":      s.cfg.DataDir,
		"db_path":       s.cfg.DBPath,
		"db_size_bytes": dbSize,
		"base_url":      s.cfg.BaseURL,
		"scheduler":     s.cfg.SchedulerEnabled,
		"uptime":        time.Since(s.started).Round(time.Second).String(),
		"metrics":       s.cfg.MetricsEnabled,
	})
}

// ---- currencies & rates ----

func (s *Server) handleListCurrencies(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListCurrencies(r.Context(), r.URL.Query().Get("enabled") == "1")
	if err != nil {
		s.fail(w, err, "list currencies")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUpdateCurrency(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], "")))
	var c store.Currency
	if err := decode(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	c.Code = code
	existing := s.store.GetCurrency(r.Context(), code)
	if c.Name == "" {
		c.Name = existing.Name
	}
	if c.Symbol == "" {
		c.Symbol = existing.Symbol
	}
	if c.Decimals == 0 && existing.Decimals != 0 && r.URL.Query().Get("decimals") == "" {
		c.Decimals = existing.Decimals
	}
	if err := s.store.UpsertCurrency(r.Context(), c); err != nil {
		s.fail(w, err, "update currency")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleListRates(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context())
	base := firstNonEmpty(r.URL.Query().Get("base"), st.BaseCurrency)
	list, err := s.store.ListRates(r.Context(), base)
	if err != nil {
		s.fail(w, err, "list rates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"base": base, "rates": list, "provider": s.provider(r.Context()).Name(), "default_provider": s.rates.Name(), "providers": currency.Names})
}

func (s *Server) handlePutRate(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context())
	var in store.ExchangeRate
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Base = strings.ToUpper(firstNonEmpty(in.Base, st.BaseCurrency))
	in.Quote = strings.ToUpper(in.Quote)
	if in.Quote == "" || in.Rate <= 0 {
		writeErr(w, http.StatusBadRequest, "quote and a positive rate are required")
		return
	}
	in.Source = "manual"
	if err := s.store.UpsertRate(r.Context(), in); err != nil {
		s.fail(w, err, "save rate")
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteRate(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.GetSettings(r.Context())
	quote := strings.ToUpper(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
	base := strings.ToUpper(firstNonEmpty(r.URL.Query().Get("base"), st.BaseCurrency))
	if err := s.store.DeleteRate(r.Context(), base, quote); err != nil {
		s.fail(w, err, "delete rate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// RefreshRates pulls rates for all enabled currencies against the base.
func (s *Server) RefreshRates(ctx context.Context) (int, error) { return s.refreshRates(ctx) }

func (s *Server) handleRefreshRates(w http.ResponseWriter, r *http.Request) {
	n, err := s.refreshRates(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	from := strings.ToUpper(r.URL.Query().Get("from"))
	to := strings.ToUpper(r.URL.Query().Get("to"))
	var amount float64
	fmt.Sscanf(r.URL.Query().Get("amount"), "%g", &amount)
	// With a date, providers that publish daily lists (CBBH) return the official rate of that day.
	rate, source, ok := s.rateOn(r.Context(), r.URL.Query().Get("date"), from, to)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("no rate for %s→%s (add one under Settings → Currencies or refresh rates)", from, to))
		return
	}
	cur := s.store.GetCurrency(r.Context(), to)
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "rate": rate, "source": source, "amount": amount, "converted": money.Convert(amount, rate, cur.Decimals)})
}

// ---- tax rates ----

func (s *Server) handleListTaxRates(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTaxRates(r.Context())
	if err != nil {
		s.fail(w, err, "list tax rates")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveTaxRate(w http.ResponseWriter, r *http.Request) {
	var t store.TaxRate
	if err := decode(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t.ID = idParam(r, "id")
	if t.Name == "" {
		t.Name = fmt.Sprintf("%g%%", t.Rate)
	}
	if err := s.store.SaveTaxRate(r.Context(), &t); err != nil {
		s.fail(w, err, "save tax rate")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTaxRate(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTaxRate(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete tax rate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- products ----

func (s *Server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProducts(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		s.fail(w, err, "list products")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveProduct(w http.ResponseWriter, r *http.Request) {
	var p store.Product
	if err := decode(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p.ID = idParam(r, "id")
	if strings.TrimSpace(p.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.Unit == "" {
		p.Unit = "unit"
	}
	var err error
	if p.ID == 0 {
		err = s.store.CreateProduct(r.Context(), &p)
	} else {
		err = s.store.UpdateProduct(r.Context(), &p)
	}
	if err != nil {
		s.fail(w, err, "save product")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProduct(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete product")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- templates ----

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTemplates(r.Context())
	if err != nil {
		s.fail(w, err, "list templates")
		return
	}
	for i := range list {
		decorateTemplate(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDefaultTemplateHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(pdf.DefaultTemplateHTML()))
}

func (s *Server) docxTemplatePath(t *store.InvoiceTemplate) string {
	return filepath.Join(s.cfg.DataDir, "templates", filepath.Base(t.DocxPath))
}

func (s *Server) handlePlaceholders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pdf.DataKeys)
}

// handleUploadDocxTemplate creates (POST /templates/docx) or replaces (POST /templates/{id}/docx)
// a Word template. Multipart fields: file (.docx), name (optional), is_default (optional).
func (s *Server) handleUploadDocxTemplate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid upload")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing 'file' (.docx)")
		return
	}
	defer f.Close()
	if !strings.EqualFold(filepath.Ext(hdr.Filename), ".docx") {
		writeErr(w, http.StatusBadRequest, "only .docx files are supported (save .doc/.odt as Word Document first)")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 20<<20))
	if err != nil {
		s.fail(w, err, "read upload")
		return
	}
	placeholders, err := docx.Placeholders(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// smoke-test the template against sample data so broken loops surface now
	if _, err := docx.Fill(data, pdf.Build(sampleBuildInput()).Data); err != nil {
		writeErr(w, http.StatusBadRequest, "template cannot be filled: "+err.Error())
		return
	}
	var t *store.InvoiceTemplate
	if id := idParam(r, "id"); id > 0 {
		t, err = s.store.GetTemplate(ctx, id)
		if err != nil {
			s.fail(w, err, "get template")
			return
		}
		if t.DocxPath != "" {
			_ = os.Remove(s.docxTemplatePath(t))
		}
	} else {
		t = &store.InvoiceTemplate{Name: strings.TrimSuffix(hdr.Filename, filepath.Ext(hdr.Filename)), Layout: "classic", AccentColor: "#2563eb", Labels: map[string]string{}}
	}
	if n := strings.TrimSpace(r.FormValue("name")); n != "" {
		t.Name = n
	}
	if v := r.FormValue("is_default"); v == "1" || v == "true" {
		t.IsDefault = true
	}
	dir := filepath.Join(s.cfg.DataDir, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fail(w, err, "mkdir templates")
		return
	}
	stored := store.RandomToken(8) + "__" + safeFilename(filepath.Base(hdr.Filename))
	if err := os.WriteFile(filepath.Join(dir, stored), data, 0o644); err != nil {
		s.fail(w, err, "save template")
		return
	}
	t.Kind = "docx"
	t.DocxPath = stored
	if err := s.store.SaveTemplate(ctx, t); err != nil {
		s.fail(w, err, "save template")
		return
	}
	saved, _ := s.store.GetTemplate(ctx, t.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"template": decorateTemplate(saved), "placeholders": placeholders})
}

func decorateTemplate(t *store.InvoiceTemplate) *store.InvoiceTemplate {
	if t != nil && t.DocxPath != "" {
		if i := strings.Index(t.DocxPath, "__"); i >= 0 {
			t.DocxName = t.DocxPath[i+2:]
		} else {
			t.DocxName = t.DocxPath
		}
	}
	return t
}

func (s *Server) handleDownloadDocxTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTemplate(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get template")
		return
	}
	if t.DocxPath == "" {
		writeErr(w, http.StatusNotFound, "no Word file on this template")
		return
	}
	decorateTemplate(t)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, t.DocxName))
	http.ServeFile(w, r, s.docxTemplatePath(t))
}

func (s *Server) handleSaveTemplate(w http.ResponseWriter, r *http.Request) {
	var t store.InvoiceTemplate
	if err := decode(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t.ID = idParam(r, "id")
	if t.ID > 0 {
		if existing, err := s.store.GetTemplate(r.Context(), t.ID); err == nil {
			t.Kind, t.DocxPath = existing.Kind, existing.DocxPath // file is managed by the upload endpoint
		}
	}
	if strings.TrimSpace(t.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if t.Layout == "" {
		t.Layout = "classic"
	}
	if t.AccentColor == "" {
		t.AccentColor = "#2563eb"
	}
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	if t.HTML != "" {
		// validate template compiles
		if _, err := pdf.RenderHTML(pdf.Build(sampleBuildInput()), t.HTML); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.store.SaveTemplate(r.Context(), &t); err != nil {
		s.fail(w, err, "save template")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if t, err := s.store.GetTemplate(r.Context(), idParam(r, "id")); err == nil && t.DocxPath != "" && !t.IsDefault {
		_ = os.Remove(s.docxTemplatePath(t))
	}
	if err := s.store.DeleteTemplate(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// sampleBuildInput returns a demo invoice used for template previews.
func sampleBuildInput() pdf.BuildInput {
	ps, pe := "2026-09-01", "2026-09-30"
	return pdf.BuildInput{
		Invoice: &store.Invoice{
			Number: "INV-2026-0042", Status: "sent", IssueDate: "2026-10-01", DueDate: "2026-10-15", Currency: "EUR", ExchangeRate: 1,
			BillingMode: "hourly", PeriodStart: &ps, PeriodEnd: &pe, DiscountType: "none",
			Subtotal: 2950, TaxTotal: 590, Total: 3540, Notes: "Thank you for the opportunity to work on this project.", Terms: "Payment is due within 14 days.",
			Items: []store.InvoiceItem{
				{Description: "Infrastructure consulting", Unit: "hour", Quantity: 20, UnitPrice: 95, TaxRate: 20, LineTotal: 1900},
				{Description: "On-site workshop", Unit: "day", Quantity: 1, UnitPrice: 850, TaxRate: 20, LineTotal: 850},
				{Description: "Monitoring stack hosting (September)", Unit: "month", Quantity: 1, UnitPrice: 200, TaxRate: 20, LineTotal: 200},
			},
		},
		Client:       &store.Client{Name: "Acme Corporation", ContactName: "Jane Doe", Email: "accounts@acme.example", Address1: "42 Example Street", City: "Berlin", PostalCode: "10115", Country: "Germany", TaxID: "DE123456789"},
		Currency:     store.Currency{Code: "EUR", Symbol: "€", Decimals: 2},
		BaseCurrency: store.Currency{Code: "EUR", Symbol: "€", Decimals: 2},
	}
}

func (s *Server) previewDoc(r *http.Request) (*pdf.Document, *store.InvoiceTemplate, []byte, string, error) {
	ctx := r.Context()
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, nil, nil, "", err
	}
	tpl, err := s.store.GetTemplate(ctx, idParam(r, "id"))
	if err != nil {
		return nil, nil, nil, "", err
	}
	// Allow live overrides from the editor via query params.
	q := r.URL.Query()
	if v := q.Get("layout"); v != "" {
		tpl.Layout = v
	}
	if v := q.Get("accent"); v != "" {
		tpl.AccentColor = v
	}
	in := sampleBuildInput()
	if id := qInt64(r, "invoice_id"); id > 0 {
		if inv, err := s.store.GetInvoice(ctx, id); err == nil {
			in.Invoice = inv
			in.Client, _ = s.store.GetClient(ctx, inv.ClientID)
			in.Currency = s.store.GetCurrency(ctx, inv.Currency)
		}
	}
	in.Settings = st
	in.Template = tpl
	in.BaseCurrency = s.store.GetCurrency(ctx, st.BaseCurrency)
	logo, logoType, mime := s.readLogo(ctx, st)
	if len(logo) > 0 {
		in.LogoDataURL = "data:" + mime + ";base64," + base64Encode(logo)
	}
	return pdf.Build(in), tpl, logo, logoType, nil
}

func (s *Server) handlePreviewTemplate(w http.ResponseWriter, r *http.Request) {
	doc, tpl, _, _, err := s.previewDoc(r)
	if err != nil {
		s.fail(w, err, "preview")
		return
	}
	b, err := pdf.RenderHTML(doc, tpl.HTML)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) handlePreviewTemplatePDF(w http.ResponseWriter, r *http.Request) {
	doc, tpl, logo, logoType, err := s.previewDoc(r)
	if err != nil {
		s.fail(w, err, "preview")
		return
	}
	var b []byte
	if tpl.Kind == "docx" {
		filled, ferr := s.fillDocx(tpl, doc)
		if ferr != nil {
			writeErr(w, http.StatusBadRequest, ferr.Error())
			return
		}
		b, err = s.docx.Convert(r.Context(), filled)
	} else {
		b, err = s.pdf.Render(r.Context(), doc, tpl.HTML, logo, logoType)
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="preview.pdf"`)
	_, _ = w.Write(b)
}
