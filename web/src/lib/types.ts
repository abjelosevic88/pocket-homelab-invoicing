export interface User { id: number; email: string; name: string; role: string; created_at: string }
export interface Currency { code: string; name: string; symbol: string; decimals: number; enabled: boolean }
export interface ExchangeRate { base: string; quote: string; rate: number; source: string; fetched_at: string }
export interface TaxRate { id: number; name: string; rate: number; is_default: boolean }
export interface Client {
  id: number; name: string; contact_name: string; email: string; phone: string; address1: string; address2: string; city: string; state: string; postal_code: string; country: string; tax_id: string; website: string;
  currency: string; billing_mode: string; default_rate: number; payment_terms_days: number; notes: string; archived: boolean; created_at: string; updated_at: string;
  invoice_count?: number; outstanding: number; total_billed: number;
}
export interface Product { id: number; name: string; description: string; unit: string; unit_price: number; currency: string; tax_rate: number; archived: boolean }
export interface InvoiceTemplate { id: number; name: string; layout: string; accent_color: string; labels: Record<string, string>; html: string; is_default: boolean }
export interface InvoiceItem { id?: number; description: string; unit: string; quantity: number; unit_price: number; tax_rate: number; discount: number; line_total?: number }
export interface Payment { id: number; invoice_id: number; invoice_number?: string; client_name?: string; date: string; amount: number; currency: string; exchange_rate: number; applied_amount: number; method: string; reference: string; notes: string; created_at: string }
export interface Invoice {
  id: number; number: string; client_id: number; client_name: string; status: string; issue_date: string; due_date: string; currency: string; exchange_rate: number; billing_mode: string;
  period_start: string | null; period_end: string | null; po_number: string; discount_type: string; discount_value: number; subtotal: number; discount_total: number; tax_total: number; total: number; amount_paid: number; balance: number;
  notes: string; terms: string; footer: string; template_id: number | null; recurring_id: number | null; public_token: string; sent_at: string | null; viewed_at: string | null; paid_at: string | null; created_at: string; updated_at: string;
  items?: InvoiceItem[]; payments?: Payment[];
}
export interface RecurringItem { description: string; unit: string; quantity: number; unit_price: number; tax_rate: number; discount: number }
export interface Recurring {
  id: number; name: string; client_id: number; client_name: string; status: string; frequency: string; interval: number; start_date: string; end_date: string | null; next_run: string; last_run: string | null; occurrences: number; max_occurrences: number; due_days: number; currency: string; billing_mode: string; items: RecurringItem[]; discount_type: string; discount_value: number; notes: string; terms: string; auto_send: boolean; template_id: number | null;
}
export interface TimeEntry { id: number; client_id: number; client_name: string; project: string; description: string; started_at: string; ended_at: string | null; duration_minutes: number; billable: boolean; rate: number | null; invoice_id: number | null }
export interface Expense { id: number; client_id: number | null; client_name: string; date: string; category: string; description: string; amount: number; currency: string; exchange_rate: number; billable: boolean; invoice_id: number | null }
export interface Webhook { id: number; url: string; events: string; secret: string; enabled: boolean }
export interface APIToken { id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }
export interface Activity { id: number; entity_type: string; entity_id: number; action: string; message: string; created_at: string }
export interface Settings {
  company_name: string; company_email: string; company_phone: string; company_website: string; address1: string; address2: string; city: string; state: string; postal_code: string; country: string; tax_id: string; logo_path: string;
  base_currency: string; locale: string; date_format: string; timezone: string; invoice_number_format: string; invoice_next_seq: number; invoice_seq_reset_yearly: boolean; invoice_seq_year: number; default_due_days: number; default_notes: string; default_terms: string; default_footer: string; default_billing_mode: string;
  default_hourly_rate: number; default_daily_rate: number; default_monthly_rate: number; default_tax_rate: number; hours_per_day: number; time_rounding_minutes: number; show_tax_column: boolean; payment_details: string;
  smtp_host: string; smtp_port: number; smtp_user: string; smtp_password: string; smtp_from: string; smtp_from_name: string; smtp_tls: string; smtp_bcc: string; email_subject: string; email_body: string; reminder_days: string; reminders_enabled: boolean; setup_complete: boolean;
}
export interface DashboardStats { outstanding: number; outstanding_count: number; overdue: number; overdue_count: number; paid_this_month: number; paid_this_year: number; invoiced_this_year: number; draft_count: number; unbilled_minutes: number; unbilled_expenses: number; active_clients: number }
export interface MonthlyRevenue { month: string; invoiced: number; paid: number; expenses: number; count: number }
