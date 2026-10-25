-- Days subtracted from the working days of each period on quantity_mode = working_days profiles
-- (e.g. 2 = bill 19 days in a 21-day month); hour lines lose days_off x hours_per_day.
ALTER TABLE recurring_invoices ADD COLUMN days_off REAL NOT NULL DEFAULT 0;
