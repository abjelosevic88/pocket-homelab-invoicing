# Month-end billing, working days and the accountant package

Three features that turn the first of the month into a two-minute job and the
end of the year into one download.

## Month-end wizard

**Sidebar → Month-end.** Pick a month (defaults to the previous one). The page
has two parts:

1. **Prepare invoices** – one card per client with a proposed invoice:
   - the line items come from the client's monthly recurring profile (or, when
     there is none, from the client's billing mode and default rate),
   - the quantity comes from, in this order: unbilled tracked time in that month
     (hours), the working-days calendar (profiles with *Quantity: working days*
     or clients billed daily/hourly without a profile), or the fixed quantity of
     the profile,
   - clients billed in days (or in hours derived from days) get a **calendar of
     the month**: every working day is pre-selected; click a day to turn it off
     or on, click a week number to clear that week or put its working days back.
     The day or hour quantity follows the selection and the dates are saved on
     the invoice (`worked_days`), shown on the invoice page and in its timeline,
   - custom fields that look like counters (`14/15ПП`, `0031`) are pre-filled
     with the next value; the last used value is shown as a hint,
   - clients that already have an invoice for that service period are unticked
     and show the existing invoice; clients with neither a profile nor tracked
     time are unticked too (tick to invoice anyway).

   Adjust days, hours, rates or fiscal numbers inline and click **Create N draft
   invoices**. Each draft gets the next invoice number, the service period of the
   month, the exchange rate of the issue date and the client's or profile's
   template. Linked time entries are marked billed. The recurring profile is
   advanced past that period so the scheduler does not create it again.

2. **Finish & send** – every invoice of that month, new or existing, with
   buttons for the PDF, uploading a file (for example the fiscalised PDF, which
   is what gets emailed when *Email attachment* is set to "uploaded files"),
   emailing with the client's template, and marking as sent.

API: `GET /api/v1/month-end?month=YYYY-MM` returns the proposal; `POST
/api/v1/month-end` with `{month, issue_date, rows:[...]}` creates the drafts.

### Days worked in the invoice editor

The same calendar is available when creating or editing any invoice that has a
*day* (or *hour*) line: the **Days worked** card covers the service period (or
the issue month when no period is set). *Pick days* selects all working days and
sets the line quantity; every further click updates it. The picked dates are
stored with the invoice and restored when you edit it again. API: `worked_days`
(array of `YYYY-MM-DD`) on invoice create/update.

## Working-days calendar

**Settings → Invoicing → Working days & holidays.**

- **Work week**: which weekdays count (default Monday–Friday).
- **Holidays**: yearly public holidays (repeat every year on the same date) and
  one-off days (vacation, movable feasts). Built-in presets: Republika Srpska,
  Federation of BiH, and the Orthodox Easter days for a given year. Nothing is
  added until you click *Add* and save.
- **Preview** shows the working days and hours of any month, including which
  holidays were skipped.

Where it is used:

- Recurring profiles with **Quantity: working days of the period**: lines in
  *days* get the working days of the service period, lines in *hours* get
  working days × *hours per day*. Other units keep their fixed quantity.
- Recurring profiles also gained **Service period: previous period (in
  arrears)**: a run on 1 November then bills October (period 1–31 October)
  instead of November. `{month}` and `{period}` placeholders follow the billed
  period.
- The month-end wizard, as described above.

API: `GET /api/v1/calendar/working-days?month=YYYY-MM` (or `?from=&to=`),
`GET /api/v1/calendar/presets` and `GET /api/v1/calendar/presets?set=rs&year=2026`.

## Accountant package

**Reports → Income tax → Accountant package**, or the *Accountant package* button
in the Reports header. One ZIP per year:

| File | Content |
|---|---|
| `invoices/<number>_<client>.pdf` | every invoice as rendered by the app |
| `attachments/<number>/…` | files uploaded to invoices (fiscalised or signed originals) |
| `invoices.csv` | totals, exchange rate, total in base currency, custom fields, and the exchange-rate sentence per invoice |
| `exchange-rates.csv` | the rate used on every invoice |
| `payments.csv`, `expenses.csv` | money in and out during the year |
| `income-tax.csv` | the monthly income-tax estimate |
| `SUMMARY.md` | totals by currency and client, the invoice list, the income-tax estimate |
| `errors.txt` | only when a file could not be produced |

Options: include drafts; invoice PDFs as generated, uploaded only (generated when
an invoice has no upload), or both. Drafts and cancelled invoices are excluded by
default.

API: `GET /api/v1/reports/accountant-package.zip?year=2026[&drafts=1][&pdf=generated|uploaded|both]`.
