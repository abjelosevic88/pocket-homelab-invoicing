# Templates & PDF rendering

## Engines

| Engine | How | Pros | Cons |
|---|---|---|---|
| `native` (default) | pure Go via go-pdf/fpdf with embedded DejaVu Sans | zero dependencies, ~15 MB image, fast, Unicode | layout limited to the three built-in designs |
| `chromium` | headless Chromium prints the HTML template | pixel-perfect custom HTML/CSS | +250 MB; use image tag `latest-chromium` |
| `gotenberg` | HTTP call to a Gotenberg container | custom HTML, keeps the app image small | an extra container (`docker compose --profile gotenberg`) |

Select with `PDF_ENGINE`. The web view and the public link always use the HTML template.

## Design settings (all engines)

Settings → Templates:

- **Layout:** *Classic* (logo/company left, big title right, coloured table header), *Modern* (full-width accent header), *Minimal* (monochrome, thin rules).
- **Accent colour**
- **Default** flag; invoices and recurring profiles can also pick a template explicitly.
- **Labels:** every printed word (INVOICE, Bill to, Due date, Subtotal, hours/days/months…) can be overridden per template. Make a "Deutsch" template with `invoice=RECHNUNG`, `bill_to=Rechnung an`, `due_date=Fällig am`… and assign it to German clients.

Logo: upload PNG/JPG under Settings → Company (SVG only works with the HTML engines).

## Custom HTML (chromium / gotenberg / web view)

The *HTML (advanced)* tab accepts a Go `html/template`. Click *Load built-in template* to start from the default and adjust CSS or structure. Available fields:

```
.Title .Number .Status .IssueDate .DueDate .Period .PONumber .Currency .BillingMode
.From / .To      → .Name .Contact .Lines (address lines) .Email .Phone .Website .TaxID
.Lines           → .Description .Unit .Quantity .UnitPrice .Discount .TaxRate .Total
.Subtotal .DiscountLabel .Discount .Taxes (→ .Label .Amount) .Total .AmountPaid .Balance
.ShowPaid .ShowTax .ShowDiscount .ShowUnit .BaseCurrencyNote
.Notes .Terms .Footer .PaymentDetails
.LogoDataURL .AccentColor .Layout .PublicURL
.Label "key"     → localised label with fallback
```

All amounts are pre-formatted strings in the invoice currency. Templates are validated on save; a broken template is rejected with the parse error.

Print CSS tips: the default uses `@page { size: A4; margin: 18mm }`; Gotenberg is configured with zero margins so the template controls them.

## Numbering

Settings → Invoicing. Pattern placeholders: `{YYYY} {YY} {MM} {DD} {SEQ} {SEQ:4} {CLIENT}` (3-letter client code). Sequence can reset yearly. Example: `{CLIENT}-{YYYY}-{SEQ:3}` → `ACM-2026-007`.
