// Package money provides precise monetary arithmetic helpers.
//
// Amounts are persisted as REAL in SQLite and exposed as JSON numbers, but
// all intermediate calculations run through shopspring/decimal and are
// rounded to the currency's minor unit so totals never drift.
package money

import (
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

// Round rounds v half-up to the given number of decimals.
func Round(v float64, decimals int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	d := decimal.NewFromFloat(v).Round(int32(decimals))
	f, _ := d.Float64()
	return f
}

// Mul multiplies a and b and rounds to decimals.
func Mul(a, b float64, decimals int) float64 {
	d := decimal.NewFromFloat(a).Mul(decimal.NewFromFloat(b)).Round(int32(decimals))
	f, _ := d.Float64()
	return f
}

// Div divides a by b and rounds to decimals. Returns 0 when b == 0.
func Div(a, b float64, decimals int) float64 {
	if b == 0 {
		return 0
	}
	d := decimal.NewFromFloat(a).Div(decimal.NewFromFloat(b)).Round(int32(decimals))
	f, _ := d.Float64()
	return f
}

// Convert converts an amount using rate (amount * rate) rounded to decimals.
func Convert(amount, rate float64, decimals int) float64 {
	return Mul(amount, rate, decimals)
}

// Format formats an amount with thousands separators and the given decimals,
// e.g. Format(1234.5, 2, "€", "before") => "€1,234.50".
func Format(amount float64, decimals int, symbol string, symbolPosition string) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	s := decimal.NewFromFloat(amount).StringFixed(int32(decimals))
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String() + frac
	if symbol != "" {
		if symbolPosition == "after" {
			out = out + " " + symbol
		} else {
			out = symbol + out
		}
	}
	if neg {
		out = "-" + out
	}
	return out
}

// FormatCode formats with the ISO code appended, e.g. "1,234.50 EUR".
func FormatCode(amount float64, decimals int, code string) string {
	return fmt.Sprintf("%s %s", Format(amount, decimals, "", ""), code)
}

// LineItem is the minimal input to compute a line total.
type LineItem struct {
	Quantity  float64
	UnitPrice float64
	Discount  float64 // percent 0-100
	TaxRate   float64 // percent
}

// Totals are the computed document totals.
type Totals struct {
	Subtotal      float64            // sum of line totals after line discounts, before tax
	DiscountTotal float64            // document level discount amount
	TaxTotal      float64            // total tax
	Total         float64            // grand total
	TaxBreakdown  map[string]float64 // rate -> tax amount
	LineTotals    []float64
}

// Compute calculates totals for items with an optional document-level
// discount ("percent" or "fixed"). Tax is applied after discounts.
func Compute(items []LineItem, discountType string, discountValue float64, decimals int) Totals {
	t := Totals{TaxBreakdown: map[string]float64{}}
	var subtotal decimal.Decimal
	lines := make([]decimal.Decimal, len(items))
	for i, it := range items {
		line := decimal.NewFromFloat(it.Quantity).Mul(decimal.NewFromFloat(it.UnitPrice))
		if it.Discount > 0 {
			line = line.Mul(decimal.NewFromInt(100).Sub(decimal.NewFromFloat(it.Discount))).Div(decimal.NewFromInt(100))
		}
		line = line.Round(int32(decimals))
		lines[i] = line
		f, _ := line.Float64()
		t.LineTotals = append(t.LineTotals, f)
		subtotal = subtotal.Add(line)
	}
	t.Subtotal, _ = subtotal.Round(int32(decimals)).Float64()

	var docDiscount decimal.Decimal
	switch discountType {
	case "percent":
		docDiscount = subtotal.Mul(decimal.NewFromFloat(discountValue)).Div(decimal.NewFromInt(100)).Round(int32(decimals))
	case "fixed":
		docDiscount = decimal.NewFromFloat(discountValue).Round(int32(decimals))
	}
	if docDiscount.GreaterThan(subtotal) {
		docDiscount = subtotal
	}
	t.DiscountTotal, _ = docDiscount.Float64()

	// Document discount is distributed proportionally across lines for tax.
	var taxTotal decimal.Decimal
	for i, it := range items {
		if it.TaxRate <= 0 {
			continue
		}
		base := lines[i]
		if !subtotal.IsZero() && !docDiscount.IsZero() {
			base = base.Sub(base.Mul(docDiscount).Div(subtotal))
		}
		tax := base.Mul(decimal.NewFromFloat(it.TaxRate)).Div(decimal.NewFromInt(100)).Round(int32(decimals))
		taxTotal = taxTotal.Add(tax)
		key := decimal.NewFromFloat(it.TaxRate).String()
		tf, _ := tax.Float64()
		t.TaxBreakdown[key] = Round(t.TaxBreakdown[key]+tf, decimals)
	}
	t.TaxTotal, _ = taxTotal.Round(int32(decimals)).Float64()
	t.Total, _ = subtotal.Sub(docDiscount).Add(taxTotal).Round(int32(decimals)).Float64()
	return t
}
