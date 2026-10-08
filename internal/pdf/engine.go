package pdf

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

//go:embed default_template.html
var defaultTemplateHTML string

// DefaultTemplateHTML returns the built-in HTML template source.
func DefaultTemplateHTML() string { return defaultTemplateHTML }

// RenderHTML renders the document with an HTML template (empty = built-in).
func RenderHTML(d *Document, tmplSrc string) ([]byte, error) {
	if tmplSrc == "" {
		tmplSrc = defaultTemplateHTML
	}
	t, err := template.New("invoice").Parse(tmplSrc)
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	return buf.Bytes(), nil
}

// Engine renders a Document to PDF bytes.
type Engine interface {
	Name() string
	Render(ctx context.Context, d *Document, tmplHTML string, logo []byte, logoType string) ([]byte, error)
}

// Native is the pure-Go engine.
type Native struct{}

// Name implements Engine.
func (Native) Name() string { return "native" }

// Render implements Engine.
func (Native) Render(_ context.Context, d *Document, _ string, logo []byte, logoType string) ([]byte, error) {
	return RenderNative(d, logo, logoType)
}

// Chromium shells out to a headless browser binary.
type Chromium struct{ Path string }

// Name implements Engine.
func (Chromium) Name() string { return "chromium" }

// Render implements Engine.
func (c Chromium) Render(ctx context.Context, d *Document, tmplHTML string, _ []byte, _ string) ([]byte, error) {
	html, err := RenderHTML(d, tmplHTML)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "pi-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "invoice.html")
	out := filepath.Join(dir, "invoice.pdf")
	if err := os.WriteFile(in, html, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Path, "--headless=new", "--no-sandbox", "--disable-gpu", "--no-pdf-header-footer", "--print-to-pdf="+out, "file://"+in)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("chromium: %w: %s", err, string(output))
	}
	return os.ReadFile(out)
}

// Gotenberg posts HTML to a Gotenberg service.
type Gotenberg struct {
	URL    string
	Client *http.Client
}

// Name implements Engine.
func (Gotenberg) Name() string { return "gotenberg" }

// Render implements Engine.
func (g Gotenberg) Render(ctx context.Context, d *Document, tmplHTML string, _ []byte, _ string) ([]byte, error) {
	html, err := RenderHTML(d, tmplHTML)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "index.html")
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(html); err != nil {
		return nil, err
	}
	_ = mw.WriteField("paperWidth", "8.27")
	_ = mw.WriteField("paperHeight", "11.7")
	_ = mw.WriteField("marginTop", "0")
	_ = mw.WriteField("marginBottom", "0")
	_ = mw.WriteField("marginLeft", "0")
	_ = mw.WriteField("marginRight", "0")
	_ = mw.WriteField("printBackground", "true")
	mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.URL+"/forms/chromium/convert/html", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gotenberg: HTTP %d: %s", resp.StatusCode, string(msg))
	}
	return io.ReadAll(resp.Body)
}

// NewEngine builds an engine by name.
func NewEngine(name, chromiumPath, gotenbergURL string) Engine {
	switch name {
	case "chromium":
		return Chromium{Path: chromiumPath}
	case "gotenberg":
		return Gotenberg{URL: gotenbergURL}
	default:
		return Native{}
	}
}
