package docx

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	data := map[string]any{
		"number": "INV-1", "client": map[string]any{"name": "Acme & Co"}, "paid": false, "po_number": "",
		"items": []map[string]any{{"description": "A", "amount": "1"}, {"description": "B", "amount": "2"}},
	}
	got := Render("{{number}} for {{client.name}}: {{#items}}[{{description}}={{amount}}]{{/items}} {{^paid}}UNPAID{{/paid}}{{#paid}}PAID{{/paid}} {{#po_number}}PO{{/po_number}}{{missing}}", data, xmlEscape)
	if got != "INV-1 for Acme &amp; Co: [A=1][B=2] UNPAID " {
		t.Fatalf("got %q", got)
	}
}

const docXML = `<w:document><w:body>
<w:p><w:r><w:t>Invoice </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>{{num</w:t></w:r><w:r><w:t>ber}}</w:t></w:r></w:p>
<w:tbl>
<w:tr><w:tc><w:p><w:r><w:t>Description</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Amount</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>{{#items}}{{description}}</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>{{amount}}{{/items}}</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>Total</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>{{total}}</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>
<w:p><w:r><w:t>{{payment_details}}</w:t></w:r></w:p>
</w:body></w:document>`

func TestFillXML(t *testing.T) {
	data := map[string]any{"number": "INV-7", "total": "€3", "payment_details": "IBAN: X\nBIC: Y",
		"items": []map[string]any{{"description": "A <b>", "amount": "€1"}, {"description": "B", "amount": "€2"}}}
	out := FillXML(docXML, data)
	if !strings.Contains(out, "Invoice INV-7") {
		t.Fatalf("split placeholder not merged: %s", out)
	}
	if strings.Count(out, "<w:tr>") != 4 {
		t.Fatalf("expected 4 rows (header, 2 items, total), got %d:\n%s", strings.Count(out, "<w:tr>"), out)
	}
	if !strings.Contains(out, "A &lt;b&gt;") || !strings.Contains(out, "<w:br/>") || strings.Contains(out, "{{") {
		t.Fatalf("escaping/newlines/leftover: %s", out)
	}
}

func buildDocx(t *testing.T, document string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   document,
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

func TestFillAndPlaceholders(t *testing.T) {
	tpl := buildDocx(t, docXML)
	ph, err := Placeholders(tpl)
	if err != nil || len(ph) < 5 {
		t.Fatalf("placeholders: %v %v", err, ph)
	}
	out, err := Fill(tpl, map[string]any{"number": "X", "items": []map[string]any{}, "total": "0", "payment_details": ""})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			if !strings.Contains(string(b), "Invoice X") || strings.Count(string(b), "<w:tr>") != 2 {
				t.Fatalf("bad output: %s", b)
			}
		}
	}
	if _, err := Fill([]byte("not a zip"), nil); err == nil {
		t.Fatal("expected error for non-zip")
	}
}
