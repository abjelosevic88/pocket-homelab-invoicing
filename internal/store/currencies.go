package store

import (
	"context"
	"database/sql"
	"errors"
)

// SeedCurrencies inserts the standard currency list if the table is empty.
func (s *Store) SeedCurrencies(ctx context.Context, base string) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM currencies`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return s.Tx(ctx, func(tx *sql.Tx) error {
		for _, c := range DefaultCurrencies {
			enabled := 0
			if c.Code == base || c.Code == "EUR" || c.Code == "USD" || c.Code == "GBP" {
				enabled = 1
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO currencies (code, name, symbol, decimals, enabled) VALUES (?, ?, ?, ?, ?)`, c.Code, c.Name, c.Symbol, c.Decimals, enabled); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListCurrencies returns all currencies (enabledOnly filters).
func (s *Store) ListCurrencies(ctx context.Context, enabledOnly bool) ([]Currency, error) {
	q := `SELECT code, name, symbol, decimals, enabled FROM currencies`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY enabled DESC, code`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Currency{}
	for rows.Next() {
		var c Currency
		if err := rows.Scan(&c.Code, &c.Name, &c.Symbol, &c.Decimals, &c.Enabled); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCurrency returns a currency; unknown codes get a generic 2-decimal entry.
func (s *Store) GetCurrency(ctx context.Context, code string) Currency {
	var c Currency
	err := s.DB.QueryRowContext(ctx, `SELECT code, name, symbol, decimals, enabled FROM currencies WHERE code = ?`, code).Scan(&c.Code, &c.Name, &c.Symbol, &c.Decimals, &c.Enabled)
	if err != nil {
		return Currency{Code: code, Name: code, Symbol: code + " ", Decimals: 2}
	}
	return c
}

// UpsertCurrency creates or updates a currency.
func (s *Store) UpsertCurrency(ctx context.Context, c Currency) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO currencies (code, name, symbol, decimals, enabled) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET name = excluded.name, symbol = excluded.symbol, decimals = excluded.decimals, enabled = excluded.enabled`,
		c.Code, c.Name, c.Symbol, c.Decimals, c.Enabled)
	return err
}

// SetCurrencyEnabled toggles a currency.
func (s *Store) SetCurrencyEnabled(ctx context.Context, code string, enabled bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE currencies SET enabled = ? WHERE code = ?`, enabled, code)
	return err
}

// UpsertRate stores a base->quote rate.
func (s *Store) UpsertRate(ctx context.Context, r ExchangeRate) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO exchange_rates (base, quote, rate, source, fetched_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(base, quote) DO UPDATE SET rate = excluded.rate, source = excluded.source, fetched_at = excluded.fetched_at`,
		r.Base, r.Quote, r.Rate, r.Source, Now())
	return err
}

// ListRates lists stored rates for a base currency.
func (s *Store) ListRates(ctx context.Context, base string) ([]ExchangeRate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT base, quote, rate, source, fetched_at FROM exchange_rates WHERE base = ? ORDER BY quote`, base)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExchangeRate{}
	for rows.Next() {
		var r ExchangeRate
		if err := rows.Scan(&r.Base, &r.Quote, &r.Rate, &r.Source, &r.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRate returns the rate to convert 1 unit of `from` into `to`.
// Rates are stored as base->quote; we derive inverse / cross rates as needed.
func (s *Store) GetRate(ctx context.Context, from, to string) (float64, bool) {
	if from == to {
		return 1, true
	}
	var r float64
	err := s.DB.QueryRowContext(ctx, `SELECT rate FROM exchange_rates WHERE base = ? AND quote = ?`, from, to).Scan(&r)
	if err == nil && r > 0 {
		return r, true
	}
	err = s.DB.QueryRowContext(ctx, `SELECT rate FROM exchange_rates WHERE base = ? AND quote = ?`, to, from).Scan(&r)
	if err == nil && r > 0 {
		return 1 / r, true
	}
	// Cross rate through any common base.
	var a, b float64
	err = s.DB.QueryRowContext(ctx, `SELECT x.rate, y.rate FROM exchange_rates x JOIN exchange_rates y ON x.base = y.base WHERE x.quote = ? AND y.quote = ? LIMIT 1`, from, to).Scan(&a, &b)
	if err == nil && a > 0 {
		return b / a, true
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false
	}
	return 0, false
}

// DeleteRate removes a manual rate.
func (s *Store) DeleteRate(ctx context.Context, base, quote string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM exchange_rates WHERE base = ? AND quote = ?`, base, quote)
	return err
}

