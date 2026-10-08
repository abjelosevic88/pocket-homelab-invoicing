#!/usr/bin/env python3
"""Generate sample Word (.docx) invoice templates without third-party libraries.

  ./scripts/make_sample_docx.py docs/templates

Produces `invoice-simple.docx` (clean generic layout) and `invoice-wisestack.docx`
(bordered header, HOURS/AMOUNT table, transfer details, base-currency sentence,
custom fields, signature line). Both only use placeholders documented in
docs/word-templates.md, so they double as a syntax reference.
"""
import sys, zipfile, os
from xml.sax.saxutils import escape

W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"'


def run(text, bold=False, size=None, color=None, italic=False):
    rpr = ""
    if bold: rpr += "<w:b/>"
    if italic: rpr += "<w:i/>"
    if color: rpr += f'<w:color w:val="{color}"/>'
    if size: rpr += f'<w:sz w:val="{size*2}"/><w:szCs w:val="{size*2}"/>'
    return f'<w:r><w:rPr>{rpr}</w:rPr><w:t xml:space="preserve">{escape(text)}</w:t></w:r>'


def para(*runs, align=None, space_after=60, space_before=0, keep=False):
    ppr = f'<w:spacing w:before="{space_before}" w:after="{space_after}"/>'
    if align: ppr += f'<w:jc w:val="{align}"/>'
    return f"<w:p><w:pPr>{ppr}</w:pPr>{''.join(runs)}</w:p>"


def cell(content, width, align=None, shade=None, borders=True, bold=False, size=None):
    tcpr = f'<w:tcW w:w="{width}" w:type="dxa"/>'
    if shade: tcpr += f'<w:shd w:val="clear" w:color="auto" w:fill="{shade}"/>'
    if not borders: tcpr += '<w:tcBorders><w:top w:val="nil"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/></w:tcBorders>'
    tcpr += '<w:tcMar><w:top w:w="60" w:type="dxa"/><w:left w:w="100" w:type="dxa"/><w:bottom w:w="60" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tcMar>'
    if isinstance(content, str) and not content.lstrip().startswith("<w:p"):
        content = para(run(content, bold=bold, size=size), align=align, space_after=0)
    return f"<w:tc><w:tcPr>{tcpr}</w:tcPr>{content}</w:tc>"


def table(rows, widths, borders=True):
    b = '<w:tblBorders><w:top w:val="single" w:sz="6" w:color="000000"/><w:left w:val="single" w:sz="6" w:color="000000"/><w:bottom w:val="single" w:sz="6" w:color="000000"/><w:right w:val="single" w:sz="6" w:color="000000"/><w:insideH w:val="single" w:sz="6" w:color="000000"/><w:insideV w:val="single" w:sz="6" w:color="000000"/></w:tblBorders>' if borders else '<w:tblBorders><w:top w:val="nil"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/><w:insideH w:val="nil"/><w:insideV w:val="nil"/></w:tblBorders>'
    grid = "".join(f'<w:gridCol w:w="{w}"/>' for w in widths)
    return f'<w:tbl><w:tblPr><w:tblW w:w="{sum(widths)}" w:type="dxa"/>{b}<w:tblLayout w:type="fixed"/></w:tblPr><w:tblGrid>{grid}</w:tblGrid>{"".join(rows)}</w:tbl>'


def row(cells):
    return f"<w:tr>{''.join(cells)}</w:tr>"


def document(body, footer_text=None):
    sect = '<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/>'
    if footer_text:
        sect = '<w:sectPr><w:footerReference w:type="default" r:id="rIdFooter"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/>'
    sect += "</w:sectPr>"
    return f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document {W} xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>{body}{sect}</w:body></w:document>'


