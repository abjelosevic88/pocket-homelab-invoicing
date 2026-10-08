package money

import "testing"

func TestCompute(t *testing.T) {
	items := []LineItem{
		{Quantity: 10, UnitPrice: 85, TaxRate: 20},
		{Quantity: 2.5, UnitPrice: 600, Discount: 10, TaxRate: 20},
		{Quantity: 1, UnitPrice: 99.99, TaxRate: 0},
	}
	tot := Compute(items, "percent", 5, 2)
	if tot.LineTotals[0] != 850 || tot.LineTotals[1] != 1350 || tot.LineTotals[2] != 99.99 {
		t.Fatalf("line totals: %v", tot.LineTotals)
	}
	if tot.Subtotal != 2299.99 {
		t.Fatalf("subtotal %v", tot.Subtotal)
	}
	if tot.DiscountTotal != 115 {
		t.Fatalf("discount %v", tot.DiscountTotal)
	}
	// tax base = 2200 * 0.95 = 2090 -> 418
	if tot.TaxTotal != 418 {
		t.Fatalf("tax %v", tot.TaxTotal)
	}
	if tot.Total != 2602.99 {
		t.Fatalf("total %v", tot.Total)
	}
}

func TestComputeZeroDecimals(t *testing.T) {
	tot := Compute([]LineItem{{Quantity: 3, UnitPrice: 1000.4, TaxRate: 10}}, "none", 0, 0)
	if tot.Subtotal != 3001 || tot.TaxTotal != 300 || tot.Total != 3301 {
		t.Fatalf("%+v", tot)
	}
}

func TestFormat(t *testing.T) {
	cases := map[string]string{
		Format(1234567.891, 2, "€", "before"): "€1,234,567.89",
		Format(-42.5, 2, "", ""):              "-42.50",
		Format(1000, 0, "¥", "before"):        "¥1,000",
		Format(12.345, 3, "KD", "after"):      "12.345 KD",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
