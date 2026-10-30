package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/calendar"
)

// Settings is the application-wide configuration stored in the DB.
type Settings struct {
	CompanyName    string `json:"company_name"`
	BrandName      string `json:"brand_name"` // short name shown in the app sidebar (defaults to company name)
	CompanyEmail   string `json:"company_email"`
	CompanyPhone   string `json:"company_phone"`
	CompanyWebsite string `json:"company_website"`
	Address1       string `json:"address1"`
	Address2       string `json:"address2"`
	City           string `json:"city"`
	State          string `json:"state"`
	PostalCode     string `json:"postal_code"`
	Country        string `json:"country"`
	TaxID          string `json:"tax_id"`
	VatID          string `json:"vat_id"` // VAT registration number, printed under the tax ID (e.g. PDV broj next to the JIB)
	LogoPath       string `json:"logo_path"`
	IssuePlace     string `json:"issue_place"` // place of issue printed next to the date (e.g. Banja Luka)

	BaseCurrency         string  `json:"base_currency"`
	ExchangeRateProvider string  `json:"exchange_rate_provider"` // frankfurter | cbbh | none; empty = EXCHANGE_RATE_PROVIDER env
	Locale               string  `json:"locale"`
	DateFormat           string  `json:"date_format"` // Go layout
	Timezone             string  `json:"timezone"`
	InvoiceNumberFmt     string  `json:"invoice_number_format"` // e.g. INV-{YYYY}-{SEQ:4}
	InvoiceNextSeq       int64   `json:"invoice_next_seq"`
	InvoiceSeqResetYr    bool    `json:"invoice_seq_reset_yearly"`
	InvoiceSeqYear       int     `json:"invoice_seq_year"`
	DefaultDueDays       int     `json:"default_due_days"`
	DefaultNotes         string  `json:"default_notes"`
	DefaultTerms         string  `json:"default_terms"`
	DefaultFooter        string  `json:"default_footer"`
	DefaultBilling       string  `json:"default_billing_mode"`
	DefaultHourlyRate    float64 `json:"default_hourly_rate"`
	DefaultDailyRate     float64 `json:"default_daily_rate"`
	DefaultMonthlyRate   float64 `json:"default_monthly_rate"`
	DefaultTaxRate       float64 `json:"default_tax_rate"`
	HoursPerDay          float64 `json:"hours_per_day"`
	TimeRoundingMin      int     `json:"time_rounding_minutes"`
	ShowTaxColumn        bool    `json:"show_tax_column"`
	AlwaysShowTax        bool    `json:"always_show_tax"` // print the tax line in the totals even at 0% (VAT rate and amount must appear on the invoice)

	DefaultPaymentMethod     string            `json:"default_payment_method"`      // method for recorded payments and "Mark fully paid" (clients can override)
	PaymentDetails           string            `json:"payment_details"`             // bank account, IBAN, PayPal etc. shown on invoice
	PaymentDetailsByCurrency map[string]string `json:"payment_details_by_currency"` // optional per-currency override (e.g. USD IBAN)
	NumberFormat             string            `json:"number_format"`               // "1,234.56" | "1.234,56" | "1 234,56" | "1'234.56"
	CustomFields             []CustomFieldDef  `json:"custom_fields"`               // user-defined invoice fields
	ShowBaseTotal            bool              `json:"show_base_total"`             // print the total converted to the base currency when currencies differ
	BaseTotalNote            string            `json:"base_total_note"`             // sentence printed under the totals; placeholders {rate} {currency} {currency_name} {base} {total_base}
	EmailAttachmentMode      string            `json:"email_attachment_mode"`       // generated | uploaded | both

	// Working-days calendar: feeds recurring profiles with quantity_mode = working_days and the month-end wizard.
	WorkWeek string             `json:"work_week"` // ISO weekdays that count as working days, e.g. "1,2,3,4,5"
	Holidays []calendar.Holiday `json:"holidays"`  // public holidays (yearly) and personal days off (one-off)

	// Income tax estimate (for the owner's own tax return; not printed on invoices)
	IncomeTaxRate          float64 `json:"income_tax_rate"`            // percent, e.g. 10
	IncomeTaxBasis         string  `json:"income_tax_basis"`           // revenue (gross received) | profit (received - expenses)
	IncomeTaxByPaymentDate bool    `json:"income_tax_by_payment_date"` // cash basis (payments) vs invoice date
	IncomeTaxMinYearly     float64 `json:"income_tax_min_yearly"`      // minimum yearly tax (e.g. 600 KM for "mali preduzetnik")
	IncomeTaxDeduction     float64 `json:"income_tax_deduction"`       // yearly allowance subtracted from the base
	ContributionsMonthly   float64 `json:"contributions_monthly"`      // fixed monthly contributions (doprinosi)
	IncomeTaxLabel         string  `json:"income_tax_label"`           // e.g. "Porez na dohodak (RS)"

	SMTPHost         string `json:"smtp_host"`
	SMTPPort         int    `json:"smtp_port"`
	SMTPUser         string `json:"smtp_user"`
	SMTPPassword     string `json:"smtp_password"`
	SMTPFrom         string `json:"smtp_from"`
	SMTPFromName     string `json:"smtp_from_name"`
	SMTPTLS          string `json:"smtp_tls"` // none | starttls | tls
	SMTPBCC          string `json:"smtp_bcc"`
	EmailSubject     string `json:"email_subject"`
	EmailBody        string `json:"email_body"`
	ReminderDays     string `json:"reminder_days"` // comma separated days after due date, e.g. "3,7,14"
	RemindersEnabled bool   `json:"reminders_enabled"`

	// Pocket Documents (optional): issued invoices and company documents are
	// filed there. Through a Pocket Core link (nothing to paste), a URL and a
	// pd_ API token of one's own, or off.
	DocumentsMode                 string `json:"documents_mode"`                  // core | direct | off; empty = direct with a token, else core when Pocket Core is there
	DocumentsURL                  string `json:"documents_url"`                   // direct: how this app reaches it, e.g. http://pocket-documents:8080 (empty = ask Core / DOCUMENTS_URL)
	DocumentsPublicURL            string `json:"documents_public_url"`            // direct: what a browser opens (defaults to DocumentsURL)
	DocumentsToken                string `json:"documents_token"`                 // direct: the person's pd_ API token (never cleared by an empty PUT)
	DocumentsClearToken           bool   `json:"documents_clear_token,omitempty"` // transient: PUT with true removes the stored token
	DocumentsTags                 string `json:"documents_tags"`                  // comma-separated tag names added to everything filed
	DocumentsInvoiceType          string `json:"documents_invoice_type"`          // document type for invoices (created if missing)
	DocumentsCreateCorrespondents bool   `json:"documents_create_correspondents"` // file under the client's name when the client has no correspondent (created if missing)
	DocumentsArchiveInvoices      string `json:"documents_archive_invoices"`      // off | sent | paid — file the invoice automatically
	DocumentsCategoryAsType       bool   `json:"documents_category_as_type"`      // use the company document's category as document type

	// Pocket Time (optional): the timer here is the person's one timer in
	// Pocket Time. Through a Pocket Core link (nothing to paste), a URL and a
	// ptm_ API token of one's own, or off (the timer works on its own here).
	TimeMode       string `json:"time_mode"`                  // core | direct | off; empty = direct with a token, else core when Pocket Core is there
	TimeURL        string `json:"time_url"`                   // direct: how this app reaches it (empty = ask Core)
	TimeToken      string `json:"time_token"`                 // direct: the person's ptm_ API token (never cleared by an empty PUT)
	TimeClearToken bool   `json:"time_clear_token,omitempty"` // transient: PUT with true removes the stored token

	SetupComplete bool `json:"setup_complete"`
}

