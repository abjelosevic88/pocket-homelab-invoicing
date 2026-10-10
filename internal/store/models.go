package store

// User is an application user.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token.
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Currency is an ISO 4217 currency.
type Currency struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Enabled  bool   `json:"enabled"`
}

// ExchangeRate is a base->quote conversion rate.
type ExchangeRate struct {
	Base      string  `json:"base"`
	Quote     string  `json:"quote"`
	Rate      float64 `json:"rate"`
	Source    string  `json:"source"`
	FetchedAt string  `json:"fetched_at"`
}

// TaxRate is a named tax percentage.
type TaxRate struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Rate      float64 `json:"rate"`
	IsDefault bool    `json:"is_default"`
	Archived  bool    `json:"archived"`
}

// Client is a customer.
type Client struct {
	ID                       int64   `json:"id"`
	Name                     string  `json:"name"`
	ContactName              string  `json:"contact_name"`
	Email                    string  `json:"email"`
	Phone                    string  `json:"phone"`
	Address1                 string  `json:"address1"`
	Address2                 string  `json:"address2"`
	City                     string  `json:"city"`
	State                    string  `json:"state"`
	PostalCode               string  `json:"postal_code"`
	Country                  string  `json:"country"`
	TaxID                    string  `json:"tax_id"`
	Website                  string  `json:"website"`
	Currency                 string  `json:"currency"`
	BillingMode              string  `json:"billing_mode"`
	DefaultRate              float64 `json:"default_rate"`
	PaymentTermsDays         int     `json:"payment_terms_days"`
	TemplateID               *int64  `json:"template_id"`
	EmailSubject             string  `json:"email_subject"`              // per-client override of the invoice email subject ("" = global)
	EmailBody                string  `json:"email_body"`                 // per-client override of the invoice email body ("" = global)
	EmailCC                  string  `json:"email_cc"`                   // extra recipients, comma separated
	PaperlessCorrespondentID int64   `json:"paperless_correspondent_id"` // 0 = not linked
	PaperlessCorrespondent   string  `json:"paperless_correspondent"`    // cached name for display
	Notes                    string  `json:"notes"`
	Archived                 bool    `json:"archived"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`

	// Computed (list views)
	InvoiceCount int64   `json:"invoice_count,omitempty"`
	Outstanding  float64 `json:"outstanding"`
	TotalBilled  float64 `json:"total_billed"`
}

// Product is a catalog item / rate card entry.
type Product struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	UnitPrice   float64 `json:"unit_price"`
	Currency    string  `json:"currency"`
	TaxRate     float64 `json:"tax_rate"`
	Archived    bool    `json:"archived"`
	CreatedAt   string  `json:"created_at"`
}

