-- discount_once = 1: the profile's discount applies to the next generated invoice only and is
-- cleared from the profile afterwards (scheduler, Run now, month-end wizard).
ALTER TABLE recurring_invoices ADD COLUMN discount_once INTEGER NOT NULL DEFAULT 1;
