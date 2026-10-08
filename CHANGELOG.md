# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
