# Homelab integration: backups, monitoring, dashboard

Pocket Invoicing is one container, one SQLite file and one data folder, so it
slots into the usual homelab tooling with a few lines of config. This page
collects the recipes.

## Backups

Everything lives under the data directory (`/data` in the container):

| Path | What | How to back up |
|---|---|---|
| `pocket-invoicing.db` (+ `-wal`, `-shm`) | all records and settings | **never copy the raw file** while the app runs; take a snapshot (below) |
| `attachments/` | uploaded invoice files | plain files, copy as-is |
| `templates/` | uploaded Word templates | plain files, copy as-is |
| `backups/` | snapshots made from the UI / API / CLI | plain files |

The database runs in WAL mode and is written on every request. A file-level
copy taken mid-write can be corrupt, so produce a consistent snapshot first and
back that up. Any of these is fine:

```bash
# built-in CLI (uses VACUUM INTO, safe while running)
docker compose exec -T app pocket-invoicing backup -o /data/backups/nightly.db

# HTTP (needs an API token), streams a snapshot
curl -H "Authorization: Bearer $TOKEN" -o nightly.db https://billing.example.com/api/v1/backup

# host-side, using sqlite's online backup API
python3 - <<'PY'
import sqlite3
src = sqlite3.connect("file:/srv/pocket-invoicing/data/pocket-invoicing.db?mode=ro", uri=True)
dst = sqlite3.connect("/srv/backups/staging/pocket-invoicing-db.sqlite3")
src.backup(dst)
PY
```

### restic / Backrest

Add the data folder to your plan and **exclude the live database files**; the
snapshot you produce in a pre-backup hook replaces them.

```
paths:    /srv/pocket-invoicing
excludes: /srv/pocket-invoicing/data/pocket-invoicing.db*
hook (CONDITION_SNAPSHOT_START): /srv/backups/bin/prep-backup.sh   # produces the snapshot above
```

Restore: stop the container, copy the snapshot back to `data/pocket-invoicing.db`,
delete any stale `-wal`/`-shm` files, start the container. Migrations run
automatically if the backup came from an older version.

## Monitoring

| Endpoint | Auth | Use for |
|---|---|---|
| `GET /healthz` | none | liveness: process is up |
| `GET /readyz` | none | readiness: database answers; returns 503 otherwise |
| `GET /metrics` | none (disable with `METRICS_ENABLED=false`) | Prometheus text format |

**Uptime Kuma**: an HTTP monitor on `https://billing.example.com/readyz`,
expecting 200-299, catches both a dead container and a broken database. If
Kuma runs on the same box you can add it over the socket API without clicking
through the UI:

```js
// docker exec -e KUSER=admin -e KPASS=... uptime_kuma node /tmp/add.js
const { io } = require('/app/node_modules/socket.io-client');
const s = io('http://localhost:3001', { transports: ['websocket'] });
s.on('connect', () => s.emit('login', { username: process.env.KUSER, password: process.env.KPASS, token: '' }, () =>
  s.emit('add', { type: 'http', name: 'Pocket Invoicing', url: 'https://billing.example.com/readyz',
    method: 'GET', interval: 60, retryInterval: 30, maxretries: 2, timeout: 48, maxredirects: 10,
    accepted_statuscodes: ['200-299'], notificationIDList: { '1': true } }, r => { console.log(r); process.exit(0) })));
```

**Prometheus**: scrape `/metrics`. Exposed gauges include outstanding and
overdue totals, invoice/client/payment counts and scheduler runs.

**Beszel / Dozzle / cAdvisor** pick the container up automatically; nothing to
configure.

## Homepage dashboard card

Create a token under *Settings → API tokens*, put it in Homepage's `.env` as
`HOMEPAGE_VAR_POCKET_INVOICING_TOKEN`, pass it through in the compose
`environment:` block, then:

```yaml
- Pocket Invoicing:
    href: https://billing.example.com/
    icon: mdi-receipt-text
    server: local                 # docker status badge
    container: pocket-invoicing
    widget:
      type: customapi
      url: http://pocket-invoicing:8080/api/v1/dashboard   # or http://host:8168/...
      refreshInterval: 300000
      headers:
        Authorization: Bearer {{HOMEPAGE_VAR_POCKET_INVOICING_TOKEN}}
      mappings:
        - field: { stats: outstanding }
          label: Outstanding
          format: number
          suffix: " KM"
        - field: { stats: overdue }
          label: Overdue
          format: number
          suffix: " KM"
        - field: { stats: paid_this_year }
          label: Paid this year
          format: number
          suffix: " KM"
        - field: { stats: draft_count }
          label: Drafts
```

Amounts are in the base currency at each invoice's locked exchange rate. Other
useful fields on the same endpoint: `stats.overdue_count`,
`stats.unbilled_minutes`, `lifetime.paid_total`, `by_year[]`, `by_client[]`.

## Notifications

Webhooks (*Settings → Webhooks*) fire on invoice sent / viewed / paid / overdue
with an HMAC-signed JSON body. Point one at ntfy, Apprise, n8n or Gotify to get
a push when a client opens or pays an invoice.
