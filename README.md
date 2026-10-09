<p align="center">
  <img src="web/public/favicon.svg" width="72" alt="">
</p>
<h1 align="center">Pocket Invoicing</h1>
<p align="center">
  Self-hosted invoicing for freelancers, consultants and homelabbers.<br>
  <b>One binary · one SQLite file · one volume.</b> No PHP, no Redis, no MySQL.
</p>
<p align="center">
  <a href="https://github.com/abjelosevic88/pocket-homelab-invoicing/actions"><img src="https://img.shields.io/github/actions/workflow/status/abjelosevic88/pocket-homelab-invoicing/ci.yml?branch=main&label=CI" alt="CI"></a>
  <a href="https://github.com/abjelosevic88/pocket-homelab-invoicing/pkgs/container/pocket-homelab-invoicing"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64%20%7C%20armv7-blue" alt="image"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT"></a>
  <img src="https://img.shields.io/badge/image%20size-~15MB-lightgrey" alt="size">
</p>

---

Pocket Invoicing is an Invoice-Ninja-style app trimmed down to what a one-person business actually needs, and packaged the way the self-hosting community expects: a tiny multi-arch container, environment-variable config, a health endpoint, Prometheus metrics, reverse-proxy auth, an API with tokens, and webhooks for n8n / Home Assistant.

## Features

| Area | What you get |
|---|---|
| **Billing modes** | Hourly, daily, monthly retainer, fixed price, or per unit. Mix them on one invoice. |
| **Time tracking** | Start/stop timer, manual entries, rounding (e.g. 15 min), bill unbilled time as hours *or* days in one click, grouped per entry / day / project / total. |
| **Recurring invoices** | Weekly → yearly schedules, "every N" intervals, end date or max occurrences, `{month}` / `{period}` placeholders, optional auto-send. Missed runs catch up after downtime. |
| **Multi-currency** | Invoice each client in its own currency. Rates from the ECB (free, no key) or entered manually. Payments can arrive in a third currency. Reports convert everything to your base currency at the rate locked on each invoice. 56 currencies seeded, add your own (incl. crypto). |
| **PDF & templates** | Three built-in layouts (Classic, Modern, Minimal), accent colour, logo, per-template label overrides for localisation (RECHNUNG / FAKTURA / …), optional custom HTML template. **Word templates:** upload any `.docx` with `{{placeholders}}`, keep several, assign per client, converted by Gotenberg. Pure-Go PDF engine by default. |
| **Payments** | Partial payments, multiple methods, references. Status flow draft → sent → viewed → partial → paid, with automatic overdue marking. |
| **Client portal link** | Every invoice has a share link (`/i/<token>`) with web view, PDF download and view tracking. No client accounts needed. |
| **Email** | SMTP with PDF attachment, test button, BCC yourself, automatic payment reminders (e.g. 3, 7, 14 days after due). |
| **Expenses** | Track costs, re-bill them to clients, see net income. |
| **Reports** | Revenue by month, by client, aging (outstanding by days overdue), tax summary by rate, currency breakdown, time summary. CSV exports for everything. |
| **Catalog** | Products / rate cards for one-click line items. Tax rates with a default. |
| **Local compliance** | Custom invoice fields (fiscal numbers), total in your base currency with a legal sentence, per-currency bank details, European number formats, signature line, attach your own signed PDF. |
| **Automation** | REST API with personal access tokens, outgoing webhooks (HMAC-signed), Prometheus `/metrics`, `/healthz` + `/readyz`. |
| **Ops-friendly** | Single static binary, embedded SPA, SQLite with WAL, `VACUUM INTO` backups from the UI/CLI/API, JSON export, structured logs, graceful shutdown, non-root container, dark mode. |

## Quick start

```bash
mkdir pocket-invoicing && cd pocket-invoicing
curl -O https://raw.githubusercontent.com/abjelosevic88/pocket-homelab-invoicing/main/docker-compose.yml
docker compose up -d
```

Open `http://<host>:8080`, create your admin account in the first-run wizard, add your company details and logo under **Settings**, and start invoicing.

<details>
<summary><b>docker run one-liner</b></summary>

```bash
docker run -d --name pocket-invoicing -p 8080:8080 -v $PWD/data:/data \
  -e TZ=Europe/Berlin -e BASE_URL=https://invoices.example.com \
  ghcr.io/abjelosevic88/pocket-homelab-invoicing:latest
```
</details>

<details>
<summary><b>Try it with demo data</b></summary>

```bash
docker run --rm -p 8080:8080 -e DEMO_DATA=true ghcr.io/abjelosevic88/pocket-homelab-invoicing:latest
```
Three clients in EUR/USD/GBP, paid and overdue invoices, a monthly retainer, tracked time and expenses.
</details>

<details>
<summary><b>Unraid</b></summary>

Add the template URL in *Docker → Add Container → Template repositories*:
`https://raw.githubusercontent.com/abjelosevic88/pocket-homelab-invoicing/main/deploy/unraid/pocket-invoicing.xml`
</details>

<details>
<summary><b>Kubernetes / bare metal</b></summary>

