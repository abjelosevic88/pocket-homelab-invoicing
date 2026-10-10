-- Dates picked in the day calendar (JSON array of YYYY-MM-DD), kept so the editor can show them again.
ALTER TABLE invoices ADD COLUMN worked_days TEXT NOT NULL DEFAULT '[]';
