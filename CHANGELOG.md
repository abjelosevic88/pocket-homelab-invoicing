# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Day calendar: in the month-end wizard and the invoice editor ("Days worked" card). Working days pre-selected, days toggle on/off, week numbers toggle a whole week; the selection drives the day (or hour) quantity. Picked dates are stored on the invoice (`worked_days`, migration 0010) and shown on the invoice page.
- Month-end wizard (sidebar → Month-end): proposes one invoice per client for a month from the recurring profile, tracked time or the working-days calendar, pre-fills counter-style custom fields (`14/15ПП` → `15/16ПП`), creates all drafts in one click and lists the month's invoices with upload / email / mark-sent actions. `GET|POST /api/v1/month-end`.
- Working-days calendar (Settings → Invoicing): configurable work week, yearly holidays and one-off days off, presets for Republika Srpska, Federation of BiH and Orthodox Easter, monthly preview. `GET /api/v1/calendar/working-days`, `/calendar/presets`.
- Recurring profiles: "Quantity: working days of the period" (days, or days × hours per day) and "Service period: previous period (in arrears)" so a run on the 1st bills the month that just ended.
- Accountant package: `GET /api/v1/reports/accountant-package.zip?year=` and a card under Reports → Income tax. One ZIP with every invoice PDF, uploaded originals, `invoices.csv` (with base-currency totals, custom fields and the exchange-rate sentence), `payments.csv`, `expenses.csv`, `exchange-rates.csv`, `income-tax.csv` and `SUMMARY.md`.
- `docs/homelab-integration.md`: backup (restic/Backrest, consistent SQLite snapshots), Uptime Kuma, Prometheus and Homepage customapi recipes.

### Fixed
- Exchange rates stored against a previous base currency (for example EUR-based ECB rows from before switching to BAM) lingered invisibly and could feed wrong cross rates. A migration removes them, and changing the base currency now prunes old rows and refreshes rates for the new base automatically.

### Changed
- Text labels replace the arrow icons on document, attachment and payment row actions (Open in Paperless, Download, Import, Edit, Replace, To Paperless, Delete).

### Added
- Documents: a company-wide file store (contracts, registration, tax, bank, certificates …) with categories, client link, document date, expiry warnings, search and a card on the client page.
- Clients can be linked to a Paperless correspondent (picker in the client form, name-based sync in Settings → Paperless); the client page lists that correspondent's documents and archived files are filed under it.
- Optional Paperless-ngx integration (Settings → Paperless): archive invoices manually or automatically when sent/paid, send company documents, browse and search the Paperless archive from Documents, copy documents back, consume-task tracking with links. Off unless a URL and token are configured; `PAPERLESS_URL` / `PAPERLESS_TOKEN` env defaults.
- Email placeholders `{first_name}`, `{month}`, `{year}` and `{number_short}`; the default invoice email is now a short personal note ("Here is my invoice for August").

### Changed
- Invoice numbers are now unique per client instead of globally (migration rebuilds the invoices table). The editor warns when another client already uses a number; auto-numbering still skips numbers used anywhere.

### Added
- Per-client email template (subject, message, always-CC) overriding the global one; new placeholders {contact} and {period}.
- Income tax estimate: configurable rate, base (gross or profit), cash/invoice basis, yearly minimum, allowance and fixed monthly contributions (Settings → Taxes); Reports → Income tax with per-month and per-year breakdown; year selector on Reports.
- Dashboard lifetime earnings, by-year and by-client views.
- Word (.docx) invoice templates: upload any number, assign per client or per invoice, global default; Mustache-style placeholders with table-row repetition; PDF via Gotenberg (LibreOffice) or a local `soffice`; download the filled .docx. Sample templates in docs/templates/.
- Invoice attachments: upload your own PDFs per invoice, download via the public link, and choose whether emails carry the generated PDF, the uploaded files, or both.
- Custom invoice fields (Settings → Invoicing), printed in the PDF header block (e.g. fiscal receipt numbers).
- Base-currency total on invoices in other currencies, with a configurable sentence (`{rate}`, `{currency_name}`, `{total_base}` …).
- Per-currency payment details (different IBAN for EUR and USD invoices).
- Number format setting (`1.234,56`, `1 234,56`, …) applied to PDFs and the UI.
- Template options: hide rate/unit columns, summed quantity in totals ("100 hours"), signature line, hide logo.
- Exchange-rate refresh falls back to EUR cross-rates when the provider does not know the base currency (BAM, RSD …).
- `scripts/import_invoiceninja.py` and docs/migrating-from-invoice-ninja.md.

### Changed
- Payment terms of 0 days are allowed (due on receipt).

## [0.1.0] - 2026-10-08

### Added
- Clients with per-client currency, billing mode (hourly / daily / monthly retainer / fixed), default rate and payment terms.
- Invoices with line-level units, discounts and tax, document-level discount, service period, PO number, configurable numbering (`INV-{YYYY}-{SEQ:4}`).
- Status workflow draft → sent → viewed → partial → paid / overdue / cancelled, automatic overdue marking.
- Payments (partial, multi-currency, several methods).
- Recurring invoice profiles (daily … yearly, interval, end date / max occurrences, auto-send, placeholders), catch-up after downtime.
- Time tracking with timer, rounding, manual entries, invoice-from-time (hours or days; grouped by entry / day / project / total).
- Expenses with re-billing.
- Multi-currency: 56 seeded currencies, ECB rates via frankfurter.app, manual rates, cross/inverse rate resolution, rate locked per invoice.
- PDF engines: native (pure Go, embedded DejaVu fonts, three layouts), Chromium, Gotenberg; HTML template customisation; label overrides for localisation.
- Public invoice links with web view, PDF download and view tracking.
- SMTP email with PDF attachment, test email, automatic payment reminders.
- Reports: revenue by month, by client, aging, tax by rate, by currency, time; CSV exports.
- Dashboard with KPIs and revenue chart.
- Auth: local login with sessions, reverse-proxy header mode, no-auth mode, API tokens.
- Outgoing webhooks with HMAC signatures.
- Prometheus metrics, health/readiness endpoints, structured logging.
- Backups (`VACUUM INTO`) via UI, API and CLI; JSON export.
- Docker image (amd64/arm64/armv7), docker-compose with optional Gotenberg profile, Unraid template, Kubernetes manifest, systemd unit, reverse-proxy examples.