def write_docx(path, body, footer_text=None):
    styles = f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles {W}><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:cs="Arial"/><w:sz w:val="20"/><w:szCs w:val="20"/><w:lang w:val="en-US"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="60" w:line="264" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style></w:styles>'
    rels = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>'
    doc_rels = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>' + ('<Relationship Id="rIdFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>' if footer_text else '') + '</Relationships>'
    ct = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>' + ('<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>' if footer_text else '') + '</Types>'
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("[Content_Types].xml", ct)
        z.writestr("_rels/.rels", rels)
        z.writestr("word/_rels/document.xml.rels", doc_rels)
        z.writestr("word/styles.xml", styles)
        z.writestr("word/document.xml", document(body, footer_text))
        if footer_text:
            z.writestr("word/footer1.xml", f'<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:ftr {W}>{para(run(footer_text, size=9, color="555555"), align="center")}</w:ftr>')
    print("wrote", path)


def simple():
    body = ""
    body += para(run("{{title}}", bold=True, size=24, color="2563EB"), run("   {{number}}", size=14), space_after=120)
    body += table([row([
        cell(para(run("{{company.name}}", bold=True, size=12), space_after=0) + para(run("{{company.address1}}"), space_after=0) + para(run("{{company.postal_code}} {{company.city}}, {{company.country}}"), space_after=0) + para(run("{{company.email}}  {{company.phone}}"), space_after=0) + para(run("{{#company.tax_id}}Tax ID: {{company.tax_id}}{{/company.tax_id}}"), space_after=0), 5000, borders=False),
        cell(para(run("Bill to", bold=True, color="666666"), space_after=0) + para(run("{{client.name}}", bold=True), space_after=0) + para(run("{{client.contact}}"), space_after=0) + para(run("{{client.address}}"), space_after=0) + para(run("{{client.email}}"), space_after=0), 4638, borders=False),
    ])], [5000, 4638], borders=False)
    body += para(run(""), space_after=120)
    body += table([
        row([cell("Invoice date", 2400, bold=True), cell("{{issue_date}}", 2400), cell("Due date", 2400, bold=True), cell("{{due_date}}", 2438)]),
        row([cell("Service period", 2400, bold=True), cell("{{period}}", 2400), cell("PO number", 2400, bold=True), cell("{{po_number}}", 2438)]),
    ], [2400, 2400, 2400, 2438])
    body += para(run(""), space_after=160)
    body += table([
        row([cell("Description", 5038, shade="2563EB", bold=True), cell("Qty", 1200, align="right", shade="2563EB", bold=True), cell("Rate", 1600, align="right", shade="2563EB", bold=True), cell("Amount", 1800, align="right", shade="2563EB", bold=True)]),
        row([cell("{{#items}}{{description}}", 5038), cell("{{quantity_unit}}", 1200, align="right"), cell("{{unit_price}}", 1600, align="right"), cell("{{amount}}{{/items}}", 1800, align="right")]),
        row([cell("", 5038, borders=False), cell("", 1200, borders=False), cell("Subtotal", 1600, align="right", bold=True), cell("{{subtotal}}", 1800, align="right")]),
        row([cell("", 5038, borders=False), cell("", 1200, borders=False), cell("{{#taxes}}{{label}}", 1600, align="right"), cell("{{amount}}{{/taxes}}", 1800, align="right")]),
        row([cell("", 5038, borders=False), cell("", 1200, borders=False), cell("Total", 1600, align="right", bold=True, size=11), cell("{{total}}", 1800, align="right", bold=True, size=11)]),
        row([cell("", 5038, borders=False), cell("", 1200, borders=False), cell("{{#has_payments}}Paid{{/has_payments}}", 1600, align="right"), cell("{{#has_payments}}-{{amount_paid}}{{/has_payments}}", 1800, align="right")]),
        row([cell("", 5038, borders=False), cell("", 1200, borders=False), cell("Balance due", 1600, align="right", bold=True, shade="DBEAFE"), cell("{{balance}}", 1800, align="right", bold=True, shade="DBEAFE")]),
        row([cell("", 5038, borders=False), cell("", 1200, borders=False), cell("{{#is_foreign_currency}}{{base_total_label}}{{/is_foreign_currency}}", 1600, align="right", bold=True), cell("{{#is_foreign_currency}}{{total_in_base}}{{/is_foreign_currency}}", 1800, align="right", bold=True)]),
    ], [5038, 1200, 1600, 1800])
    body += para(run("{{#is_foreign_currency}}{{base_note}}{{/is_foreign_currency}}", italic=True, size=9), space_before=120)
    body += para(run("{{#custom_fields}}{{label}}: {{value}}", size=9), space_after=0) + para(run("{{/custom_fields}}"), space_after=0)
    body += para(run("Payment details", bold=True, color="666666"), space_before=200, space_after=40) + para(run("{{payment_details}}"))
    body += para(run("{{#notes}}Notes{{/notes}}", bold=True, color="666666"), space_before=160, space_after=40) + para(run("{{notes}}"))
    body += para(run("{{#terms}}Terms{{/terms}}", bold=True, color="666666"), space_before=160, space_after=40) + para(run("{{terms}}"))
    body += para(run("{{#paid}}PAID — thank you!{{/paid}}", bold=True, color="16A34A", size=14), space_before=200)
    return body


