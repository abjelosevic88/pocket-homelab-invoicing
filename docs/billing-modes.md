# Billing modes

Pocket Invoicing doesn't force one pricing model. Each **client** has a default billing mode and rate, each **invoice** has a billing mode, and each **line item** has its own unit. You can mix units on one invoice (e.g. 2 days on-site + 3.5 hours remote + 1 domain renewal).

## Hourly

- Set the client to **Hourly** with their hourly rate.
- Track time with the timer or manual entries. Timer stops are rounded *up* to the configured block (default 15 min — set to 1 to disable).
- **Time tracking → Invoice unbilled time** creates a draft with one line per entry, per day, per project, or a single line. Entries are linked to the invoice and marked as billed; deleting the invoice unlinks them.
- Per-entry rate overrides are honoured (e.g. emergency rate).

## Daily

- Set the client to **Daily** and the day rate.
- When invoicing tracked time choose **Bill as: Days**. Hours are divided by `hours_per_day` (Settings → Invoicing, default 8) and rounded to 2 decimals, so 6 hours = 0.75 days.
- Or skip time tracking and type `quantity = 3, unit = day` directly.

## Monthly retainer

- Set the client to **Monthly retainer**.
- Create a **Recurring profile**: frequency *Monthly*, one line "Managed hosting – {month}", unit *month*, quantity 1, the monthly price. The placeholder expands to e.g. "October 2026"; `{period}` gives "2026-10-01 – 2026-10-31".
- Choose when it runs (e.g. 1st of the month), due days, and whether to **auto-send** by email.
- The scheduler generates the invoice on the run date (and catches up if the server was down). The service period is printed on the invoice.
- Overages: add hourly lines to the generated draft before sending, or invoice extra time separately.
- Quarterly/yearly retainers: same thing with a different frequency. "Every 2 months": frequency monthly, interval 2.

## Fixed price

- Set the invoice to **Fixed price**, one line with unit *fixed* and quantity 1.
- Milestones: either several fixed lines, or record partial **payments** as they arrive (the invoice shows paid/balance and a progress bar).
- Deposits: create an invoice for the deposit, then the final invoice for the remainder.

## Per unit / products

Use the **Catalog** (Settings → Catalog) for anything with a standard price: domain renewals, hardware, licences. Adding from the catalog fills description, unit, price and tax.

## Discounts & taxes

- Line discount in % and/or a document-level discount (percent or fixed amount).
- Tax per line (choose from your tax rates). The document discount is apportioned across lines before tax, and the PDF shows a per-rate tax breakdown — what most EU tax offices want.
- 0% rates (e.g. "Reverse charge") are fine; put the legal note in the invoice terms.

## Expenses

Mark an expense as **billable** and assign a client. "Invoice unbilled time" can include those expenses as extra lines (converted to the invoice currency if needed).
