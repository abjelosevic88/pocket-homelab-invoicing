# Word (.docx) templates

Design your invoice in Microsoft Word, LibreOffice or Google Docs, sprinkle in placeholders,
upload it under **Settings → Templates → + Word**, and Pocket Invoicing fills it for every
invoice. You can keep several templates (one per client, per language, per bank account …),
pick one per **client** (Clients → edit → Invoice template) or per **invoice**, and mark one as
the global default.

The filled document is converted to PDF by the **Gotenberg** container (LibreOffice inside),
which is part of the default `docker-compose.yml`. Without Gotenberg you can still download the
filled `.docx` from the invoice page ("Download as Word") and print it yourself.

Two ready-made templates live in [`docs/templates/`](templates/): `invoice-simple.docx` and
`invoice-wisestack.docx` (bordered header, HOURS/AMOUNT table, transfer details, base-currency
sentence, signature line). Download one, open it in Word, restyle it, upload it.

## Syntax

| Write in Word | Result |
|---|---|
| `{{number}}`, `{{client.name}}`, `{{custom.pfr}}` | Replaced with the value |
| `{{#items}} … {{/items}}` | Repeated per line item. Put `{{#items}}` in the **first cell** of a table row and `{{/items}}` in its **last cell** → that row is repeated. Also works across several rows, or inside a paragraph. |
| `{{#taxes}} … {{/taxes}}`, `{{#custom_fields}} … {{/custom_fields}}` | Same, per tax rate / per custom field (`{{label}}`, `{{amount}}` / `{{value}}`) |
| `{{#paid}} … {{/paid}}` | Shown only when the invoice is paid. Any non-empty value works as a condition: `{{#po_number}}PO: {{po_number}}{{/po_number}}` |
| `{{^paid}} … {{/paid}}` | Shown when **not** paid |

Placeholders may be formatted freely (bold, colour, size) — the formatting of the first part of
the placeholder wins for the whole value. Multi-line values (payment details, notes) keep their
line breaks. Values are inserted as text, never as markup.

## Placeholders

**Invoice:** `number` `title` `status` `issue_date` `due_date` `period` `period_start` `period_end` `po_number` `currency` `currency_name` `currency_symbol` `base_currency` `exchange_rate` `billing_mode` `public_url`

**You:** `company.name` `company.address` (one line) `company.address1` `company.address2` `company.city` `company.postal_code` `company.state` `company.country` `company.email` `company.phone` `company.website` `company.tax_id`

**Client:** `client.name` `client.contact` `client.address` `client.address1` `client.address2` `client.city` `client.postal_code` `client.state` `client.country` `client.email` `client.phone` `client.website` `client.tax_id`

**Line items** (inside `{{#items}}`): `n` `description` `unit` `quantity` `quantity_unit` ("21 days") `unit_price` `discount` `tax_rate` `amount`

**Totals:** `subtotal` `discount` `discount_label` `tax_total` `total` `amount_paid` `balance` `quantity_total` ("100 hours") `total_in_base` `base_total_label` `base_note`

**Text blocks:** `payment_details` (already the per-currency variant) `notes` `terms` `footer`

**Custom fields:** `custom.<key>` for a single field, or loop `{{#custom_fields}}{{label}}: {{value}}{{/custom_fields}}`

**Conditions:** `paid` `has_discount` `has_tax` `has_payments` `is_foreign_currency` plus any value above.

The same list is shown in the app under the template's *Word file* tab, and the upload reports
which placeholders it found so typos show up immediately.

## Tips

- Keep each placeholder in one paragraph/cell; don't split `{{` and `}}` across lines.
- Table row loops: the row containing `{{#items}}`…`{{/items}}` is the *prototype* and is
  removed if there are no items.
- Headers and footers are filled too.
- Fonts: Gotenberg ships common fonts (Liberation, DejaVu, Noto). Exotic Word fonts are
  substituted; stick to Arial/Calibri-like fonts or embed nothing fancy.
- Previews use sample data; open any real invoice → *View PDF* to see the actual output.
- Converter options: `DOCX_CONVERTER=gotenberg` (default, `GOTENBERG_URL`), `libreoffice`
  (`LIBREOFFICE_PATH`, for bare-metal installs with LibreOffice), or `none`.