def wisestack():
    body = ""
    body += table([row([cell(
        para(run("{{company.name}}", bold=True, size=12), space_after=0) + para(run("JIB: {{company.tax_id}}", bold=True), space_after=0) + para(run("{{company.address1}},", bold=True), space_after=0) + para(run("{{company.postal_code}} {{company.city}}", bold=True), space_after=0), 9638)])], [9638])
    body += para(run(""), space_after=200)
    body += para(run("{{client.name}}", bold=True, size=9), space_after=0)
    body += para(run("{{client.address1}}, {{client.city}} {{client.postal_code}}", bold=True), space_after=200)
    body += table([
        row([cell("Invoice date:", 2200, bold=True, borders=False), cell("{{issue_date}}", 7438, borders=False)]),
        row([cell("Invoice no.:", 2200, bold=True, borders=False), cell("{{number}}", 7438, borders=False)]),
        row([cell("Services period:", 2200, bold=True, borders=False), cell("{{period_start}} to {{period_end}}", 7438, borders=False)]),
    ], [2200, 7438], borders=False)
    body += para(run(""), space_after=160)
    body += table([
        row([cell("DESCRIPTION:", 5638, bold=True), cell("HOURS", 2000, align="center", bold=True), cell("{{currency}}", 2000, align="center", bold=True)]),
        row([cell("{{#items}}-   {{description}}", 5638), cell("{{quantity_unit}}", 2000, align="center"), cell("{{amount}}{{/items}}", 2000, align="center")]),
        row([cell("TOTAL", 5638, bold=True, size=12), cell("{{quantity_total}}", 2000, align="center", bold=True, size=12), cell("{{total}}", 2000, align="center", bold=True, size=12)]),
    ], [5638, 2000, 2000])
    body += para(run(""), space_after=200)
    body += para(run("TRANSFER DETAILS:", bold=True), space_after=0)
    body += para(run("{{payment_details}}", bold=True), space_after=200)
    body += para(run("{{#is_foreign_currency}}{{base_note}}{{/is_foreign_currency}}"), space_after=80)
    body += para(run("{{#custom_fields}}{{label}}: {{value}}"), space_after=0) + para(run("{{/custom_fields}}"), space_after=200)
    body += para(run("ACCOUNT HOLDER DETAILS", bold=True), space_after=40)
    body += para(run("Name: ", bold=True), run("{{company.name}}"), space_after=0)
    body += para(run("Country: ", bold=True), run("{{company.country}}"), space_after=0)
    body += para(run("City: ", bold=True), run("{{company.city}}"), space_after=0)
    body += para(run("Address: ", bold=True), run("{{company.address1}}"), space_after=0)
    body += para(run("Postal Code: ", bold=True), run("{{company.postal_code}}"), space_after=300)
    body += para(run("_____________________"), align="right", space_after=0)
    body += para(run("Odgovorno lice", size=9), align="right")
    return body


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "docs/templates"
    os.makedirs(out, exist_ok=True)
    write_docx(os.path.join(out, "invoice-simple.docx"), simple(), footer_text="{{company.name}} · {{company.email}} · {{public_url}}")
    write_docx(os.path.join(out, "invoice-wisestack.docx"), wisestack(), footer_text="Adresa: {{company.address1}}, {{company.city}} / GSM: {{company.phone}}")
