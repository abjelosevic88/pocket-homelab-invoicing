# Migrating from Invoice Ninja v5

`scripts/import_invoiceninja.py` imports clients, invoices (with line items), payments and
the documents you attached to invoices. It talks to the Pocket Invoicing API, so run it from
anywhere that can reach your instance. Only the Python 3 standard library is needed.

## 1. Export from Invoice Ninja

Run these against the Invoice Ninja MySQL database (adjust container/user names):

```bash
cd ~/invoiceninja
PW=$(grep ^DB_PASSWORD= .env | cut -d= -f2-)
Q() { docker exec invoiceninja-db mysql --default-character-set=utf8mb4 -N -B -r -uninja -p"$PW" ninja -e "$1"; }

Q "SELECT JSON_ARRAYAGG(JSON_OBJECT('id',c.id,'name',c.name,'address1',c.address1,'address2',c.address2,'city',c.city,'state',c.state,'postal_code',c.postal_code,'country_id',c.country_id,'vat_number',c.vat_number,'website',c.website,'phone',c.phone,'currency',cu.code,'contacts',(SELECT JSON_ARRAYAGG(JSON_OBJECT('first',cc.first_name,'last',cc.last_name,'email',cc.email)) FROM client_contacts cc WHERE cc.client_id=c.id))) FROM clients c LEFT JOIN currencies cu ON cu.id = COALESCE(c.settings->>'$.currency_id', 1) WHERE c.is_deleted=0" > in_clients.json

Q "SELECT JSON_ARRAYAGG(JSON_OBJECT('id',i.id,'client_id',i.client_id,'number',i.number,'status',i.status_id,'date',i.date,'due',i.due_date,'amount',i.amount,'paid',i.paid_to_date,'po',i.po_number,'notes',i.public_notes,'cv1',i.custom_value1,'cv2',i.custom_value2,'cv3',i.custom_value3,'cv4',i.custom_value4,'deleted',i.is_deleted,'items',CAST(i.line_items AS JSON))) FROM (SELECT * FROM invoices ORDER BY date, id) i" > in_invoices.json

# attached documents, renamed to NUMBER__original-name.pdf
docker cp invoiceninja:/var/www/html/storage/app/public/<company-hash>/documents ./in_docs_raw
Q "SELECT i.number, d.name, SUBSTRING_INDEX(d.url,'/',-1) FROM documents d JOIN invoices i ON i.id=d.documentable_id AND d.documentable_type='invoices'" | while IFS=$'\t' read -r number name file; do cp "in_docs_raw/$file" "in_docs/${number}__${name}"; done
```

## 2. Describe the extras (optional `extras.json`)

Everything Invoice Ninja does not export cleanly goes here: your company settings,
contact names, rates and billing mode per client, which custom field maps to which,
and the line unit (hour/day) per client. Example:

```json
{
  "base_eur_peg": 1.95583,
  "enable_currencies": ["BAM", "EUR", "USD"],
  "manual_rates": {"EUR": 0.511292},
  "settings": {"base_currency": "BAM", "number_format": "1.234,56", "invoice_number_format": "INV-{SEQ:3}-{YYYY}", "invoice_next_seq": 7, "default_due_days": 0,
               "custom_fields": [{"key": "pfr", "label": "PFR broj računa", "show_on_pdf": true}]},
  "clients": {"1": {"contact_name": "Jane Doe", "email": "jane@acme.test", "billing_mode": "daily", "default_rate": 400, "payment_terms_days": 0}},
  "unit_by_client": {"1": "day", "2": "hour"},
  "custom_field_map": {"cv1": "pfr"},
  "total_in_base_field": "cv3",
  "time_entries": [{"in_client_id": 2, "description": "April (unbilled)", "started_at": "2026-04-01T09:00:00Z", "duration_minutes": 6000, "billable": true}],
  "products": [], "recurring": []
}
```

- `base_eur_peg`: if your base currency is not on the ECB list (BAM, RSD, …), give the fixed
  base-per-EUR rate so historical exchange rates can be derived.
- `total_in_base_field`: an Invoice Ninja custom field holding the invoice total in your base
  currency; the importer derives each invoice's exchange rate from it. Otherwise the ECB rate
  of the invoice date is used (via frankfurter.app).
- `unit_by_client`: `hour`, `day`, `month`, `unit` or `fixed`.

## 3. Run

```bash
./scripts/import_invoiceninja.py --url https://invoices.example.com \
  --token pi_…  (or --email/--password) \
  --invoices in_invoices.json --clients in_clients.json --docs ./in_docs --config extras.json
```

Invoices that already exist (same number) are skipped, so the script can be re-run. Paid
invoices get one payment dated on the invoice date with reference "Imported from Invoice Ninja".
Attached documents land under the invoice's **Attachments**; set *Settings → Invoicing → Email
attachment* to "uploaded files" if you send your own PDFs instead of the generated one.
