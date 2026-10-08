package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
)

//go:embed DejaVuSans.ttf
var fontRegular []byte

//go:embed DejaVuSans-Bold.ttf
var fontBold []byte

type rgb struct{ r, g, b int }

func parseHex(s string) rgb {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return rgb{37, 99, 235}
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return rgb{37, 99, 235}
	}
	return rgb{int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff)}
}

func firstLabel(d *Document, keys ...string) string {
	for _, k := range keys {
		if v, ok := d.Labels[k]; ok && v != "" {
			return v
		}
	}
	for _, k := range keys {
		if v := DefaultLabels[k]; v != "" {
			return v
		}
	}
	return "Total"
}

// RenderNative renders the document with the pure-Go engine.
func RenderNative(d *Document, logo []byte, logoType string) ([]byte, error) {
	p := fpdf.New("P", "mm", "A4", "")
	p.AddUTF8FontFromBytes("DejaVu", "", fontRegular)
	p.AddUTF8FontFromBytes("DejaVu", "B", fontBold)
	p.SetMargins(18, 18, 18)
	p.SetAutoPageBreak(true, 28)
	accent := parseHex(d.AccentColor)
	grey := rgb{107, 114, 128}
	dark := rgb{17, 24, 39}
	pageW, _ := p.GetPageSize()
	left, _, right, _ := p.GetMargins()
	contentW := pageW - left - right

	p.SetFooterFunc(func() {
		p.SetY(-20)
		p.SetFont("DejaVu", "", 8)
		p.SetTextColor(grey.r, grey.g, grey.b)
		if d.Footer != "" {
			p.MultiCell(contentW, 4, d.Footer, "", "C", false)
		}
		p.SetY(-12)
		p.CellFormat(contentW, 4, fmt.Sprintf("%s · %s %d", d.Number, d.Label("page"), p.PageNo()), "", 0, "R", false, 0, "")
	})
	p.AddPage()

	// ----- Header -----
	headerTop := p.GetY()
	switch d.Layout {
	case "modern":
		p.SetFillColor(accent.r, accent.g, accent.b)
		p.Rect(0, 0, pageW, 34, "F")
		p.SetXY(left, 10)
		p.SetTextColor(255, 255, 255)
		if len(logo) > 0 {
			opts := fpdf.ImageOptions{ImageType: logoType, ReadDpi: true}
			p.RegisterImageOptionsReader("logo", opts, bytes.NewReader(logo))
			p.ImageOptions("logo", left, 7, 0, 20, false, opts, 0, "")
			p.SetXY(left+50, 10)
		}
		p.SetFont("DejaVu", "B", 20)
		p.CellFormat(contentW/2, 12, d.From.Name, "", 0, "L", false, 0, "")
		p.SetFont("DejaVu", "B", 24)
		p.CellFormat(contentW/2, 12, d.Title, "", 1, "R", false, 0, "")
		p.SetFont("DejaVu", "", 10)
		p.SetX(left + contentW/2)
		p.CellFormat(contentW/2, 6, d.Number, "", 1, "R", false, 0, "")
		p.SetY(42)
		headerTop = 42
	default: // classic / minimal
		if len(logo) > 0 {
			opts := fpdf.ImageOptions{ImageType: logoType, ReadDpi: true}
			p.RegisterImageOptionsReader("logo", opts, bytes.NewReader(logo))
			p.ImageOptions("logo", left, headerTop, 0, 18, false, opts, 0, "")
			p.SetY(headerTop + 22)
		}
		p.SetFont("DejaVu", "B", 14)
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.MultiCell(contentW/2-6, 7, d.From.Name, "", "L", false)
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(grey.r, grey.g, grey.b)
		for _, l := range d.From.Lines {
			p.CellFormat(contentW/2, 4.5, l, "", 1, "L", false, 0, "")
		}
		for _, l := range nonEmpty(d.From.Email, d.From.Phone, d.From.Website) {
			p.CellFormat(contentW/2, 4.5, l, "", 1, "L", false, 0, "")
		}
		if d.From.TaxID != "" {
			p.CellFormat(contentW/2, 4.5, d.Label("tax_id")+": "+d.From.TaxID, "", 1, "L", false, 0, "")
		}
		fromBottom := p.GetY()

		// Right side: title + number
		p.SetXY(left+contentW/2, headerTop)
		if d.Layout == "minimal" {
			p.SetTextColor(dark.r, dark.g, dark.b)
		} else {
			p.SetTextColor(accent.r, accent.g, accent.b)
		}
		p.SetFont("DejaVu", "B", 26)
		p.CellFormat(contentW/2, 12, d.Title, "", 1, "R", false, 0, "")
		p.SetX(left + contentW/2)
		p.SetFont("DejaVu", "", 11)
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.CellFormat(contentW/2, 6, d.Number, "", 1, "R", false, 0, "")
		if p.GetY() < fromBottom {
			p.SetY(fromBottom)
		}
	}

	// ----- Meta + Bill to -----
	p.Ln(6)
	top := p.GetY()
	// Bill to (left)
	p.SetFont("DejaVu", "B", 8)
	p.SetTextColor(grey.r, grey.g, grey.b)
	p.CellFormat(contentW/2, 5, strings.ToUpper(d.Label("bill_to")), "", 1, "L", false, 0, "")
	p.SetFont("DejaVu", "B", 11)
	p.SetTextColor(dark.r, dark.g, dark.b)
	p.CellFormat(contentW/2, 6, d.To.Name, "", 1, "L", false, 0, "")
	p.SetFont("DejaVu", "", 9)
	p.SetTextColor(grey.r, grey.g, grey.b)
	if d.To.Contact != "" {
		p.CellFormat(contentW/2, 4.5, d.To.Contact, "", 1, "L", false, 0, "")
	}
	for _, l := range d.To.Lines {
		p.CellFormat(contentW/2, 4.5, l, "", 1, "L", false, 0, "")
	}
	if d.To.Email != "" {
		p.CellFormat(contentW/2, 4.5, d.To.Email, "", 1, "L", false, 0, "")
	}
	if d.To.TaxID != "" {
		p.CellFormat(contentW/2, 4.5, d.Label("tax_id")+": "+d.To.TaxID, "", 1, "L", false, 0, "")
	}
	leftBottom := p.GetY()

	// Meta (right)
	p.SetXY(left+contentW/2, top)
	meta := [][2]string{{d.Label("issue_date"), d.IssueDate}, {d.Label("due_date"), d.DueDate}}
	if d.Period != "" {
		meta = append(meta, [2]string{d.Label("period"), d.Period})
	}
	if d.PONumber != "" {
		meta = append(meta, [2]string{d.Label("po_number"), d.PONumber})
	}
	for _, kv := range d.CustomFields {
		meta = append(meta, [2]string{kv.Label, kv.Value})
	}
	meta = append(meta, [2]string{d.Label("balance_due"), d.Balance})
	for i, kv := range meta {
		p.SetX(left + contentW/2)
		last := i == len(meta)-1
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(grey.r, grey.g, grey.b)
		p.CellFormat(contentW/4, 6, kv[0], "", 0, "R", false, 0, "")
		if last {
			p.SetFont("DejaVu", "B", 11)
			p.SetTextColor(accent.r, accent.g, accent.b)
		} else {
			p.SetFont("DejaVu", "", 9)
			p.SetTextColor(dark.r, dark.g, dark.b)
		}
		p.CellFormat(contentW/4, 6, kv[1], "", 1, "R", false, 0, "")
	}
	if p.GetY() < leftBottom {
		p.SetY(leftBottom)
	}
	p.Ln(8)

	// ----- Table -----
	cols := []struct {
		key   string
		w     float64
		align string
	}{{"description", 0, "L"}}
	if d.ShowUnit {
		cols = append(cols, struct {
			key   string
			w     float64
			align string
		}{"unit", 18, "L"})
	}
	qtyW := 16.0
	if !d.ShowUnit {
		qtyW = 26
	}
	cols = append(cols, struct {
		key   string
		w     float64
		align string
	}{"quantity", qtyW, "R"})
	if d.ShowRate {
		cols = append(cols, struct {
			key   string
			w     float64
			align string
		}{"unit_price", 28, "R"})
	}
	if d.ShowDiscount {
		cols = append(cols, struct {
			key   string
			w     float64
			align string
		}{"discount", 18, "R"})
	}
	if d.ShowTax {
		cols = append(cols, struct {
			key   string
			w     float64
			align string
		}{"tax", 16, "R"})
	}
	cols = append(cols, struct {
		key   string
		w     float64
		align string
	}{"amount", 30, "R"})
	fixed := 0.0
	for _, c := range cols {
		fixed += c.w
	}
	cols[0].w = contentW - fixed

	drawHeader := func() {
		p.SetFont("DejaVu", "B", 8)
		if d.Layout == "minimal" {
			p.SetTextColor(grey.r, grey.g, grey.b)
			p.SetDrawColor(dark.r, dark.g, dark.b)
			for _, c := range cols {
				p.CellFormat(c.w, 7, strings.ToUpper(d.Label(c.key)), "B", 0, c.align, false, 0, "")
			}
		} else {
			p.SetFillColor(accent.r, accent.g, accent.b)
			p.SetTextColor(255, 255, 255)
			for _, c := range cols {
				p.CellFormat(c.w, 7, strings.ToUpper(d.Label(c.key)), "", 0, c.align, true, 0, "")
			}
		}
		p.Ln(-1)
	}
	drawHeader()
	p.SetFont("DejaVu", "", 9)
	p.SetTextColor(dark.r, dark.g, dark.b)
	p.SetDrawColor(229, 231, 235)
	for i, l := range d.Lines {
		vals := map[string]string{"description": l.Description, "unit": l.Unit, "quantity": l.Quantity, "unit_price": l.UnitPrice, "discount": l.Discount, "tax": l.TaxRate, "amount": l.Total}
		// measure description height
		lines := p.SplitText(l.Description, cols[0].w-2)
		h := float64(len(lines))*4.5 + 3
		if h < 8 {
			h = 8
		}
		if p.GetY()+h > 297-28 {
			p.AddPage()
			drawHeader()
			p.SetFont("DejaVu", "", 9)
			p.SetTextColor(dark.r, dark.g, dark.b)
		}
		y := p.GetY()
		x := left
		if i%2 == 1 && d.Layout != "minimal" {
			p.SetFillColor(249, 250, 251)
			p.Rect(left, y, contentW, h, "F")
		}
		for ci, c := range cols {
			p.SetXY(x, y+1.5)
			if ci == 0 {
				p.MultiCell(c.w-2, 4.5, vals[c.key], "", "L", false)
			} else {
				p.CellFormat(c.w, 5, vals[c.key], "", 0, c.align, false, 0, "")
			}
			x += c.w
		}
		p.SetXY(left, y+h)
		p.Line(left, y+h, left+contentW, y+h)
	}

	// ----- Totals -----
	p.Ln(4)
	totalsW := 70.0
	tx := left + contentW - totalsW
	totalsTop := p.GetY()
	row := func(label, value string, bold bool, accentRow bool) {
		if p.GetY()+8 > 297-28 {
			p.AddPage()
		}
		p.SetX(tx)
		if bold {
			p.SetFont("DejaVu", "B", 10)
		} else {
			p.SetFont("DejaVu", "", 9)
		}
		if accentRow {
			p.SetFillColor(accent.r, accent.g, accent.b)
			p.SetTextColor(255, 255, 255)
			p.CellFormat(totalsW/2, 8, label, "", 0, "L", true, 0, "")
			p.CellFormat(totalsW/2, 8, value, "", 1, "R", true, 0, "")
			return
		}
		p.SetTextColor(grey.r, grey.g, grey.b)
		p.CellFormat(totalsW/2, 6, label, "", 0, "L", false, 0, "")
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.CellFormat(totalsW/2, 6, value, "", 1, "R", false, 0, "")
	}
	if d.QuantityTotal != "" {
		row(firstLabel(d, "quantity_total", "quantity"), d.QuantityTotal, false, false)
	}
	row(d.Label("subtotal"), d.Subtotal, false, false)
	if d.Discount != "" {
		row(d.DiscountLabel, d.Discount, false, false)
	}
	for _, t := range d.Taxes {
		row(t.Label, t.Amount, false, false)
	}
	row(d.Label("total"), d.Total, true, false)
	if d.ShowPaid {
		row(d.Label("amount_paid"), "-"+d.AmountPaid, false, false)
	}
	row(d.Label("balance_due"), d.Balance, true, d.Layout != "minimal")
	if d.BaseTotal != "" {
		row(d.BaseTotalLabel, d.BaseTotal, true, false)
	}
	if d.Status == "paid" {
		afterTotals := p.GetY()
		p.SetFont("DejaVu", "B", 28)
		p.SetTextColor(22, 163, 74)
		p.SetDrawColor(22, 163, 74)
		p.SetLineWidth(1.2)
		p.TransformBegin()
		p.TransformRotate(-12, left+35, totalsTop+14)
		p.SetXY(left+10, totalsTop+6)
		p.CellFormat(50, 16, d.Label("paid_stamp"), "1", 0, "C", false, 0, "")
		p.TransformEnd()
		p.SetLineWidth(0.2)
		p.SetDrawColor(229, 231, 235)
		p.SetY(afterTotals)
	}

	// ----- Notes / terms / payment details -----
	p.Ln(8)
	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		if p.GetY()+20 > 297-28 {
			p.AddPage()
		}
		p.SetFont("DejaVu", "B", 8)
		p.SetTextColor(grey.r, grey.g, grey.b)
		p.CellFormat(contentW, 5, strings.ToUpper(title), "", 1, "L", false, 0, "")
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.MultiCell(contentW, 4.5, body, "", "L", false)
		p.Ln(3)
	}
	if d.BaseCurrencyNote != "" {
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.MultiCell(contentW, 4.5, d.BaseCurrencyNote, "", "L", false)
		p.Ln(4)
	}
	section(d.Label("payment_details"), d.PaymentDetails)
	section(d.Label("notes"), d.Notes)
	section(d.Label("terms"), d.Terms)
	if d.SignatureLabel != "" {
		if p.GetY()+16 > 297-28 {
			p.AddPage()
		}
		p.Ln(6)
		x := left + contentW - 60
		y := p.GetY()
		p.SetDrawColor(dark.r, dark.g, dark.b)
		p.Line(x, y, x+60, y)
		p.SetXY(x, y+1)
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(grey.r, grey.g, grey.b)
		p.CellFormat(60, 5, d.SignatureLabel, "", 1, "C", false, 0, "")
	}

	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
