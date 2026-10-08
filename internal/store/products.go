package store

import "context"

// ListProducts lists catalog items.
func (s *Store) ListProducts(ctx context.Context, includeArchived bool) ([]Product, error) {
	q := `SELECT id, name, description, unit, unit_price, currency, tax_rate, archived, created_at FROM products`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Unit, &p.UnitPrice, &p.Currency, &p.TaxRate, &p.Archived, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateProduct inserts a product.
func (s *Store) CreateProduct(ctx context.Context, p *Product) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO products (name, description, unit, unit_price, currency, tax_rate, archived) VALUES (?, ?, ?, ?, ?, ?, ?)`, p.Name, p.Description, p.Unit, p.UnitPrice, p.Currency, p.TaxRate, p.Archived)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdateProduct updates a product.
func (s *Store) UpdateProduct(ctx context.Context, p *Product) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE products SET name=?, description=?, unit=?, unit_price=?, currency=?, tax_rate=?, archived=? WHERE id=?`, p.Name, p.Description, p.Unit, p.UnitPrice, p.Currency, p.TaxRate, p.Archived, p.ID)
	return err
}

// DeleteProduct deletes a product.
func (s *Store) DeleteProduct(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM products WHERE id = ?`, id)
	return err
}

// ListTaxRates lists tax rates.
func (s *Store) ListTaxRates(ctx context.Context) ([]TaxRate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, rate, is_default, archived FROM tax_rates WHERE archived = 0 ORDER BY rate`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaxRate{}
	for rows.Next() {
		var t TaxRate
		if err := rows.Scan(&t.ID, &t.Name, &t.Rate, &t.IsDefault, &t.Archived); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveTaxRate creates or updates a tax rate.
func (s *Store) SaveTaxRate(ctx context.Context, t *TaxRate) error {
	if t.IsDefault {
		if _, err := s.DB.ExecContext(ctx, `UPDATE tax_rates SET is_default = 0`); err != nil {
			return err
		}
	}
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO tax_rates (name, rate, is_default) VALUES (?, ?, ?)`, t.Name, t.Rate, t.IsDefault)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE tax_rates SET name=?, rate=?, is_default=? WHERE id=?`, t.Name, t.Rate, t.IsDefault, t.ID)
	return err
}

// DeleteTaxRate removes a tax rate.
func (s *Store) DeleteTaxRate(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM tax_rates WHERE id = ?`, id)
	return err
}
