# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.6.0] - 2026-10-20

### Added
- Settings → Business details: **VAT ID** next to the tax ID (e.g. PDV broj next to the JIB). Printed under the tax ID in the PDF header with the template label `vat_id` (default "VAT ID"), available to Word templates as `company.vat_id` and listed in the accountant package.
- Custom fields get an **Auto-fill** setting per field (Settings → Invoicing → Custom invoice fields): *No auto-fill* (default), *Copy previous* or *Increment previous* (`14/15ПП` → `15/16ПП`, `ELJA9MLH-ELJA9MLH-15` → `ELJA9MLH-ELJA9MLH-16`), taken from the client's latest sent or paid invoice. The invoice editor fills empty fields of new invoices and drafts and shows the previous value under each field; month-end and recurring drafts use the same values (not auto-sent ones). `GET /api/v1/invoices/next-custom-fields?client_id=` returns `last` and `next`.

- Clients: **Company reg. no.** (e.g. Luxembourg RCS B274410), printed under Bill to as "Reg. no. RCS B274410" (template label `registration_no`, no colon) and available to Word templates as `client.registration_no`.

- Settings → Company: **Place of issue** (e.g. Banja Luka), printed under the invoice date (label `issue_place`, Word: `issue_place`).
- Settings → Invoicing: **Always print the tax line** — the totals show "Tax 0% 0,00" when an invoice has no tax, for laws that require the VAT rate and amount on every invoice (BiH Zakon o PDV-u čl. 107).

### Changed
- The client's tax number is now labelled **VAT ID** (template label `client_tax_id`) in the client form and under Bill to, separate from your own `tax_id` label, so a template that prints yours as "JIB" no longer prints the client's as "JIB" too.
- Counter suggestions now bump the last number of any value, not only `N`, `N/M` or `N/MПП`, so the month-end wizard also suggests the PFR number.
- Recurring profiles and the month-end wizard fall back to the default notes and terms in Settings when the profile has none of its own.

### Fixed
- Native PDF: the signature line sits to the right of the last text block (notes/terms) instead of on its own row, so a full invoice keeps it on page 1.
- Card headers: when the title and its buttons do not fit on one line (e.g. Settings → Templates in a narrow column), the buttons wrap below the title instead of overlapping it.
- Tabs no longer show a vertical scrollbar (the active underline overflowed the strip) or a focus box after a click; tabs inside cards are a little tighter and the template editor tab is just "HTML", so all three fit.
- Settings → Invoicing reorganised: Numbering and Custom invoice fields stacked beside Defaults; one full-width "Text printed on invoices" card (sentence under the totals, notes, terms, footer, in PDF order); Working days & holidays full width (cards marked full width now really span both columns); a single sticky Save button that shows when there are unsaved changes.
- Changing the default Notes, Terms or Footer in Settings also updates drafts that still carry the previous default text; drafts with their own text and sent/paid invoices are untouched.
- Draft PDFs (and Word output) use the current default Notes, Terms and Footer from Settings when the draft's own field is empty, so Settings edits show up on open drafts without re-saving them. Sent and paid invoices keep their stored text.
- Native PDF: the signature line only moves to a new page when it really does not fit (it reserved 4 mm too much, so one line of notes pushed it alone onto page 2).

## [0.5.0] - 2026-10-20

### Changed
- Core SDK v0.16.0. Paperless through Pocket Core reuses the person's credential for about a minute and rides out a Core restart; Test connection asks Core afresh.
- Settings → Paperless with a Pocket Core: **Pocket Core | Off**, Direct behind "Advanced: connect directly with a URL and key" (shown whenever it is the current mode, and normally without a Core).

### Added
- Connection modes on Core's map: every person's Paperless mode in effect is reported to Core (`service:paperless`) after registering, every 30 minutes and right after a switch or Disconnect.

## [0.2.1] - 2026-10-19

### Changed
- Every dropdown in the web UI (52 of them: client, currency, billing mode, template, tax, unit, filters, settings…) is now the searchable combobox used by Pocket Inventory: type to filter, arrow keys + Enter, hints shown in grey beside the label instead of crammed into parentheses. Line-item unit/tax pickers float above the scrolling items table so they are never clipped. Values, defaults and side effects are unchanged.

## [0.2.0] - 2026-10-19

### Changed
- Every person is their own business. Clients, invoices (with items, attachments, payments and the email log), recurring profiles, time entries, expenses, products, tax rates, templates (including Word files), company documents, webhooks, Paperless links and the activity log belong to exactly one account, and every query, report, CSV/JSON export, accountant package, search, month-end plan and public-link render is scoped to it. Another person's id answers 404, never 403.
- Settings are per person: company/sender profile, logo, invoice number format and sequence, defaults, custom fields, working-days calendar, income-tax settings, SMTP and email texts, reminders and Paperless credentials. The base currency, exchange-rate provider, enabled currencies and the rate table stay box-wide and only admins change them.
- Roles: the first account is the admin (box owner); accounts created later, including the ones auto-provisioned by Pocket Core / OIDC sign-in, Core device tokens, proxy or no-auth mode, are members. Admin-only: users, currencies and rates, system info, database backup, running the scheduler. Admins get no extra access to anyone's records.
- The scheduler runs per person (recurring invoices, overdue marking, reminders with that person's SMTP, Paperless follow-ups); webhooks only fire for the owner of the record; Core events carry `user_email`.
- `PAPERLESS_URL` / `PAPERLESS_TOKEN` environment defaults now only apply to the owner account.
- Invoice numbers stay unique per client (clients belong to one person, so no two people can collide) and each person has their own sequence.

### Migration
- `0011_per_user.sql` adds `user_id` to every content table (invoice items inherit through their invoice), backfills all existing rows to the owner (first admin), moves the personal settings into `user_settings` and demotes accounts that were admin only because they were auto-created (no local password).

## [0.1.1] – [0.1.2]

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
