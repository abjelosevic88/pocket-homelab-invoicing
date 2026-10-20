-- Company register number of a client (e.g. Luxembourg RCS B123456), printed under Bill to.
ALTER TABLE clients ADD COLUMN registration_no TEXT NOT NULL DEFAULT '';
