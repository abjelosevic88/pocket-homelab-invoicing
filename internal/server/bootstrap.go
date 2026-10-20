package server

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pdf"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/store"
)

// Bootstrap seeds currencies, each user's default templates and (optionally) an admin user / demo data.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	st, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		return err
	}
	if err := s.store.SeedCurrencies(ctx, st.BaseCurrency); err != nil {
		return err
	}
	if s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if _, err := s.store.GetUserByEmail(ctx, s.cfg.AdminEmail); err != nil {
			h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
			u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: roleAdmin}
			if err := s.store.CreateUser(ctx, u); err != nil {
				return err
			}
			st.SetupComplete = true
			_ = s.store.SaveGlobalSettings(ctx, st)
			s.log.Info("admin user created from ADMIN_EMAIL/ADMIN_PASSWORD", "email", u.Email)
		}
	}
	// Every account has its own templates; new accounts get theirs lazily (handleListTemplates).
	if users, err := s.store.ListUsers(ctx); err == nil {
		for _, u := range users {
			if err := s.store.EnsureDefaultTemplates(ctx, u.ID); err != nil {
				return err
			}
		}
	}
	if s.cfg.PaperlessEnvSet {
		s.log.Warn("PAPERLESS_URL / PAPERLESS_TOKEN are set but ignored: Pocket Invoicing files invoices in Pocket Documents now (Settings → Pocket Documents); remove them from the environment")
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
	}
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// seedDemo creates sample data for the owner account when DEMO_DATA=true and the database
// is empty. Without any account yet it waits for the next start.
func (s *Server) seedDemo(ctx context.Context) error {
	var n int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM clients`).Scan(&n); err != nil || n > 0 {
		return err
	}
	uid := s.store.OwnerID(ctx)
	if uid == 0 {
		s.log.Info("DEMO_DATA: no account yet; demo data is seeded for the owner on the next start")
		return nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	st.CompanyName = "Homelab Consulting"
	st.CompanyEmail = "billing@homelab.example"
	st.Address1 = "1 Rack Unit Way"
	st.City = "Berlin"
	st.PostalCode = "10115"
	st.Country = "Germany"
	st.TaxID = "DE123456789"
	st.PaymentDetails = "Bank: Example Bank\nIBAN: DE00 1234 5678 9012 3456 78\nBIC: EXAMPLEX"
	st.DefaultHourlyRate = 90
	st.DefaultDailyRate = 650
	st.DefaultTaxRate = 19
	if err := s.store.SaveSettings(ctx, uid, st); err != nil {
		return err
	}
	_ = s.store.SaveTaxRate(ctx, uid, &store.TaxRate{Name: "VAT 19%", Rate: 19, IsDefault: true})
	_ = s.store.SaveTaxRate(ctx, uid, &store.TaxRate{Name: "Reduced 7%", Rate: 7})
	_ = s.store.SaveTaxRate(ctx, uid, &store.TaxRate{Name: "Reverse charge 0%", Rate: 0})
	for _, c := range []string{"USD", "GBP", "CHF", "RSD"} {
		_ = s.store.SetCurrencyEnabled(ctx, c, true)
	}
	_ = s.store.UpsertRate(ctx, store.ExchangeRate{Base: "EUR", Quote: "USD", Rate: 1.08, Source: "manual"})
	_ = s.store.UpsertRate(ctx, store.ExchangeRate{Base: "EUR", Quote: "GBP", Rate: 0.85, Source: "manual"})
	_ = s.store.UpsertRate(ctx, store.ExchangeRate{Base: "EUR", Quote: "CHF", Rate: 0.95, Source: "manual"})
	_ = s.store.UpsertRate(ctx, store.ExchangeRate{Base: "EUR", Quote: "RSD", Rate: 117.2, Source: "manual"})

	acme := &store.Client{Name: "Acme Corporation", ContactName: "Jane Doe", Email: "jane@acme.example", Address1: "42 Example Street", City: "Berlin", PostalCode: "10115", Country: "Germany", TaxID: "DE987654321", Currency: "EUR", BillingMode: "hourly", DefaultRate: 95, PaymentTermsDays: 14}
	globex := &store.Client{Name: "Globex Inc.", ContactName: "Hank Scorpio", Email: "ap@globex.example", Address1: "100 Volcano Lair", City: "Cypress Creek", State: "CA", PostalCode: "90001", Country: "USA", Currency: "USD", BillingMode: "daily", DefaultRate: 700, PaymentTermsDays: 30}
	initech := &store.Client{Name: "Initech Ltd", ContactName: "Bill Lumbergh", Email: "bill@initech.example", City: "London", Country: "United Kingdom", Currency: "GBP", BillingMode: "monthly", DefaultRate: 1500, PaymentTermsDays: 7}
	for _, c := range []*store.Client{acme, globex, initech} {
		if err := s.store.CreateClient(ctx, uid, c); err != nil {
			return err
		}
	}
	_ = s.store.CreateProduct(ctx, uid, &store.Product{Name: "Consulting (hourly)", Unit: "hour", UnitPrice: 95, TaxRate: 19})
	_ = s.store.CreateProduct(ctx, uid, &store.Product{Name: "On-site day", Unit: "day", UnitPrice: 700, TaxRate: 19})
	_ = s.store.CreateProduct(ctx, uid, &store.Product{Name: "Managed hosting retainer", Description: "Monitoring, patching and backups for the customer cluster", Unit: "month", UnitPrice: 1500, TaxRate: 19})
	_ = s.store.CreateProduct(ctx, uid, &store.Product{Name: "Domain renewal", Unit: "unit", UnitPrice: 14.99, TaxRate: 19})

	now := time.Now()
	mk := func(c *store.Client, daysAgo int, status string, items []store.InvoiceItem, paid float64) error {
		issue := now.AddDate(0, 0, -daysAgo)
		in := invoiceInput{ClientID: c.ID, IssueDate: issue.Format("2006-01-02"), DueDate: issue.AddDate(0, 0, c.PaymentTermsDays).Format("2006-01-02"), Items: items, Terms: "Payment due within " + fmt.Sprint(c.PaymentTermsDays) + " days.", Footer: st.DefaultFooter}
		inv := &store.Invoice{Status: status}
		if err := s.applyInput(ctx, uid, inv, in, st); err != nil {
			return err
		}
		var err error
		inv.Number, err = s.nextNumber(ctx, uid, &st, inv.IssueDate, c.Name)
		if err != nil {
			return err
		}
		if status != store.StatusDraft {
			sent := issue.UTC().Format(time.RFC3339)
			inv.SentAt = &sent
		}
		if err := s.store.CreateInvoice(ctx, uid, inv); err != nil {
			return err
		}
		if paid > 0 {
			p := &store.Payment{InvoiceID: inv.ID, Date: issue.AddDate(0, 0, 5).Format("2006-01-02"), Amount: paid, Currency: inv.Currency, ExchangeRate: 1, AppliedAmount: paid, Method: "bank_transfer", Reference: "Demo"}
			if err := s.store.CreatePayment(ctx, uid, p); err != nil {
				return err
			}
			if _, err := s.store.RecalcAmountPaid(ctx, uid, inv.ID); err != nil {
				return err
			}
		}
		return nil
	}
	for m := 5; m >= 1; m-- {
		if err := mk(acme, m*30+3, store.StatusPaid, []store.InvoiceItem{{Description: "Infrastructure consulting", Unit: "hour", Quantity: float64(20 + m*3), UnitPrice: 95, TaxRate: 19}}, float64(20+m*3)*95*1.19); err != nil {
			return err
		}
		if err := mk(initech, m*30+1, store.StatusPaid, []store.InvoiceItem{{Description: "Managed hosting retainer – {month}", Unit: "month", Quantity: 1, UnitPrice: 1500, TaxRate: 0}}, 1500); err != nil {
			return err
		}
	}
	if err := mk(globex, 40, store.StatusSent, []store.InvoiceItem{{Description: "On-site migration workshop", Unit: "day", Quantity: 3, UnitPrice: 700, TaxRate: 0}, {Description: "Travel expenses", Unit: "unit", Quantity: 1, UnitPrice: 420.5, TaxRate: 0}}, 0); err != nil {
		return err
	}
	if err := mk(acme, 10, store.StatusSent, []store.InvoiceItem{{Description: "Kubernetes upgrade", Unit: "hour", Quantity: 12.5, UnitPrice: 95, TaxRate: 19}, {Description: "Domain renewal", Unit: "unit", Quantity: 2, UnitPrice: 14.99, TaxRate: 19}}, 500); err != nil {
		return err
	}
	if err := mk(initech, 2, store.StatusDraft, []store.InvoiceItem{{Description: "Managed hosting retainer – " + now.Format("January 2006"), Unit: "month", Quantity: 1, UnitPrice: 1500, TaxRate: 0}}, 0); err != nil {
		return err
	}
	_, _ = s.store.MarkOverdue(ctx, uid)

	rec := &store.RecurringInvoice{Name: "Initech hosting retainer", ClientID: initech.ID, Status: "active", Frequency: "monthly", Interval: 1, StartDate: now.Format("2006-01-02"), NextRun: NextRun(now.Format("2006-01-02"), "monthly", 1), DueDays: 7, Currency: "GBP", BillingMode: "monthly", Items: []store.RecurringItem{{Description: "Managed hosting retainer – {month}", Unit: "month", Quantity: 1, UnitPrice: 1500}}, DiscountType: "none", Terms: "Payment due within 7 days."}
	if err := s.store.SaveRecurring(ctx, uid, rec); err != nil {
		return err
	}
	for d := 1; d <= 6; d++ {
		start := now.AddDate(0, 0, -d).Truncate(time.Hour).Add(-3 * time.Hour)
		end := start.Add(time.Duration(60+d*20) * time.Minute)
		es := end.UTC().Format(time.RFC3339)
		_ = s.store.SaveTimeEntry(ctx, uid, &store.TimeEntry{ClientID: acme.ID, Project: "Cluster ops", Description: fmt.Sprintf("Daily maintenance #%d", d), StartedAt: start.UTC().Format(time.RFC3339), EndedAt: &es, DurationMinutes: 60 + d*20, Billable: true})
	}
	_ = s.store.SaveExpense(ctx, uid, &store.Expense{Date: now.AddDate(0, 0, -3).Format("2006-01-02"), Category: "Software", Description: "Monitoring SaaS", Amount: 49, Currency: "EUR", ExchangeRate: 1, Billable: false})
	cid := acme.ID
	_ = s.store.SaveExpense(ctx, uid, &store.Expense{ClientID: &cid, Date: now.AddDate(0, 0, -1).Format("2006-01-02"), Category: "Hardware", Description: "Replacement SSD for client NAS", Amount: 129.9, Currency: "EUR", ExchangeRate: 1, Billable: true})
	s.store.LogActivity(ctx, uid, "system", 0, "demo", "Demo data seeded")
	s.log.Info("demo data seeded")
	_ = pdf.DefaultLabels
	return nil
}