- `kubectl apply -f deploy/kubernetes/pocket-invoicing.yaml` (PVC + Deployment + Service + Ingress)
- Bare metal: grab a binary from [Releases](https://github.com/abjelosevic88/pocket-homelab-invoicing/releases) and use `deploy/systemd/pocket-invoicing.service`.
</details>

## Configuration

Everything is an environment variable; all are optional. The important ones:

| Variable | Default | Purpose |
|---|---|---|
| `BASE_URL` | `http://localhost:8080` | Public URL used in emails and client links |
| `DATA_DIR` | `/data` | Database, uploads, backups |
| `AUTH_MODE` | `local` | `local` (built-in login), `proxy` (trust `Remote-User` from Authelia/Authentik), `none` (LAN only!) |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | – | Create the admin without the wizard |
| `PDF_ENGINE` | `native` | `native` (pure Go) · `chromium` · `gotenberg` |
| `EXCHANGE_RATE_PROVIDER` | `frankfurter` | ECB rates via frankfurter.app, or `none` for fully offline |
| `SCHEDULER_INTERVAL` | `15m` | Recurring invoices, overdue marking, reminders, rate refresh |
| `TRUST_PROXY` | `true` | Honour `X-Forwarded-*` headers |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `LOG_FORMAT` | `text` | `text` or `json` |

Full list with explanations: [docs/configuration.md](docs/configuration.md).
Reverse-proxy recipes (Caddy, Traefik, NPM, nginx, Authelia): [docs/reverse-proxy.md](docs/reverse-proxy.md).

## How billing modes work

| You charge… | Set the client to | What happens |
|---|---|---|
| per hour | **Hourly** | Timer/time entries are summed, rounded to your block size, and billed at the client's hourly rate. |
| per day | **Daily** | Tracked hours are converted with `hours_per_day` (default 8) into fractional days, or you type days directly. |
| a monthly retainer | **Monthly retainer** | Create a *Recurring* profile: one line "Retainer – {month}", unit *month*, quantity 1. The scheduler issues (and optionally emails) it every month, with the service period printed. Overages can be added as extra hourly lines. |
| a project price | **Fixed price** | Single line, unit *fixed*. Partial payments / milestones are recorded as payments. |

Details and examples: [docs/billing-modes.md](docs/billing-modes.md).

## Multi-currency in practice

1. Pick a **base currency** (Settings → Currencies). Reports and the dashboard are shown in it.
2. Enable the currencies you invoice in. Each client has a default currency.
3. Rates are fetched automatically from the ECB every 12h, or enter them manually (manual rates are never overwritten). Fully offline? `EXCHANGE_RATE_PROVIDER=none`.
4. When you create an invoice the current rate is **locked** on that invoice, so reports stay consistent even if rates move later. You can override it per invoice.
5. Payments in a different currency are converted at the stored or a typed rate; the invoice shows what was applied.

## Templates & PDF

- **Native engine (default):** pure Go, zero dependencies, Unicode (DejaVu embedded, Cyrillic/Greek/Latin-extended fine). Layout + accent colour + labels configurable in the UI.
- **Chromium / Gotenberg engines:** render the customisable HTML template (Go `html/template`). Use `ghcr.io/…:latest-chromium` or `docker compose --profile gotenberg up -d`.
- Localise labels per template: Settings → Templates → Labels.

See [docs/templates.md](docs/templates.md) and, for Word templates, [docs/word-templates.md](docs/word-templates.md) (samples in `docs/templates/`).

## API & webhooks

```bash
# create a token under Settings → API tokens, then:
curl -H "Authorization: Bearer pi_…" https://invoices.example.com/api/v1/invoices?status=overdue
curl -X POST -H "Authorization: Bearer pi_…" -d '{"client_id":1,"description":"patching"}' …/api/v1/time/start
```

Webhooks fire on `invoice.created|sent|viewed|paid|overdue…`, `payment.created`, `client.*` with an HMAC signature. Reference: [docs/api.md](docs/api.md).

## Backups

Download a consistent `.db` snapshot from **Settings → Backup**, via `GET /api/v1/backup`, or from cron:

```bash
docker compose exec app pocket-invoicing backup -o /data/backups/nightly.db
```

Restore = stop the container, replace `data/pocket-invoicing.db`, start. See [docs/backup.md](docs/backup.md).

## Migrating from Invoice Ninja

See [docs/migrating-from-invoice-ninja.md](docs/migrating-from-invoice-ninja.md): two SQL exports plus `scripts/import_invoiceninja.py` bring over clients, invoices, payments and attached documents.

## Development

```bash
make dev        # Go API on :8080 + Vite with hot reload on :5173
make test       # go vet, go test, tsc
make all        # build web + single binary ./pocket-invoicing
make docker     # local image
```

Stack: Go 1.26 · chi · SQLite (modernc, no CGO) · go-pdf/fpdf · React 19 + Vite + TypeScript, no CSS framework.

```
cmd/pocket-invoicing   entrypoint, CLI (serve | backup | scheduler | version)
internal/server        HTTP API, auth, business logic, scheduler
internal/store         SQLite schema, migrations, queries, reports
internal/pdf           document model, native renderer, HTML template, chromium/gotenberg
internal/money         decimal-safe totals & formatting
web/                   React SPA (embedded into the binary)
deploy/                unraid, kubernetes, reverse-proxy, systemd
```

## Roadmap

Quotes/estimates with "convert to invoice" · credit notes · Stripe/PayPal pay-now links on the public page · OIDC login · i18n of the UI itself · multi-business profiles. PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE). Fonts: DejaVu (Bitstream Vera license).
