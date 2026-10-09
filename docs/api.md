# REST API

Base path: `/api/v1`. All responses are JSON. Authenticate with a session cookie (browser) or a personal access token created under **Settings → API tokens**:

```
Authorization: Bearer pi_xxxxxxxx
# or
X-API-Key: pi_xxxxxxxx
```

Errors: `{"error": "message"}` with a 4xx/5xx status. Dates are `YYYY-MM-DD`; timestamps RFC 3339 (UTC). Amounts are JSON numbers in the document's currency.

## Auth
| Method | Path | Notes |
|---|---|---|
| GET | `/auth/status` | `{needs_setup, authenticated, auth_mode, user, version}` |
| POST | `/auth/setup` | First user `{email, password, name, company_name, base_currency}` |
| POST | `/auth/login` | `{email, password}` → sets cookie |
| POST | `/auth/logout` | |
| GET/PUT | `/auth/me` | Profile; PUT `{name, email, current_password, new_password}` |
| GET/POST/DELETE | `/tokens`, `/tokens/{id}` | API tokens (plaintext returned once on create) |
| GET/POST/PUT/DELETE | `/users`, `/users/{id}` | User management |

## Clients
| Method | Path |
|---|---|
| GET | `/clients?q=&archived=1` |
| POST | `/clients` |
| GET | `/clients/{id}` → `{client, invoices, recurring, unbilled_minutes}` |
| PUT | `/clients/{id}` |
| DELETE | `/clients/{id}` (archives if it has invoices) |

Client fields: `name, contact_name, email, phone, address1, address2, city, state, postal_code, country, tax_id, website, currency, billing_mode (hourly|daily|monthly|fixed), default_rate, payment_terms_days, template_id, email_subject, email_body, email_cc, notes, archived`.

`email_subject` / `email_body` (per client, and the global ones in settings) accept the placeholders `{number} {number_short} {client} {contact} {first_name} {company} {total} {balance} {due_date} {issue_date} {month} {year} {period} {link}`. `{month}`/`{year}` come from the service period start (or the issue date), `{first_name}` is the first word of the contact name, `{number_short}` drops an alphabetic prefix (`INV-005-2026` → `005-2026`).

## Invoices
| Method | Path | Notes |
|---|---|---|
| GET | `/invoices?status=&client_id=&q=&from=&to=&limit=&offset=` | `status`: draft, sent, viewed, partial, paid, overdue, cancelled, `open` (any unpaid) |
| POST | `/invoices` | see body below |
| GET | `/invoices/next-number` | |
| GET | `/invoices/check-number?number=&client_id=&exclude=` | `{same_client, other_clients[]}` — numbers are unique per client |
| POST | `/invoices/from-time` | `{client_id, from, to, entry_ids[], group_by: entry|day|project|total, billing_mode: hourly|daily, rate, include_expenses}` |
| GET | `/invoices/{id}` | includes `items[]`, `payments[]` |
| PUT | `/invoices/{id}` | same body as POST; recalculates totals |
| DELETE | `/invoices/{id}` | |
| POST | `/invoices/{id}/status` | `{status: draft|sent|paid|cancelled}` ("paid" records the remaining balance as a payment) |
| POST | `/invoices/{id}/send` | `{to?, subject?, body?}` — emails PDF, marks sent |
| POST | `/invoices/{id}/duplicate` | new draft |
| GET | `/invoices/{id}/pdf?download=1` | |
| GET | `/invoices/{id}/html` | |
| GET | `/invoices/{id}/emails` | email log |

Create/update body:
```json
{
  "client_id": 1,
  "number": "",                      // blank = auto
  "status": "draft",                 // or "sent"
  "issue_date": "2026-10-01",
  "due_date": "",                    // blank = client terms
  "currency": "USD",                 // blank = client currency
  "exchange_rate": 0,                // 0 = stored rate to base currency
  "billing_mode": "hourly",
  "period_start": "2026-09-01", "period_end": "2026-09-30",
  "po_number": "PO-42",
  "discount_type": "percent", "discount_value": 5,
  "notes": "", "terms": "", "footer": "",
  "template_id": null,
  "items": [
    {"description": "Consulting", "unit": "hour", "quantity": 10, "unit_price": 95, "tax_rate": 20, "discount": 0}
  ],
  "time_entry_ids": [12, 13]         // optional: link entries
}
```

## Payments
| Method | Path | Notes |
|---|---|---|
| GET | `/payments?invoice_id=&client_id=&from=&to=` | |
| POST | `/payments` | `{invoice_id, amount, currency?, exchange_rate?, date?, method?, reference?, notes?}` — currency may differ from the invoice |
| DELETE | `/payments/{id}` | |

## Recurring
`GET /recurring`, `POST /recurring`, `GET/PUT/DELETE /recurring/{id}`, `POST /recurring/{id}/run` (generate now).

