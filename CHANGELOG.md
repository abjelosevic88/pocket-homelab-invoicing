# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
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
