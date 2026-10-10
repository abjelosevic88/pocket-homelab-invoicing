-- Recurring profiles: how the quantity is derived and which period a run covers.
--   quantity_mode: fixed | working_days   (working days of the service period; hours = days x hours_per_day)
--   period_mode:   forward | arrears      (arrears = the period that ended the day before the run date)
ALTER TABLE recurring_invoices ADD COLUMN quantity_mode TEXT NOT NULL DEFAULT 'fixed';
ALTER TABLE recurring_invoices ADD COLUMN period_mode TEXT NOT NULL DEFAULT 'forward';