Body: `{name, client_id, status: active|paused, frequency: daily|weekly|biweekly|monthly|quarterly|yearly, interval, start_date, end_date, next_run, max_occurrences, due_days, currency, billing_mode, items[], discount_type, discount_value, notes, terms, auto_send, template_id}`. Item descriptions may contain `{month}`, `{year}`, `{period}`.

## Time tracking
| Method | Path | Notes |
|---|---|---|
| GET | `/time?client_id=&unbilled=1&from=&to=` | |
| POST | `/time` | `{client_id, project, description, started_at, duration_minutes | ended_at, billable, rate}` |
| PUT/DELETE | `/time/{id}` | |
| GET | `/time/running` | |
| POST | `/time/start` | `{client_id, project, description, rate?}` (stops any running timer) |
| POST | `/time/stop` | rounds to the configured block |

## Expenses
`GET /expenses?client_id=&unbilled=1`, `POST /expenses`, `PUT/DELETE /expenses/{id}` — `{client_id, date, category, description, amount, currency, exchange_rate, billable}`.

## Settings & catalog
| Path | Notes |
|---|---|
| `GET/PUT /settings` | whole settings object; `smtp_password` is write-only (blank keeps current) |
| `POST /settings/logo` (multipart `logo`), `DELETE /settings/logo` | |
| `POST /settings/test-email` `{to}` | |
| `GET /settings/system` | runtime info |
| `GET /currencies?enabled=1`, `PUT /currencies/{code}` | `{name, symbol, decimals, enabled}` |
| `GET /rates?base=`, `PUT /rates` `{quote, rate}`, `DELETE /rates/{quote}`, `POST /rates/refresh`, `GET /rates/convert?from=&to=&amount=` | |
| `GET/POST/PUT/DELETE /tax-rates` | |
| `GET/POST/PUT/DELETE /products` | |
| `GET/POST/PUT/DELETE /templates`, `GET /templates/{id}/preview[.pdf]?layout=&accent=&invoice_id=`, `GET /templates/default-html` | |
| `POST /templates/docx` (multipart `file`, `name`, `is_default`), `POST /templates/{id}/docx` (replace file), `GET /templates/{id}/docx`, `GET /templates/placeholders` | Word templates |
| `GET /invoices/{id}/docx[?template_id=]` | filled Word document |
| `GET/POST/PUT/DELETE /webhooks`, `POST /webhooks/test` | |

## Reports
All accept `from`, `to` (default: last 12 months) and return base-currency figures.

`GET /dashboard` · `/reports/revenue` · `/reports/clients` · `/reports/aging` · `/reports/tax` · `/reports/currencies` · `/reports/time` · `/reports/export.csv?type=invoices|items|payments|time|expenses&from=&to=`

## Ops
| Path | Notes |
|---|---|
| `GET /healthz` | liveness (no auth) |
| `GET /readyz` | DB ping (no auth) |
| `GET /metrics` | Prometheus text format (no auth; disable with `METRICS_ENABLED=false`) |
| `GET /api/v1/backup` | download a consistent `.db` |
| `GET /api/v1/export.json` | everything as JSON |
| `POST /api/v1/scheduler/run` | run background jobs now |
| `GET /api/v1/activity?entity_type=&entity_id=` | audit log |

## Public (no auth)
`GET /i/{token}` (HTML), `GET /i/{token}/pdf`, `GET /api/public/invoices/{token}` (JSON). Opening a link marks a *sent* invoice as *viewed*.

## Webhooks
Configured under Settings → Webhooks. Each delivery is `POST` with:

```json
{"event": "invoice.paid", "timestamp": "2026-10-08T12:00:00Z", "data": { ...invoice... }}
```
Headers: `X-Webhook-Event`, `X-Webhook-Signature: sha256=<hex HMAC-SHA256 of body with your secret>`.

Events: `invoice.created`, `invoice.updated`, `invoice.sent`, `invoice.viewed`, `invoice.partial`, `invoice.paid`, `invoice.overdue`, `invoice.cancelled`, `invoice.draft`, `invoice.deleted`, `payment.created`, `client.created`, `client.updated`, `test`. Subscribe with `*`, a comma-separated list, or a prefix like `invoice.*`.

### Example: n8n / Home Assistant
```yaml
# Home Assistant webhook trigger → notify when an invoice is paid
automation:
  - trigger:
      platform: webhook
      webhook_id: pocket-invoicing
    condition: "{{ trigger.json.event == 'invoice.paid' }}"
    action:
      service: notify.mobile_app
      data:
        message: "💸 {{ trigger.json.data.client_name }} paid {{ trigger.json.data.number }}"
```

## Documents and Paperless-ngx

See [paperless.md](paperless.md) for the document store endpoints (`/api/v1/documents…`) and the Paperless proxy endpoints (`/api/v1/paperless/…`).
