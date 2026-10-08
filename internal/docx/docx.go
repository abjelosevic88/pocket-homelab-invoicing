package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	paraRe  = regexp.MustCompile(`(?s)<w:p[ >].*?</w:p>|<w:p/>`)
	runRe   = regexp.MustCompile(`(?s)<w:r[ >].*?</w:r>`)
	textRe  = regexp.MustCompile(`(?s)<w:t(?: [^>]*)?>(.*?)</w:t>|<w:t(?: [^>]*)?/>`)
	rowRe   = regexp.MustCompile(`(?s)<w:tr[ >].*?</w:tr>`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	openRe  = regexp.MustCompile(`\{\{#\s*([\w.]+)\s*\}\}`)
	closeRe = regexp.MustCompile(`\{\{/\s*([\w.]+)\s*\}\}`)
	brRe    = regexp.MustCompile(`<w:br/>|<w:tab/>`)
)

// listKeys are sections that repeat table rows.
var listKeys = map[string]bool{"items": true, "taxes": true, "custom_fields": true}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\r':
		case '\n':
			b.WriteString(`</w:t><w:br/><w:t xml:space="preserve">`)
		case '\t':
			b.WriteString(`</w:t><w:tab/><w:t xml:space="preserve">`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func xmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'").Replace(s)
}

// paragraphText returns the visible text of a paragraph XML fragment.
func paragraphText(p string) string {
	var b strings.Builder
	for _, m := range textRe.FindAllStringSubmatch(p, -1) {
		b.WriteString(xmlUnescape(m[1]))
	}
	return b.String()
}

// mergeRuns rewrites every paragraph that contains a placeholder so that the whole
// paragraph text lives in its first run. Word splits text into runs unpredictably
// (spell-check, formatting), which would otherwise break "{{number}}" into pieces.
func mergeRuns(xml string) string {
	return paraRe.ReplaceAllStringFunc(xml, func(p string) string {
		if !strings.Contains(paragraphText(p), "{{") {
			return p
		}
		runs := runRe.FindAllStringIndex(p, -1)
		if len(runs) == 0 {
			return p
		}
		full := paragraphText(p)
		first := p[runs[0][0]:runs[0][1]]
		// put the merged text into the first <w:t> of the first run, drop the others
		seen := false
		first = textRe.ReplaceAllStringFunc(first, func(t string) string {
			if seen {
				return ""
			}
			seen = true
			return `<w:t xml:space="preserve">` + xmlEscape(full) + `</w:t>`
		})
		if !seen { // first run had no text element (e.g. only a tab); append one
			first = strings.Replace(first, "</w:r>", `<w:t xml:space="preserve">`+xmlEscape(full)+`</w:t></w:r>`, 1)
		}
		var b strings.Builder
		b.WriteString(p[:runs[0][0]])
		b.WriteString(first)
		last := runs[0][1]
		for _, r := range runs[1:] {
			b.WriteString(p[last:r[0]]) // keep non-run content between runs (bookmarks, etc.)
			last = r[1]
		}
		b.WriteString(p[last:])
		return b.String()
	})
}

// hoistRowSections moves list section markers that sit inside table rows to wrap the
// rows themselves, so "{{#items}}" in the first cell and "{{/items}}" in the last cell
// repeat the whole row per item.
func hoistRowSections(xml string) string {
	rows := rowRe.FindAllStringIndex(xml, -1)
	if len(rows) == 0 {
		return xml
	}
	type rowInfo struct {
		start, end    int
		opens, closes []string
	}
	infos := make([]rowInfo, 0, len(rows))
	for _, r := range rows {
		txt := paragraphText(xml[r[0]:r[1]])
		ri := rowInfo{start: r[0], end: r[1]}
		for _, m := range openRe.FindAllStringSubmatch(txt, -1) {
			if listKeys[m[1]] {
				ri.opens = append(ri.opens, m[1])
			}
		}
		for _, m := range closeRe.FindAllStringSubmatch(txt, -1) {
			if listKeys[m[1]] {
				ri.closes = append(ri.closes, m[1])
			}
		}
		infos = append(infos, ri)
	}
	var b strings.Builder
	last := 0
	for _, ri := range infos {
		b.WriteString(xml[last:ri.start])
		row := xml[ri.start:ri.end]
		prefix, suffix := "", ""
		for _, k := range ri.opens {
			row = stripMarker(row, openRe, k)
			prefix += "{{#" + k + "}}"
		}
		for _, k := range ri.closes {
			row = stripMarker(row, closeRe, k)
			suffix += "{{/" + k + "}}"
		}
		b.WriteString(prefix + row + suffix)
		last = ri.end
	}
	b.WriteString(xml[last:])
	return b.String()
}

