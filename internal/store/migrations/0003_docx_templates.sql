ALTER TABLE invoice_templates ADD COLUMN kind TEXT NOT NULL DEFAULT 'design';
ALTER TABLE invoice_templates ADD COLUMN docx_path TEXT NOT NULL DEFAULT '';
ALTER TABLE clients ADD COLUMN template_id INTEGER REFERENCES invoice_templates(id) ON DELETE SET NULL;