// InvoiceTemplate controls the look of a rendered invoice.
type InvoiceTemplate struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Layout      string            `json:"layout"`
	AccentColor string            `json:"accent_color"`
	Labels      map[string]string `json:"labels"`
	Options     TemplateOptions   `json:"options"`
	HTML        string            `json:"html"`
	Kind        string            `json:"kind"`      // design | docx
	DocxPath    string            `json:"docx_path"` // stored file name for kind == docx
	DocxName    string            `json:"docx_name"` // original upload name (derived)
	IsDefault   bool              `json:"is_default"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// TemplateOptions are layout switches that apply to every PDF engine.
type TemplateOptions struct {
	HideRate          bool   `json:"hide_rate"`           // hide the unit price column
	HideUnit          bool   `json:"hide_unit"`           // hide the unit column (quantity shows "21 days")
	ShowQuantityTotal bool   `json:"show_quantity_total"` // print the summed quantity in the totals row
	SignatureLabel    string `json:"signature_label"`     // e.g. "Odgovorno lice" — prints a signature line
	HideLogo          bool   `json:"hide_logo"`
}

// Attachment is a file stored alongside an invoice (e.g. a signed PDF).
type Attachment struct {
	ID          int64  `json:"id"`
	InvoiceID   int64  `json:"invoice_id"`
	Filename    string `json:"filename"`
	StoredName  string `json:"-"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"created_at"`

	Paperless *PaperlessLink `json:"paperless,omitempty"`
}

// CustomFieldDef describes a user-defined invoice field (Settings → Invoicing).
type CustomFieldDef struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	ShowOnPDF bool   `json:"show_on_pdf"`
}

// InvoiceItem is a single invoice line.
type InvoiceItem struct {
	ID          int64   `json:"id"`
	InvoiceID   int64   `json:"invoice_id"`
	Position    int     `json:"position"`
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TaxRate     float64 `json:"tax_rate"`
	Discount    float64 `json:"discount"`
	LineTotal   float64 `json:"line_total"`
}

// Invoice is the central document.
type Invoice struct {
	ID            int64   `json:"id"`
	Number        string  `json:"number"`
	ClientID      int64   `json:"client_id"`
	ClientName    string  `json:"client_name"`
	Status        string  `json:"status"`
	IssueDate     string  `json:"issue_date"`
	DueDate       string  `json:"due_date"`
	Currency      string  `json:"currency"`
	ExchangeRate  float64 `json:"exchange_rate"`
	BillingMode   string  `json:"billing_mode"`
	PeriodStart   *string `json:"period_start"`
	PeriodEnd     *string `json:"period_end"`
	PONumber      string  `json:"po_number"`
	DiscountType  string  `json:"discount_type"`
	DiscountValue float64 `json:"discount_value"`
	Subtotal      float64 `json:"subtotal"`
	DiscountTotal float64 `json:"discount_total"`
	TaxTotal      float64 `json:"tax_total"`
	Total         float64 `json:"total"`
	AmountPaid    float64 `json:"amount_paid"`
	Balance       float64 `json:"balance"`
	Notes         string  `json:"notes"`
	Terms         string  `json:"terms"`
	Footer        string  `json:"footer"`
	TemplateID    *int64  `json:"template_id"`
	RecurringID   *int64  `json:"recurring_id"`
	PublicToken   string  `json:"public_token"`
	SentAt        *string `json:"sent_at"`
	ViewedAt      *string `json:"viewed_at"`
	PaidAt        *string `json:"paid_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`

	CustomFields map[string]string `json:"custom_fields"`

	Items       []InvoiceItem  `json:"items,omitempty"`
	Payments    []Payment      `json:"payments,omitempty"`
	Attachments []Attachment   `json:"attachments,omitempty"`
	Paperless   *PaperlessLink `json:"paperless,omitempty"`
}

