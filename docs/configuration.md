# Configuration

All configuration is via environment variables. Nothing is required; `docker compose up` works with defaults. Settings that are business-related (company details, numbering, SMTP, rates…) live in the database and are edited in the UI under **Settings**.

## Server

| Variable | Default | Description |
|---|---|---|
| `HOST` | `0.0.0.0` | Listen address |
| `PORT` | `8080` | Listen port |
| `BASE_URL` | `http://localhost:<PORT>` | Public URL. Used to build public invoice links in emails. Set this when behind a reverse proxy. |
| `DATA_DIR` | `./data` (`/data` in Docker) | Holds the SQLite database, `uploads/` (logo) and `backups/` |
| `DB_PATH` | `$DATA_DIR/pocket-invoicing.db` | Database file |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | `text` or `json` (for Loki/ELK) |
| `TRUST_PROXY` | `true` | Use `X-Forwarded-For` / `X-Real-IP` for client IPs |
| `METRICS_ENABLED` | `true` | Expose Prometheus metrics at `/metrics` (unauthenticated) |
| `SECURE_COOKIES` | `false` | Mark the session cookie `Secure`. Enable when served over HTTPS. |
| `SESSION_TTL` | `720h` | Session lifetime (Go duration) |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | – | Sign in with an OpenID Connect provider (PocketID, Authelia, Authentik, Keycloak…). Needs `BASE_URL`; the callback is `BASE_URL/api/v1/auth/oidc/callback`. On a box with Pocket Core none of these are needed — the app receives its client when it registers. |
| `OIDC_NAME` | `single sign-on` | Button label: “Sign in with …” (“Pocket Core” when the client came from Core) |
| `OIDC_AUTO_CREATE` | `true` | Give an unknown (verified) email an account instead of refusing it. Always on for the Core-issued client |

## Authentication

| Variable | Default | Description |
|---|---|---|
| `AUTH_MODE` | `local` | `local` — built-in email/password login with the first-run wizard.<br>`proxy` — trust an identity header set by your SSO reverse proxy (Authelia, Authentik, oauth2-proxy, Caddy `forward_auth`). Users are auto-provisioned.<br>`none` — no authentication at all. Only for isolated LAN/VPN setups. |
| `AUTH_PROXY_HEADER` | `Remote-User` | Header carrying the username in `proxy` mode |
| `AUTH_PROXY_EMAIL_HEADER` | `Remote-Email` | Header carrying the email in `proxy` mode |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | If set, the admin user is created on first start and the setup wizard is skipped. Handy for IaC. | Lost the password of an existing user? `pocket-invoicing reset-password [-email USER] [-password NEW]` inside the container sets a new one (random and printed if `-password` is omitted).

API tokens (`Authorization: Bearer pi_…` or `X-API-Key`) work in every mode.

> **Important for proxy mode:** public invoice links (`/i/*`, `/api/public/*`) and `/healthz` should bypass your SSO if clients need to open them. See `deploy/reverse-proxy/Caddyfile`.

## PDF

| Variable | Default | Description |
|---|---|---|
| `PDF_ENGINE` | `native` | `native` — pure Go renderer, embedded Unicode fonts, zero dependencies.<br>`chromium` — runs a headless Chromium binary (`CHROMIUM_PATH`) to print the HTML template. Use the `-chromium` image tag.<br>`gotenberg` — posts the HTML template to a [Gotenberg](https://gotenberg.dev) container (`GOTENBERG_URL`). |
| `CHROMIUM_PATH` | `chromium` | Binary path for the chromium engine |
| `GOTENBERG_URL` | `http://gotenberg:3000` | Gotenberg base URL (also used for Word templates) |
| `DOCX_CONVERTER` | `gotenberg` | How Word templates become PDFs: `gotenberg`, `libreoffice` (local `LIBREOFFICE_PATH`), or `none` (download the filled .docx only) |

## Currencies

| Variable | Default | Description |
|---|---|---|
| `EXCHANGE_RATE_PROVIDER` | `frankfurter` | Default rate source; Settings → Currencies can override it.<br>`frankfurter` — ECB reference rates from api.frankfurter.app (free, no key, ~30 currencies).<br>`cbbh` — official daily list of the Central Bank of Bosnia and Herzegovina (middle rate, 17 currencies against BAM); invoices lock the rate valid on their issue date.<br>`none` — never call out; use manual rates only. |
| `EXCHANGE_RATE_REFRESH` | `12h` | How often the scheduler refreshes rates |

Manual rates entered in the UI are never overwritten by the provider. Currencies not covered by the ECB (e.g. RSD, BTC) need manual rates.

## Scheduler

| Variable | Default | Description |
|---|---|---|
| `SCHEDULER_ENABLED` | `true` | Run background jobs in-process |
| `SCHEDULER_INTERVAL` | `15m` | Tick interval |

Jobs per tick: generate due recurring invoices (catching up missed periods), mark overdue invoices, send payment reminders (if enabled in Settings → Email), refresh exchange rates, purge expired sessions. You can also trigger a run with `POST /api/v1/scheduler/run` or `pocket-invoicing scheduler` (useful with an external cron and `SCHEDULER_ENABLED=false`).

## Misc

| Variable | Default | Description |
|---|---|---|
| `DEMO_DATA` | `false` | Seed sample clients, invoices, time entries on first start (only if the DB is empty) |
| `TZ` | `UTC` | Container timezone (affects "today" for due dates and schedules) |
| `CORE_URL` / `CORE_REGISTRATION_KEY` | – | [Pocket Core](https://git.bjelke.org/abjelosevic88/pocket-homelab-core): register on start (the key is needed once; the minted app token lives in `$DATA_DIR/core.token`), publish `invoicing.invoice_created` / `invoicing.invoice_sent` / `invoicing.invoice_paid`, and accept the tokens Core hands to paired phones |
| `CORE_APP_URL` | `BASE_URL` | How Core reaches this app for health checks (e.g. `http://host.docker.internal:8168`) |

## CLI

```
pocket-invoicing                 # serve (default)
pocket-invoicing backup [-o F]   # consistent SQLite copy (VACUUM INTO)
pocket-invoicing scheduler       # run jobs once and exit
pocket-invoicing version
```

## Pocket Documents (optional)

| Variable | Default | Description |
|---|---|---|
| `DOCUMENTS_URL` | (empty) | Pocket Documents as reached from the container (e.g. `http://pocket-documents:8080`), for **direct** connections of people who saved no URL of their own and when Pocket Core cannot say. Tokens are personal (`pd_…`, Settings → Pocket Documents), so there is no box-wide one. On a box with Pocket Core nothing is needed: each person's link goes through Core. |
| `PAPERLESS_URL` / `PAPERLESS_TOKEN` | – | **Ignored** since Pocket Documents replaced Paperless-ngx; a warning is logged at startup while they are set. Remove them. |

See [pocket-documents.md](pocket-documents.md).