func stripMarker(row string, re *regexp.Regexp, key string) string {
	return re.ReplaceAllStringFunc(row, func(m string) string {
		if re.FindStringSubmatch(m)[1] == key {
			return ""
		}
		return m
	})
}

// FillXML renders one WordprocessingML part.
func FillXML(xml string, data map[string]any) string {
	xml = mergeRuns(xml)
	xml = hoistRowSections(xml)
	return Render(xml, data, xmlEscape)
}

// Fill renders a .docx template with data and returns the filled .docx bytes.
func Fill(template []byte, data map[string]any) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(template), int64(len(template)))
	if err != nil {
		return nil, fmt.Errorf("not a .docx (zip) file: %w", err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	found := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		name := f.Name
		if name == "word/document.xml" || (strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml")) || (strings.HasPrefix(name, "word/footer") && strings.HasSuffix(name, ".xml")) {
			content = []byte(FillXML(string(content), data))
			if name == "word/document.xml" {
				found = true
			}
		}
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.Modified = time.Now()
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(content); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, fmt.Errorf("word/document.xml not found; is this a Word .docx file?")
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Placeholders lists the {{…}} tokens used in a template (for validation / UI hints).
func Placeholders(template []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(template), int64(len(template)))
	if err != nil {
		return nil, fmt.Errorf("not a .docx (zip) file: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") || !strings.HasPrefix(f.Name, "word/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		txt := mergeRuns(string(content))
		for _, p := range paraRe.FindAllString(txt, -1) {
			for _, m := range regexp.MustCompile(`\{\{[^}]*\}\}`).FindAllString(paragraphText(p), -1) {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		}
	}
	return out, nil
}

// Converter turns a .docx into a PDF.
type Converter interface {
	Name() string
	Convert(ctx context.Context, docx []byte) ([]byte, error)
}

// Gotenberg converts via a Gotenberg service (LibreOffice route).
type Gotenberg struct {
	URL    string
	Client *http.Client
}

// Name implements Converter.
func (Gotenberg) Name() string { return "gotenberg" }

// Convert implements Converter.
func (g Gotenberg) Convert(ctx context.Context, doc []byte) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "invoice.docx")
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(doc); err != nil {
		return nil, err
	}
	mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.URL, "/")+"/forms/libreoffice/convert", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg: %w (is the gotenberg container running?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gotenberg: HTTP %d: %s", resp.StatusCode, string(msg))
	}
	return io.ReadAll(resp.Body)
}

// LibreOffice converts with a local soffice binary.
type LibreOffice struct{ Path string }

// Name implements Converter.
func (LibreOffice) Name() string { return "libreoffice" }

// Convert implements Converter.
func (l LibreOffice) Convert(ctx context.Context, doc []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "pi-docx-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "invoice.docx")
	if err := os.WriteFile(in, doc, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, l.Path, "--headless", "--convert-to", "pdf", "--outdir", dir, in)
	cmd.Env = append(os.Environ(), "HOME="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("libreoffice: %w: %s", err, string(out))
	}
	return os.ReadFile(filepath.Join(dir, "invoice.pdf"))
}

// None reports that no converter is configured.
type None struct{}

// Name implements Converter.
func (None) Name() string { return "none" }

// Convert implements Converter.
func (None) Convert(context.Context, []byte) ([]byte, error) {
	return nil, fmt.Errorf("no DOCX→PDF converter configured: run the gotenberg container (docker compose --profile gotenberg up -d) or set DOCX_CONVERTER=libreoffice with LIBREOFFICE_PATH")
}

// NewConverter builds a converter from configuration.
func NewConverter(name, gotenbergURL, libreofficePath string) Converter {
	switch strings.ToLower(name) {
	case "gotenberg":
		return Gotenberg{URL: gotenbergURL}
	case "libreoffice", "soffice":
		return LibreOffice{Path: libreofficePath}
	default:
		return None{}
	}
}