// Payment records money received against an invoice.
type Payment struct {
	ID            int64   `json:"id"`
	InvoiceID     int64   `json:"invoice_id"`
	InvoiceNumber string  `json:"invoice_number,omitempty"`
	ClientName    string  `json:"client_name,omitempty"`
	Date          string  `json:"date"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	ExchangeRate  float64 `json:"exchange_rate"`
	AppliedAmount float64 `json:"applied_amount"`
	Method        string  `json:"method"`
	Reference     string  `json:"reference"`
	Notes         string  `json:"notes"`
	CreatedAt     string  `json:"created_at"`
}

// RecurringItem is a line template inside a recurring profile.
type RecurringItem struct {
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TaxRate     float64 `json:"tax_rate"`
	Discount    float64 `json:"discount"`
}

// RecurringInvoice is a schedule that generates invoices.
type RecurringInvoice struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	ClientID       int64           `json:"client_id"`
	ClientName     string          `json:"client_name"`
	Status         string          `json:"status"`
	Frequency      string          `json:"frequency"`
	Interval       int             `json:"interval"`
	StartDate      string          `json:"start_date"`
	EndDate        *string         `json:"end_date"`
	NextRun        string          `json:"next_run"`
	LastRun        *string         `json:"last_run"`
	Occurrences    int             `json:"occurrences"`
	MaxOccurrences int             `json:"max_occurrences"`
	DueDays        int             `json:"due_days"`
	Currency       string          `json:"currency"`
	BillingMode    string          `json:"billing_mode"`
	Items          []RecurringItem `json:"items"`
	DiscountType   string          `json:"discount_type"`
	DiscountValue  float64         `json:"discount_value"`
	Notes          string          `json:"notes"`
	Terms          string          `json:"terms"`
	AutoSend       bool            `json:"auto_send"`
	TemplateID     *int64          `json:"template_id"`
	QuantityMode   string          `json:"quantity_mode"` // fixed | working_days
	PeriodMode     string          `json:"period_mode"`   // forward | arrears
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

// TimeEntry is a tracked block of work.
type TimeEntry struct {
	ID              int64    `json:"id"`
	ClientID        int64    `json:"client_id"`
	ClientName      string   `json:"client_name"`
	Project         string   `json:"project"`
	Description     string   `json:"description"`
	StartedAt       string   `json:"started_at"`
	EndedAt         *string  `json:"ended_at"`
	DurationMinutes int      `json:"duration_minutes"`
	Billable        bool     `json:"billable"`
	Rate            *float64 `json:"rate"`
	InvoiceID       *int64   `json:"invoice_id"`
	CreatedAt       string   `json:"created_at"`
}

// Expense is a cost, optionally re-billable to a client.
type Expense struct {
	ID           int64   `json:"id"`
	ClientID     *int64  `json:"client_id"`
	ClientName   string  `json:"client_name"`
	Date         string  `json:"date"`
	Category     string  `json:"category"`
	Description  string  `json:"description"`
	Amount       float64 `json:"amount"`
	Currency     string  `json:"currency"`
	ExchangeRate float64 `json:"exchange_rate"`
	Billable     bool    `json:"billable"`
	InvoiceID    *int64  `json:"invoice_id"`
	CreatedAt    string  `json:"created_at"`
}

// Webhook is an outgoing webhook subscription.
type Webhook struct {
	ID        int64  `json:"id"`
	URL       string `json:"url"`
	Events    string `json:"events"`
	Secret    string `json:"secret"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

// Activity is an audit log entry.
type Activity struct {
	ID         int64  `json:"id"`
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	Action     string `json:"action"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

// EmailLog records an attempted email.
type EmailLog struct {
	ID        int64  `json:"id"`
	InvoiceID *int64 `json:"invoice_id"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	CreatedAt string `json:"created_at"`
}

// Invoice statuses.
const (
	StatusDraft     = "draft"
	StatusSent      = "sent"
	StatusViewed    = "viewed"
	StatusPartial   = "partial"
	StatusPaid      = "paid"
	StatusOverdue   = "overdue"
	StatusCancelled = "cancelled"
)

// Billing modes / units.
const (
	UnitHour  = "hour"
	UnitDay   = "day"
	UnitMonth = "month"
	UnitUnit  = "unit"
	UnitFixed = "fixed"
)

// Document is a company-wide file (contract, registration, certificate …) not tied to an invoice.
type Document struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	ClientID    *int64 `json:"client_id"`
	ClientName  string `json:"client_name"`
	DocDate     string `json:"doc_date"`
	ExpiresAt   string `json:"expires_at"`
	Notes       string `json:"notes"`
	Filename    string `json:"filename"`
	StoredName  string `json:"-"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`

	Paperless *PaperlessLink `json:"paperless,omitempty"`
}

// PaperlessLink records that a local file was archived in Paperless-ngx.
type PaperlessLink struct {
	Kind        string `json:"kind"`
	RefID       int64  `json:"ref_id"`
	PaperlessID int64  `json:"paperless_id"`
	TaskID      string `json:"task_id,omitempty"`
	Title       string `json:"title"`
	Error       string `json:"error,omitempty"`
	URL         string `json:"url,omitempty"` // filled by the server when an external URL is known
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}
