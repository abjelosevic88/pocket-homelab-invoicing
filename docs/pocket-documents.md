# Documents and Pocket Documents

## Company documents

**Documents** (sidebar → Work) is a single place for everything that is not an invoice: contracts, company registration, tax decisions, bank letters, insurance, certificates, correspondence.

- Upload one or many files at once (any type, 50 MB each). Each gets a title, a free-form **category** (suggestions are offered, type your own), an optional **client**, a document date, optional **expiry date** and notes.
- Documents that expire within 30 days are listed in a banner on the page; expired ones are marked red.
- Search covers title, file name, notes, category and client name. Filter by category or client.
- A client's documents also show on the client page.
- Files live in `DATA_DIR/documents/` and are part of the data folder backup.

API: `GET/POST /api/v1/documents`, `GET/PUT/DELETE /api/v1/documents/{id}`, `GET /api/v1/documents/{id}/file`, `POST /api/v1/documents/{id}/file` (replace), `GET /api/v1/documents/categories`. Upload is multipart with one or more `file` fields plus `title`, `category`, `client_id`, `doc_date`, `expires_at`, `notes` and `pocket_documents=1` to file them in Pocket Documents right away.

## Pocket Documents (optional)

[Pocket Documents](https://github.com/abjelosevic88/pocket-homelab-documents) is the archive of the Pocket apps (it replaced Paperless-ngx here). Nothing in Pocket Invoicing depends on it. It is **per person**: everything goes into the signed-in person's own Pocket Documents, never into someone else's, and the client page only lists that person's documents.

### Connect

Settings → Pocket Documents has a switch; it is saved as soon as you flip it. On a box with Pocket Core it shows **Pocket Core | Off**, with *Advanced: connect directly with a URL and token* revealing **Direct** (always shown while Direct is the current mode). Without a Core all three positions show.

| Mode | How it reaches Pocket Documents |
|---|---|
| **Pocket Core** | A Pocket Core **app link** (Core 0.5+ links, see Core's `docs/links.md`): the manifest declares `uses: [{app: documents, scopes: [documents:read, documents:write]}]`, and for each piece of work Invoicing asks Core for a five-minute `pcx_` token that acts **as you** in Pocket Documents (`ActAs`). Core also says where Documents lives. Nothing to paste; switching the link off in Core (Links, or Settings → Apps acting for me) stops the next action at once. |
| **Direct** | A URL and **your own** Pocket Documents API token (`pd_…`, Pocket Documents → Settings → API tokens) saved here. The URL may stay empty when Pocket Core can say where Documents is, or `DOCUMENTS_URL` is set. |
| **Off** | The app does not talk to Pocket Documents at all. |

Pocket Invoicing reports each person's mode in effect to Core (`PUT /api/v1/links/modes`, target `documents`) after registering, every 30 minutes and right after someone switches or disconnects, so Core's Links page shows Direct connections too. Report only; Core decides nothing on it.

With no choice stored the mode is **Direct** when a token is saved, else **Pocket Core** when the box has one, else Direct. The card shows the state: *Connected through a Pocket Core app link*, a direct connection, or *Not connected:* with Core's reason (the link switched off for the box or by you, no access to Pocket Documents, Documents not registered, …).

Direct:

1. In Pocket Documents open Settings → API tokens and create a token.
2. Settings → Pocket Documents → Direct: enter the URL as seen from the Pocket Invoicing container (e.g. `http://pocket-documents:8080`, or leave it empty with Pocket Core / `DOCUMENTS_URL`) and the token. Optionally a **browser URL** (for example `https://documents.example.org`) used for the links you click. Save.
3. **Test connection** shows your document count (a token typed into the form is tried before it is saved). **Disconnect** removes the saved URL and token.

API: `GET /api/v1/settings` carries `documents_mode` (in effect: `core` | `direct` | `off`; a PUT stores it), `documents_via` (`core` | `direct` | empty when not connected) and `documents_detail` (why not); the token is write-only (`documents_token_set` says one is stored, `documents_clear_token: true` removes it). `POST /api/v1/settings/test-documents` with `{url, token}` tries those; with `{}` it follows the mode.

### What it does once connected

| Where | What |
|---|---|
| Invoice page | **File in Documents** files the invoice. Which files go depends on *Settings → Invoicing → Email attachment*: the generated PDF, your uploaded files (e.g. the fiscalised copy), or both. Each uploaded file also has its own **To Documents** button. Once filed the button becomes **In Documents ↗**. |
| Automatic | *File invoices automatically* = when marked as sent, when paid, or never. Runs in the background after the status change; parts already filed are skipped. |
| Company documents | **To Documents** per document, or tick *Also file in Pocket Documents* while uploading. The category becomes the document type (toggle), the client the correspondent. |
| Documents → Pocket Documents tab | Search of your archive (Pocket Documents' search syntax: `from:client`, `tag:x`, `created:2026` …) with thumbnails, open in Pocket Documents, download through the app, and **Import** into Company documents, which pulls the file and keeps the link. |
| Clients | Each client can be linked to a **correspondent** in Pocket Documents (Clients → edit → "Pocket Documents correspondent": pick one or "Create … in Pocket Documents"). The link is kept **by name** and the id is looked up when needed, so it survives a re-import or a second instance. The client page lists that correspondent's newest documents (thumbnails through Invoicing) with a link to the filtered view in Pocket Documents, and everything filed for the client goes under it. Without a linked correspondent, files go under the client's own name (toggle; created if missing). Settings → Pocket Documents → **Link clients to correspondents** matches unlinked clients by name (exact, or one name contained in the other, e.g. "Athena Studio S.à r.l." → "Athena Studio") and optionally creates the missing ones. |
| Metadata | Every upload gets the configured **tags** (created if missing), invoices get the configured **document type** (default "Invoice"), the created date is the invoice/document date, the title is `Invoice NUMBER – Client`, and the **external id** is `invoicing:invoice:<id>` (`invoicing:attachment:<id>`, `invoicing:document:<id>`). |

Pocket Documents answers an upload at once with the new document (OCR runs afterwards there), so the link is there right away. Filing the same record again is idempotent: Documents answers `409` with `duplicate_of` for the same external id (or the same file) and Invoicing links that document instead of making a copy. A failed attempt shows "Not filed" with the reason on hover, and the button stays.

Links are stored on this side only. Deleting a document or invoice here never deletes anything in Pocket Documents, and `DELETE /api/v1/pocket-documents/links/{id}?kind=document|invoice|attachment` forgets a link without touching Pocket Documents.

### Coming from Paperless-ngx

- `PAPERLESS_URL` / `PAPERLESS_TOKEN` are ignored (a warning is logged at startup when they are still set) and Settings → Paperless is gone; the Paperless URL and token are not carried over, because Pocket Documents is a new connection.
- The archiving preferences (tags, invoice type, automatic filing, correspondent toggles) carry over to Settings → Pocket Documents.
- A client's Paperless correspondent carries over **by name** (migration `0013`); Pocket Documents' Paperless import recreated the correspondents by name with new ids.
- Records filed in Paperless are linked again by the scheduler: a document imported from Paperless into Pocket Documents carries the external id `paperless:<id>`, so the old link finds it. One that is not there is shown as "Not filed" (file it again). `paperless_links` and the `paperless_correspondent*` columns stay in the database for history.

### API

```
GET    /api/v1/pocket-documents/status                    configured, mode, via, detail, ok, document_count, url
POST   /api/v1/settings/test-documents                    {url, token}
GET    /api/v1/pocket-documents/documents?q=&page=&page_size=      search (Pocket Documents' search syntax)
GET    /api/v1/pocket-documents/documents?client_id=      the client's documents (by its correspondent): + correspondent, correspondent_id, url
GET    /api/v1/pocket-documents/documents/{pid}/file      proxied download (?download=1, ?original=1)
GET    /api/v1/pocket-documents/documents/{pid}/thumb     proxied thumbnail
GET    /api/v1/pocket-documents/correspondents            [{id, name}]  (?refresh=1)
POST   /api/v1/pocket-documents/correspondents            {name, client_id?} → create or return existing, optionally link the client
POST   /api/v1/pocket-documents/correspondents/sync       {create} → {matched, created, unmatched}
POST   /api/v1/pocket-documents/documents/{pid}/import    {category, client_id, original} → company document
DELETE /api/v1/pocket-documents/links/{id}?kind=          forget a link
POST   /api/v1/documents/{id}/pocket-documents            (?force=1 to file again)
POST   /api/v1/invoices/{id}/pocket-documents             {source: generated|uploaded|both}  (empty = email attachment mode)
POST   /api/v1/attachments/{aid}/pocket-documents
```

Invoices, attachments and company documents carry `pocket_documents: {kind, ref_id, documents_id, title, error?, url}` once filed. Endpoints answer `409` when Pocket Documents is not connected.

What Invoicing calls in Pocket Documents (all allowed for a link token with `documents:read` / `documents:write`): `GET /api/v1/documents` (`q`, `correspondent`, `external_id`, `limit`, `offset`, `sort=-created`, `no_facets=1`), `GET /api/v1/documents/{id}`, `…/download`, `…/thumb`, `POST /api/v1/documents` (multipart `document` + `title`, `created`, `correspondent`, `document_type`, `tags`, `external_id`), `GET /api/v1/taxonomy`, `POST /api/v1/correspondents|tags|document_types`.

### Notes for the homelab

- The app only needs HTTP access to Pocket Documents; Documents never calls back.
- A direct token is stored in the settings table of the SQLite database, so protect backups accordingly. The settings API never echoes it. Through Pocket Core nothing is stored: link tokens live five minutes and are asked for per piece of work.
- The browser never gets a token: files and thumbnails go through Invoicing's own endpoints.
