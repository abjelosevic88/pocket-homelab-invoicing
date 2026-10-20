# Documents and Paperless-ngx

## Company documents

**Documents** (sidebar → Work) is a single place for everything that is not an invoice: contracts, company registration, tax decisions, bank letters, insurance, certificates, correspondence.

- Upload one or many files at once (any type, 50 MB each). Each gets a title, a free-form **category** (suggestions are offered, type your own), an optional **client**, a document date, optional **expiry date** and notes.
- Documents that expire within 30 days are listed in a banner on the page; expired ones are marked red.
- Search covers title, file name, notes, category and client name. Filter by category or client.
- A client's documents also show on the client page.
- Files live in `DATA_DIR/documents/` and are part of the data folder backup.

API: `GET/POST /api/v1/documents`, `GET/PUT/DELETE /api/v1/documents/{id}`, `GET /api/v1/documents/{id}/file`, `POST /api/v1/documents/{id}/file` (replace), `GET /api/v1/documents/categories`. Upload is multipart with one or more `file` fields plus `title`, `category`, `client_id`, `doc_date`, `expires_at`, `notes` and `paperless=1` to archive immediately.

## Paperless-ngx (optional)

Nothing in the app depends on it. Paperless is **per person**: everything goes into the signed-in person's own Paperless account, never into someone else's.

### Connect

Settings → Paperless has a switch with three positions; it is saved as soon as you flip it.

| Mode | How it reaches Paperless |
|---|---|
| **Pocket Core** | On a box with [Pocket Core](https://github.com/abjelosevic88/pocket-homelab-core): Core hands out the Paperless address and **your own** token there (set in Core by hand or by its Paperless connector). An admin without a token of their own gets the box's token (Core's rule). Asked again on every use, so switching it off in Core (Links, or Settings → Apps acting for me) applies at once. Nothing to paste. |
| **Direct** | The URL and API token you save here (how it always worked). |
| **Off** | The app does not talk to Paperless at all. |

With no choice stored the mode is **Direct** when a token is saved (existing setups keep working), else **Pocket Core** when the box has one, else Direct. The card shows the state: *Connected through Pocket Core (your own key)* / *(the box's key)*, a direct connection, or *Not connected:* with Core's reason (switched off for the box or by you, no account of your own there yet, no Paperless in Core, …).

Direct:

1. In Paperless-ngx open your profile (top right) and create an **API token**.
2. Settings → Paperless → Direct: enter the URL as seen from the Pocket Invoicing container (for Docker on the same host usually `http://host.docker.internal:PORT` or the container name on a shared network) and the token. Optionally a **browser URL** (for example `https://paperless.example.org`) used for the links you click. Save.
3. **Test connection** shows the Paperless version and document count (a token typed into the form is tried before it is saved). **Disconnect** removes the saved URL and token.

Environment variables `PAPERLESS_URL` and `PAPERLESS_TOKEN` are one box-wide credential, so they are the **owner's** (first admin): on the first start with an account they are copied into the owner's Direct settings, **once**. After that only the saved settings count, so a Disconnect stays disconnected across restarts.

API: `GET /api/v1/settings` carries `paperless_mode` (in effect: `core` | `direct` | `off`; a PUT stores it), `paperless_via` (`core` | `direct` | empty when not connected), `paperless_source` (`personal` | `box`, whose key Core handed out last) and `paperless_detail` (why not). `POST /api/v1/settings/test-paperless` with `{url, token}` tries those; with `{}` it follows the mode.

### What it does once connected

| Where | What |
|---|---|
| Invoice page | **Archive to Paperless** sends the invoice. Which files go depends on *Settings → Invoicing → Email attachment*: the generated PDF, your uploaded files (e.g. the fiscalised copy), or both. Each uploaded file also has its own ⇪ button. Once archived the button becomes **In Paperless ↗**. |
| Automatic | *Archive invoices automatically* = when marked as sent, when paid, or never. Runs in the background after the status change; already archived parts are skipped. |
| Documents | **Send to Paperless** per document, or tick *Also send to Paperless* while uploading. The category becomes the Paperless document type (toggle), the client becomes the correspondent (toggle). |
| Documents → Paperless tab | Full-text search of your archive with thumbnails, open in Paperless, download through the app, and **copy into Documents** (⇩), which pulls the file and keeps the link. |
| Clients | Each client can be linked to a Paperless **correspondent** (Clients → edit → "Paperless correspondent": pick one or "Create … in Paperless"). The client page then shows that correspondent's documents with a link to the filtered view in Paperless, and everything archived for the client is filed under it. Settings → Paperless → **Link clients to correspondents** matches unlinked clients by name (exact, or one name contained in the other, e.g. "Athena Studio S.à r.l." → "Athena Studio") and optionally creates the missing ones. |
| Metadata | Every upload gets the configured **tags** (created if missing), invoices get the configured **document type** (default "Invoice"), the created date is the invoice/document date, the title is `Invoice NUMBER – Client`. |

Paperless consumes uploads asynchronously. The app records the task, polls it for a few minutes, and the scheduler finishes any that are still pending. Until then the badge reads "Paperless…"; a duplicate or OCR failure shows as "Paperless failed" with the reason on hover.

Links are stored on this side only. Deleting a document or invoice here never deletes anything in Paperless, and `DELETE /api/v1/paperless/links/{id}?kind=document|invoice|attachment` forgets a link without touching Paperless.

### API

```
GET  /api/v1/paperless/status                      configured, ok, version, document_count, url
POST /api/v1/settings/test-paperless               {url, token}
GET  /api/v1/paperless/documents?q=&page=          search (also correspondent__id, document_type__id, tags__id__all, created__date__gte/lte)
GET  /api/v1/paperless/names                       tags, correspondents, document types
GET  /api/v1/paperless/documents/{pid}/file        proxied download (?download=1, ?original=1)
GET  /api/v1/paperless/documents/{pid}/thumb       proxied thumbnail
GET  /api/v1/paperless/correspondents              [{id, name}]  (?refresh=1)
POST /api/v1/paperless/correspondents              {name, client_id?} → create or return existing, optionally link the client
POST /api/v1/paperless/correspondents/sync         {create} → {matched, created, unmatched}
POST /api/v1/paperless/documents/{pid}/import      {category, client_id, original} → company document
POST /api/v1/documents/{id}/paperless              (?force=1 to re-send)
POST /api/v1/invoices/{id}/paperless               {source: generated|uploaded|both}  (empty = email attachment mode)
POST /api/v1/attachments/{aid}/paperless
```

Endpoints answer `409` when Paperless is not configured.

### Notes for the homelab

- The app only needs HTTP access to Paperless; Paperless never calls back.
- The token is stored in the settings table of the SQLite database, so protect backups accordingly. The settings API never echoes it (`paperless_token_set: true` tells the UI one exists).
- Paperless's own duplicate check applies: sending the same file twice results in a "duplicate" failure rather than a second copy.
