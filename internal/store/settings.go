package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	LogoPath       string `json:"logo_path"`

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

	PaymentDetails           string            `json:"payment_details"`             // bank account, IBAN, PayPal etc. shown on invoice
	PaymentDetailsByCurrency map[string]string `json:"payment_details_by_currency"` // optional per-currency override (e.g. USD IBAN)
	NumberFormat             string            `json:"number_format"`               // "1,234.56" | "1.234,56" | "1 234,56" | "1'234.56"
	CustomFields             []CustomFieldDef  `json:"custom_fields"`               // user-defined invoice fields
	ShowBaseTotal            bool              `json:"show_base_total"`             // print the total converted to the base currency when currencies differ
	BaseTotalNote            string            `json:"base_total_note"`             // sentence printed under the totals; placeholders {rate} {currency} {currency_name} {base} {total_base}
	EmailAttachmentMode      string            `json:"email_attachment_mode"`       // generated | uploaded | both

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

	SetupComplete bool `json:"setup_complete"`
}

// DefaultSettings returns the initial settings.
func DefaultSettings() Settings {
	return Settings{
		CompanyName:            "My Company",
		BaseCurrency:           "EUR",
		Locale:                 "en",
		DateFormat:             "2006-01-02",
		Timezone:               "UTC",
		InvoiceNumberFmt:       "INV-{YYYY}-{SEQ:4}",
		InvoiceNextSeq:         1,
		InvoiceSeqResetYr:      true,
		DefaultDueDays:         14,
		DefaultTerms:           "Payment is due within {due_days} days of the invoice date.",
		DefaultFooter:          "Thank you for your business.",
		DefaultBilling:         "hourly",
		HoursPerDay:            8,
		TimeRoundingMin:        15,
		ShowTaxColumn:          true,
		SMTPPort:               587,
		SMTPTLS:                "starttls",
		EmailSubject:           "Invoice {number} from {company}",
		EmailBody:              "Hi {client},\n\nPlease find attached invoice {number} for {total}, due on {due_date}.\n\nYou can also view it online: {link}\n\nThanks,\n{company}",
		ReminderDays:           "3,7,14",
		NumberFormat:           "1,234.56",
		ShowBaseTotal:          true,
		BaseTotalNote:          "Exchange rate 1 {currency} = {rate} {base}. Total in {base}: {total_base}.",
		EmailAttachmentMode:    "generated",
		IncomeTaxRate:          10,
		IncomeTaxBasis:         "revenue",
		IncomeTaxByPaymentDate: true,
		IncomeTaxLabel:         "Income tax",
	}
}

// GetSettings loads settings, applying defaults for missing keys.
func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	out := DefaultSettings()
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
	return out, nil
}

// SaveSettings persists settings.
func (s *Store) SaveSettings(ctx context.Context, st Settings) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('app', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, string(b), Now())
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
