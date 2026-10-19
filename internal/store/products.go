package store

import "context"

// ListProducts lists catalog items.
func (s *Store) ListProducts(ctx context.Context, uid int64, includeArchived bool) ([]Product, error) {
	q := `SELECT id, name, description, unit, unit_price, currency, tax_rate, archived, created_at FROM products WHERE user_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q, uid)
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
func (s *Store) CreateProduct(ctx context.Context, uid int64, p *Product) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO products (user_id, name, description, unit, unit_price, currency, tax_rate, archived) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, NullInt(uid), p.Name, p.Description, p.Unit, p.UnitPrice, p.Currency, p.TaxRate, p.Archived)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdateProduct updates a product.
func (s *Store) UpdateProduct(ctx context.Context, uid int64, p *Product) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE products SET name=?, description=?, unit=?, unit_price=?, currency=?, tax_rate=?, archived=? WHERE id=? AND user_id=?`, p.Name, p.Description, p.Unit, p.UnitPrice, p.Currency, p.TaxRate, p.Archived, p.ID, uid)
	return notFoundIfNone(res, err)
}

// DeleteProduct deletes a product.
func (s *Store) DeleteProduct(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM products WHERE id = ? AND user_id = ?`, id, uid)
	return notFoundIfNone(res, err)
}

// ListTaxRates lists tax rates.
func (s *Store) ListTaxRates(ctx context.Context, uid int64) ([]TaxRate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, rate, is_default, archived FROM tax_rates WHERE user_id = ? AND archived = 0 ORDER BY rate`, uid)
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
func (s *Store) SaveTaxRate(ctx context.Context, uid int64, t *TaxRate) error {
	if t.ID > 0 {
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM tax_rates WHERE id = ? AND user_id = ?`, t.ID, uid).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
	}
	if t.IsDefault {
		if _, err := s.DB.ExecContext(ctx, `UPDATE tax_rates SET is_default = 0 WHERE user_id = ?`, uid); err != nil {
			return err
		}
	}
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO tax_rates (user_id, name, rate, is_default) VALUES (?, ?, ?, ?)`, NullInt(uid), t.Name, t.Rate, t.IsDefault)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE tax_rates SET name=?, rate=?, is_default=? WHERE id=? AND user_id=?`, t.Name, t.Rate, t.IsDefault, t.ID, uid)
	return err
}

// DeleteTaxRate removes a tax rate.
func (s *Store) DeleteTaxRate(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM tax_rates WHERE id = ? AND user_id = ?`, id, uid)
	return notFoundIfNone(res, err)
}