// DefaultCurrencies is the seed list.
var DefaultCurrencies = []Currency{
	{Code: "EUR", Name: "Euro", Symbol: "€", Decimals: 2},
	{Code: "USD", Name: "US Dollar", Symbol: "$", Decimals: 2},
	{Code: "GBP", Name: "British Pound", Symbol: "£", Decimals: 2},
	{Code: "CHF", Name: "Swiss Franc", Symbol: "CHF ", Decimals: 2},
	{Code: "CAD", Name: "Canadian Dollar", Symbol: "CA$", Decimals: 2},
	{Code: "AUD", Name: "Australian Dollar", Symbol: "A$", Decimals: 2},
	{Code: "NZD", Name: "New Zealand Dollar", Symbol: "NZ$", Decimals: 2},
	{Code: "JPY", Name: "Japanese Yen", Symbol: "¥", Decimals: 0},
	{Code: "CNY", Name: "Chinese Yuan", Symbol: "CN¥", Decimals: 2},
	{Code: "INR", Name: "Indian Rupee", Symbol: "₹", Decimals: 2},
	{Code: "SEK", Name: "Swedish Krona", Symbol: "kr ", Decimals: 2},
	{Code: "NOK", Name: "Norwegian Krone", Symbol: "kr ", Decimals: 2},
	{Code: "DKK", Name: "Danish Krone", Symbol: "kr ", Decimals: 2},
	{Code: "PLN", Name: "Polish Złoty", Symbol: "zł ", Decimals: 2},
	{Code: "CZK", Name: "Czech Koruna", Symbol: "Kč ", Decimals: 2},
	{Code: "HUF", Name: "Hungarian Forint", Symbol: "Ft ", Decimals: 0},
	{Code: "RON", Name: "Romanian Leu", Symbol: "lei ", Decimals: 2},
	{Code: "BGN", Name: "Bulgarian Lev", Symbol: "лв ", Decimals: 2},
	{Code: "RSD", Name: "Serbian Dinar", Symbol: "RSD ", Decimals: 2},
	{Code: "BAM", Name: "Bosnia-Herzegovina Mark", Symbol: "KM ", Decimals: 2},
	{Code: "HRK", Name: "Croatian Kuna", Symbol: "kn ", Decimals: 2},
	{Code: "MKD", Name: "Macedonian Denar", Symbol: "ден ", Decimals: 2},
	{Code: "TRY", Name: "Turkish Lira", Symbol: "₺", Decimals: 2},
	{Code: "UAH", Name: "Ukrainian Hryvnia", Symbol: "₴", Decimals: 2},
	{Code: "RUB", Name: "Russian Ruble", Symbol: "₽", Decimals: 2},
	{Code: "ILS", Name: "Israeli Shekel", Symbol: "₪", Decimals: 2},
	{Code: "AED", Name: "UAE Dirham", Symbol: "AED ", Decimals: 2},
	{Code: "SAR", Name: "Saudi Riyal", Symbol: "SAR ", Decimals: 2},
	{Code: "KWD", Name: "Kuwaiti Dinar", Symbol: "KD ", Decimals: 3},
	{Code: "BHD", Name: "Bahraini Dinar", Symbol: "BD ", Decimals: 3},
	{Code: "ZAR", Name: "South African Rand", Symbol: "R", Decimals: 2},
	{Code: "NGN", Name: "Nigerian Naira", Symbol: "₦", Decimals: 2},
	{Code: "KES", Name: "Kenyan Shilling", Symbol: "KSh ", Decimals: 2},
	{Code: "EGP", Name: "Egyptian Pound", Symbol: "E£", Decimals: 2},
	{Code: "BRL", Name: "Brazilian Real", Symbol: "R$", Decimals: 2},
	{Code: "MXN", Name: "Mexican Peso", Symbol: "MX$", Decimals: 2},
	{Code: "ARS", Name: "Argentine Peso", Symbol: "AR$", Decimals: 2},
	{Code: "CLP", Name: "Chilean Peso", Symbol: "CLP$", Decimals: 0},
	{Code: "COP", Name: "Colombian Peso", Symbol: "COL$", Decimals: 2},
	{Code: "PEN", Name: "Peruvian Sol", Symbol: "S/ ", Decimals: 2},
	{Code: "SGD", Name: "Singapore Dollar", Symbol: "S$", Decimals: 2},
	{Code: "HKD", Name: "Hong Kong Dollar", Symbol: "HK$", Decimals: 2},
	{Code: "TWD", Name: "New Taiwan Dollar", Symbol: "NT$", Decimals: 2},
	{Code: "KRW", Name: "South Korean Won", Symbol: "₩", Decimals: 0},
	{Code: "THB", Name: "Thai Baht", Symbol: "฿", Decimals: 2},
	{Code: "VND", Name: "Vietnamese Dong", Symbol: "₫", Decimals: 0},
	{Code: "IDR", Name: "Indonesian Rupiah", Symbol: "Rp ", Decimals: 2},
	{Code: "MYR", Name: "Malaysian Ringgit", Symbol: "RM ", Decimals: 2},
	{Code: "PHP", Name: "Philippine Peso", Symbol: "₱", Decimals: 2},
	{Code: "PKR", Name: "Pakistani Rupee", Symbol: "₨ ", Decimals: 2},
	{Code: "BDT", Name: "Bangladeshi Taka", Symbol: "৳", Decimals: 2},
	{Code: "ISK", Name: "Icelandic Króna", Symbol: "kr ", Decimals: 0},
	{Code: "GEL", Name: "Georgian Lari", Symbol: "₾", Decimals: 2},
	{Code: "KZT", Name: "Kazakhstani Tenge", Symbol: "₸", Decimals: 2},
	{Code: "BTC", Name: "Bitcoin", Symbol: "₿", Decimals: 8},
	{Code: "USDT", Name: "Tether", Symbol: "USDT ", Decimals: 2},
	{Code: "USDC", Name: "USD Coin", Symbol: "USDC ", Decimals: 2},
}