// DefaultSettings returns the initial settings.
func DefaultSettings() Settings {
	return Settings{
		CompanyName:                   "My Company",
		BaseCurrency:                  "EUR",
		Locale:                        "en",
		DateFormat:                    "2006-01-02",
		Timezone:                      "UTC",
		InvoiceNumberFmt:              "INV-{YYYY}-{SEQ:4}",
		InvoiceNextSeq:                1,
		InvoiceSeqResetYr:             true,
		DefaultDueDays:                14,
		DefaultTerms:                  "Payment is due within {due_days} days of the invoice date.",
		DefaultFooter:                 "Thank you for your business.",
		DefaultBilling:                "hourly",
		DefaultPaymentMethod:          PaymentBankTransfer,
		HoursPerDay:                   8,
		TimeRoundingMin:               15,
		ShowTaxColumn:                 true,
		SMTPPort:                      587,
		SMTPTLS:                       "starttls",
		EmailSubject:                  "Invoice for the {month}: {number_short}",
		EmailBody:                     "Hi {first_name},\n\nHere is my invoice for {month}.\n\nLet me know if you have any questions.\n\nThank you,\n{company}",
		ReminderDays:                  "3,7,14",
		NumberFormat:                  "1,234.56",
		ShowBaseTotal:                 true,
		BaseTotalNote:                 "Exchange rate 1 {currency} = {rate} {base}. Total in {base}: {total_base}.",
		EmailAttachmentMode:           "generated",
		WorkWeek:                      calendar.DefaultWorkWeek,
		Holidays:                      []calendar.Holiday{},
		IncomeTaxRate:                 10,
		IncomeTaxBasis:                "revenue",
		IncomeTaxByPaymentDate:        true,
		IncomeTaxLabel:                "Income tax",
		DocumentsTags:                 "pocket-invoicing",
		DocumentsInvoiceType:          "Invoice",
		DocumentsCreateCorrespondents: true,
		DocumentsArchiveInvoices:      "off",
		DocumentsCategoryAsType:       true,
	}
}

