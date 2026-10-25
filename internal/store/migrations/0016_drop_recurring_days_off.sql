-- 0.9.0's days_off is gone again (a fixed discount amount was wanted, not fewer days).
ALTER TABLE recurring_invoices DROP COLUMN days_off;
