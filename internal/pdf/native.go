package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"math"
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

// RenderNative renders the invoice. When it would spill onto a second page by a little, or
// the signature would have to squeeze in beside the last text block, it is rendered again
// with tighter vertical spacing (two steps). The first one-page result with the signature
// on its own row wins; failing that, the first one-page result; failing that, the first.
func RenderNative(d *Document, logo []byte, logoType string) ([]byte, error) {
	var first, onePage []byte
	for _, k := range []float64{1, 0.88, 0.76} {
		b, pages, sigBeside, err := renderNative(d, logo, logoType, k)
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = b
		}
		if pages == 1 && !sigBeside {
			return b, nil
		}
		if pages == 1 && onePage == nil {
			onePage = b
		}
	}
	if onePage != nil {
		return onePage, nil
	}
	return first, nil
}

// pageLine is the footer's "number · Page 1 of 2"; it is left out on one-page invoices.
func pageLine(d *Document, page int, total string) string {
	return fmt.Sprintf("%s · %s %d / %s", d.Number, d.Label("page"), page, total)
}

// renderNative draws the document; k scales the vertical gaps and line heights (1 = normal).
func renderNative(d *Document, logo []byte, logoType string, k float64) ([]byte, int, bool, error) {
	lh := 4.5 * k // body text line height
	if lh < 4 {
		lh = 4
	}
	p := fpdf.New("P", "mm", "A4", "")
	p.AliasNbPages("{nb}")
	multiPage := false // set once a second page starts, so page 1's footer (drawn at that moment) gets its number too
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
		if multiPage {
			p.SetY(-12)
			p.CellFormat(contentW, 4, pageLine(d, p.PageNo(), "{nb}"), "", 0, "R", false, 0, "")
		}
	})
	newPage := func() { multiPage = true; p.AddPage() }
	p.SetAcceptPageBreakFunc(func() bool { multiPage = true; return true })
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
			p.CellFormat(contentW/2, lh, l, "", 1, "L", false, 0, "")
		}
		for _, l := range nonEmpty(d.From.Email, d.From.Phone, d.From.Website) {
			p.CellFormat(contentW/2, lh, l, "", 1, "L", false, 0, "")
		}
		if d.From.TaxID != "" {
			p.CellFormat(contentW/2, lh, d.Label("tax_id")+": "+d.From.TaxID, "", 1, "L", false, 0, "")
		}
		if d.From.VatID != "" {
			p.CellFormat(contentW/2, lh, d.Label("vat_id")+": "+d.From.VatID, "", 1, "L", false, 0, "")
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
	p.Ln(6 * k)
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
		p.CellFormat(contentW/2, lh, d.To.Contact, "", 1, "L", false, 0, "")
	}
	for _, l := range d.To.Lines {
		p.CellFormat(contentW/2, lh, l, "", 1, "L", false, 0, "")
	}
	if d.To.Email != "" {
		p.CellFormat(contentW/2, lh, d.To.Email, "", 1, "L", false, 0, "")
	}
	if d.To.TaxID != "" {
		p.CellFormat(contentW/2, lh, d.Label("client_tax_id")+": "+d.To.TaxID, "", 1, "L", false, 0, "")
	}
	if d.To.RegNo != "" {
		p.CellFormat(contentW/2, lh, d.Label("registration_no")+" "+d.To.RegNo, "", 1, "L", false, 0, "")
	}
	leftBottom := p.GetY()

	// Meta (right)
	p.SetXY(left+contentW/2, top)
	meta := [][2]string{{d.Label("issue_date"), d.IssueDate}}
	if d.IssuePlace != "" {
		meta = append(meta, [2]string{d.Label("issue_place"), d.IssuePlace})
	}
	meta = append(meta, [2]string{d.Label("due_date"), d.DueDate})
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
		p.CellFormat(contentW/4, 6*k, kv[0], "", 0, "R", false, 0, "")
		if last {
			p.SetFont("DejaVu", "B", 11)
			p.SetTextColor(accent.r, accent.g, accent.b)
		} else {
			p.SetFont("DejaVu", "", 9)
			p.SetTextColor(dark.r, dark.g, dark.b)
		}
		p.CellFormat(contentW/4, 6*k, kv[1], "", 1, "R", false, 0, "")
	}
	if p.GetY() < leftBottom {
		p.SetY(leftBottom)
	}
	p.Ln(8 * k)

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
			newPage()
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
				p.MultiCell(c.w-2, lh, vals[c.key], "", "L", false)
			} else {
				p.CellFormat(c.w, 5, vals[c.key], "", 0, c.align, false, 0, "")
			}
			x += c.w
		}
		p.SetXY(left, y+h)
		p.Line(left, y+h, left+contentW, y+h)
	}

	// ----- Totals -----
	p.Ln(4 * k)
	totalsW := 70.0
	tx := left + contentW - totalsW
	totalsTop := p.GetY()
	row := func(label, value string, bold bool, accentRow bool) {
		if p.GetY()+8 > 297-28 {
			newPage()
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
		p.CellFormat(totalsW/2, 6*k, label, "", 0, "L", false, 0, "")
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.CellFormat(totalsW/2, 6*k, value, "", 1, "R", false, 0, "")
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
	p.Ln(8 * k)
	signature := func(y float64) {
		x := left + contentW - 60
		p.SetDrawColor(dark.r, dark.g, dark.b)
		p.Line(x, y, x+60, y)
		p.SetXY(x, y+1)
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(grey.r, grey.g, grey.b)
		p.CellFormat(60, 5, d.SignatureLabel, "", 1, "C", false, 0, "")
	}
	if d.BaseCurrencyNote != "" {
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.MultiCell(contentW, lh, d.BaseCurrencyNote, "", "L", false)
		p.Ln(4 * k)
	}
	type textSection struct{ title, body string }
	var sections []textSection
	for _, sec := range []textSection{{d.Label("payment_details"), d.PaymentDetails}, {d.Label("notes"), d.Notes}, {d.Label("terms"), d.Terms}} {
		if strings.TrimSpace(sec.body) != "" {
			sections = append(sections, sec)
		}
	}
	sigPlaced := false
	signRoom := math.Max(12, 16*k) // room to sign by hand above the line
	sigBlock := signRoom + 1 + 5   // + the line and the label
	for i, sec := range sections {
		if p.GetY()+5+lh*2 > 297-28 { // title + two lines; longer blocks continue on the next page
			newPage()
		}
		// The signature gets its own row below the last text block, with room to sign. Only when
		// that would push it onto a new page does it sit to the right of the block instead.
		withSig := false
		if d.SignatureLabel != "" && i == len(sections)-1 {
			p.SetFont("DejaVu", "", 9)
			h := 5 + float64(len(p.SplitLines([]byte(sec.body), contentW)))*lh + 3*k
			withSig = p.GetY()+h+sigBlock > 297-28
		}
		w := contentW
		if withSig {
			w = contentW - 70
		}
		top := p.GetY()
		p.SetFont("DejaVu", "B", 8)
		p.SetTextColor(grey.r, grey.g, grey.b)
		p.CellFormat(w, 5, strings.ToUpper(sec.title), "", 1, "L", false, 0, "")
		p.SetFont("DejaVu", "", 9)
		p.SetTextColor(dark.r, dark.g, dark.b)
		p.MultiCell(w, lh, sec.body, "", "L", false)
		if withSig {
			sigPlaced = true
			bottom := p.GetY()
			// Line well below the block's top so there is room beside the text to sign,
			// but never into the footer.
			signature(math.Min(math.Max(top+18, bottom+4), 297-28-6))
			if p.GetY() < bottom {
				p.SetY(bottom)
			}
		}
		p.Ln(3 * k)
	}
	if d.SignatureLabel != "" && !sigPlaced {
		if p.GetY()+sigBlock > 297-28 { // only break when it really does not fit
			newPage()
		}
		// As low as the footer allows (up to 30 mm under the text) for more room to sign.
		signature(math.Max(p.GetY()+signRoom, math.Min(p.GetY()+30, 297-28-6)))
	}

	pages := p.PageNo()
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, 0, false, err
	}
	return buf.Bytes(), pages, sigPlaced, nil
}