// boxKeys are the settings that stay box-wide (admin-only): they describe the shared
// exchange-rate table, not anyone's business. Everything else in Settings is personal.
var boxKeys = []string{"base_currency", "exchange_rate_provider", "setup_complete"}

// boxSettings is the box-wide part stored in the global settings row 'app'.
type boxSettings struct {
	BaseCurrency         string `json:"base_currency"`
	ExchangeRateProvider string `json:"exchange_rate_provider"`
	SetupComplete        bool   `json:"setup_complete"`
}

func (s *Store) getBox(ctx context.Context) (boxSettings, error) {
	d := DefaultSettings()
	out := boxSettings{BaseCurrency: d.BaseCurrency}
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'app'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, err
	}
	if out.BaseCurrency == "" {
		out.BaseCurrency = d.BaseCurrency
	}
	return out, nil
}

// GetGlobalSettings returns defaults plus the box-wide fields (base currency, rate
// provider, setup flag). It carries no personal data; use GetSettings for a user.
func (s *Store) GetGlobalSettings(ctx context.Context) (Settings, error) {
	out := DefaultSettings()
	box, err := s.getBox(ctx)
	if err != nil {
		return out, err
	}
	out.BaseCurrency, out.ExchangeRateProvider, out.SetupComplete = box.BaseCurrency, box.ExchangeRateProvider, box.SetupComplete
	return out, nil
}

// SaveGlobalSettings persists only the box-wide fields of st.
func (s *Store) SaveGlobalSettings(ctx context.Context, st Settings) error {
	b, err := json.Marshal(boxSettings{BaseCurrency: st.BaseCurrency, ExchangeRateProvider: st.ExchangeRateProvider, SetupComplete: st.SetupComplete})
	if err != nil {
		return err
	}
	return s.SetKV(ctx, "app", string(b))
}

// GetSettings loads one user's settings (defaults for missing keys) merged with the
// box-wide fields.
func (s *Store) GetSettings(ctx context.Context, uid int64) (Settings, error) {
	out := DefaultSettings()
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ?`, uid).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return out, err
		}
	}
	box, err := s.getBox(ctx)
	if err != nil {
		return out, err
	}
	out.BaseCurrency, out.ExchangeRateProvider, out.SetupComplete = box.BaseCurrency, box.ExchangeRateProvider, box.SetupComplete
	return out, nil
}

// SaveSettings persists a user's personal settings. Box-wide fields are ignored here;
// they are written by SaveGlobalSettings (admins only).
func (s *Store) SaveSettings(ctx context.Context, uid int64, st Settings) error {
	if uid <= 0 {
		return errors.New("save settings: no user")
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for _, k := range boxKeys {
		delete(m, k)
	}
	b, _ = json.Marshal(m)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO user_settings (user_id, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, uid, string(b), Now())
	return err
}

// GetKV reads an arbitrary settings key.
func (s *Store) GetKV(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetKV writes an arbitrary settings key.
func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, Now())
	return err
}
