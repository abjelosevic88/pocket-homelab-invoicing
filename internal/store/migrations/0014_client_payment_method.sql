-- How a client usually pays (e.g. bank_transfer); '' = the default payment method in Settings.
-- Pre-selected when recording a payment and used by "Mark fully paid".
ALTER TABLE clients ADD COLUMN payment_method TEXT NOT NULL DEFAULT '';
