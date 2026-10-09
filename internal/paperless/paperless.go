// Package paperless is a small client for the Paperless-ngx REST API.
//
// Only the handful of endpoints the app needs are covered: searching and
// downloading documents, uploading (consuming) files with metadata, polling
// consume tasks, and resolving tag / correspondent / document-type names.
package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to one Paperless-ngx instance.
type Client struct {
	BaseURL string // e.g. http://paperless:8000 (no trailing slash)
	Token   string
	HTTP    *http.Client

	mu      sync.Mutex
	names   map[string]map[int64]string // kind -> id -> name
	ids     map[string]map[string]int64 // kind -> lower(name) -> id
	namesAt time.Time
}

// New returns a client; it is usable when both URL and token are non-empty.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Token: strings.TrimSpace(token), HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Configured reports whether URL and token are present.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// Document is a trimmed-down Paperless document record.
type Document struct {
	ID               int64    `json:"id"`
	Title            string   `json:"title"`
	Created          string   `json:"created"` // YYYY-MM-DD
	Added            string   `json:"added"`
	Modified         string   `json:"modified"`
	Correspondent    string   `json:"correspondent"`
	DocumentType     string   `json:"document_type"`
	Tags             []string `json:"tags"`
	OriginalFileName string   `json:"original_file_name"`
	ArchivedFileName string   `json:"archived_file_name"`
	ASN              *int64   `json:"archive_serial_number"`
	URL              string   `json:"url"` // UI link
}

// Page is one page of search results.
type Page struct {
	Count   int        `json:"count"`
	Page    int        `json:"page"`
	Pages   int        `json:"pages"`
	Results []Document `json:"results"`
}

// Status is what the connection test returns.
type Status struct {
	OK            bool   `json:"ok"`
	Version       string `json:"version"`
	DocumentCount int    `json:"document_count"`
	Error         string `json:"error,omitempty"`
}

// Task is a consume task.
type Task struct {
	TaskID          string `json:"task_id"`
	Status          string `json:"status"` // PENDING STARTED SUCCESS FAILURE
	Result          string `json:"result"`
	RelatedDocument int64  `json:"related_document"`
}

// UploadOptions carries the metadata sent with a file.
type UploadOptions struct {
	Title         string
	Created       string // YYYY-MM-DD
	Correspondent string // name, created when missing
	DocumentType  string // name, created when missing
	Tags          []string
	ASN           int64
}

func (c *Client) req(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Response, error) {
	if !c.Configured() {
		return nil, errors.New("paperless is not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	r, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Token "+c.Token)
	r.Header.Set("Accept", "application/json; version=9")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return nil, fmt.Errorf("paperless: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(b))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			msg = "authentication failed (check the API token)"
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("paperless: %s %s → %d %s", method, path, resp.StatusCode, msg)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	resp, err := c.req(ctx, http.MethodGet, path, q, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp.Header, json.NewDecoder(resp.Body).Decode(out)
}

// Ping checks the connection and returns version/document count.
func (c *Client) Ping(ctx context.Context) Status {
	var page struct {
		Count int `json:"count"`
	}
	h, err := c.getJSON(ctx, "/api/documents/", url.Values{"page_size": {"1"}}, &page)
	if err != nil {
		return Status{Error: err.Error()}
	}
	return Status{OK: true, Version: h.Get("X-Version"), DocumentCount: page.Count}
}

type rawDoc struct {
	ID               int64   `json:"id"`
	Title            string  `json:"title"`
	Created          string  `json:"created"`
	CreatedDate      string  `json:"created_date"`
	Added            string  `json:"added"`
	Modified         string  `json:"modified"`
	Correspondent    *int64  `json:"correspondent"`
	DocumentType     *int64  `json:"document_type"`
	Tags             []int64 `json:"tags"`
	OriginalFileName string  `json:"original_file_name"`
	ArchivedFileName string  `json:"archived_file_name"`
	ASN              *int64  `json:"archive_serial_number"`
}

func (c *Client) convert(ctx context.Context, r rawDoc) Document {
	d := Document{ID: r.ID, Title: r.Title, Created: r.CreatedDate, Added: r.Added, Modified: r.Modified, OriginalFileName: r.OriginalFileName, ArchivedFileName: r.ArchivedFileName, ASN: r.ASN, Tags: []string{}}
	if d.Created == "" && len(r.Created) >= 10 {
		d.Created = r.Created[:10]
	}
	if r.Correspondent != nil {
		d.Correspondent = c.name(ctx, "correspondents", *r.Correspondent)
	}
	if r.DocumentType != nil {
		d.DocumentType = c.name(ctx, "document_types", *r.DocumentType)
	}
	for _, t := range r.Tags {
		if n := c.name(ctx, "tags", t); n != "" {
			d.Tags = append(d.Tags, n)
		}
	}
	d.URL = c.DocumentURL(d.ID)
	return d
}

// DocumentURL returns the UI link for a document (relative to BaseURL).
func (c *Client) DocumentURL(id int64) string {
	return fmt.Sprintf("%s/documents/%d/details", c.BaseURL, id)
}

// Search lists documents matching a full-text query (empty = newest first).
func (c *Client) Search(ctx context.Context, query string, page, pageSize int, extra url.Values) (*Page, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 25
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(pageSize)}, "ordering": {"-created"}}
	if strings.TrimSpace(query) != "" {
		q.Set("query", strings.TrimSpace(query))
		q.Del("ordering")
	}
	for k, v := range extra {
		q[k] = v
	}
	var raw struct {
		Count   int      `json:"count"`
		Results []rawDoc `json:"results"`
	}
	if _, err := c.getJSON(ctx, "/api/documents/", q, &raw); err != nil {
		return nil, err
	}
	out := &Page{Count: raw.Count, Page: page, Results: []Document{}}
	out.Pages = (raw.Count + pageSize - 1) / pageSize
	for _, r := range raw.Results {
		out.Results = append(out.Results, c.convert(ctx, r))
	}
	return out, nil
}

// Get returns one document.
func (c *Client) Get(ctx context.Context, id int64) (*Document, error) {
	var r rawDoc
	if _, err := c.getJSON(ctx, fmt.Sprintf("/api/documents/%d/", id), nil, &r); err != nil {
		return nil, err
	}
	d := c.convert(ctx, r)
	return &d, nil
}

// Download streams the archived (or original) file. The caller closes the body.
func (c *Client) Download(ctx context.Context, id int64, original bool) (*http.Response, error) {
	q := url.Values{}
	if original {
		q.Set("original", "true")
	}
	return c.req(ctx, http.MethodGet, fmt.Sprintf("/api/documents/%d/download/", id), q, nil, "")
}

// Thumbnail streams the thumbnail image. The caller closes the body.
func (c *Client) Thumbnail(ctx context.Context, id int64) (*http.Response, error) {
	return c.req(ctx, http.MethodGet, fmt.Sprintf("/api/documents/%d/thumb/", id), nil, nil, "")
}

// Upload sends a file to the consumer and returns the task id.
func (c *Client) Upload(ctx context.Context, filename string, data []byte, o UploadOptions) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("document", filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	if o.Title != "" {
		_ = mw.WriteField("title", o.Title)
	}
	if o.Created != "" {
		_ = mw.WriteField("created", o.Created)
	}
	if o.Correspondent != "" {
		if id, err := c.ensure(ctx, "correspondents", o.Correspondent); err == nil && id > 0 {
			_ = mw.WriteField("correspondent", strconv.FormatInt(id, 10))
		}
	}
	if o.DocumentType != "" {
		if id, err := c.ensure(ctx, "document_types", o.DocumentType); err == nil && id > 0 {
			_ = mw.WriteField("document_type", strconv.FormatInt(id, 10))
		}
	}
	for _, t := range o.Tags {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if id, err := c.ensure(ctx, "tags", t); err == nil && id > 0 {
			_ = mw.WriteField("tags", strconv.FormatInt(id, 10))
		}
	}
	if o.ASN > 0 {
		_ = mw.WriteField("archive_serial_number", strconv.FormatInt(o.ASN, 10))
	}
	mw.Close()
	resp, err := c.req(ctx, http.MethodPost, "/api/documents/post_document/", nil, &buf, mw.FormDataContentType())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	var task string
	if err := json.Unmarshal(b, &task); err != nil {
		task = strings.Trim(strings.TrimSpace(string(b)), `"`)
	}
	if task == "" {
		return "", errors.New("paperless: upload accepted but no task id returned")
	}
	return task, nil
}

// Task fetches the state of a consume task.
func (c *Client) Task(ctx context.Context, taskID string) (*Task, error) {
	var list []struct {
		TaskID          string `json:"task_id"`
		Status          string `json:"status"`
		Result          string `json:"result"`
		RelatedDocument any    `json:"related_document"`
	}
	if _, err := c.getJSON(ctx, "/api/tasks/", url.Values{"task_id": {taskID}}, &list); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return &Task{TaskID: taskID, Status: "PENDING"}, nil
	}
	t := list[0]
	out := &Task{TaskID: t.TaskID, Status: t.Status, Result: t.Result}
	switch v := t.RelatedDocument.(type) {
	case string:
		out.RelatedDocument, _ = strconv.ParseInt(v, 10, 64)
	case float64:
		out.RelatedDocument = int64(v)
	}
	if out.RelatedDocument == 0 && out.Status == "SUCCESS" {
		// Older versions only mention the id in the result text: "Success. New document id 123 created"
		for _, w := range strings.Fields(strings.ReplaceAll(out.Result, ".", " ")) {
			if n, err := strconv.ParseInt(w, 10, 64); err == nil && n > 0 {
				out.RelatedDocument = n
			}
		}
	}
	return out, nil
}

// WaitTask polls a task until it finishes or the context ends.
func (c *Client) WaitTask(ctx context.Context, taskID string, interval time.Duration) (*Task, error) {
	for {
		t, err := c.Task(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if t.Status == "SUCCESS" || t.Status == "FAILURE" {
			return t, nil
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ---------- names (tags, correspondents, document types) ----------

func (c *Client) loadNames(ctx context.Context) {
	c.mu.Lock()
	fresh := c.names != nil && time.Since(c.namesAt) < 5*time.Minute
	c.mu.Unlock()
	if fresh {
		return
	}
	names := map[string]map[int64]string{}
	ids := map[string]map[string]int64{}
	for _, kind := range []string{"tags", "correspondents", "document_types"} {
		names[kind] = map[int64]string{}
		ids[kind] = map[string]int64{}
		for page := 1; page < 50; page++ {
			var raw struct {
				Next    *string `json:"next"`
				Results []struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"results"`
			}
			if _, err := c.getJSON(ctx, "/api/"+kind+"/", url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}}, &raw); err != nil {
				break
			}
			for _, r := range raw.Results {
				names[kind][r.ID] = r.Name
				ids[kind][strings.ToLower(r.Name)] = r.ID
			}
			if raw.Next == nil || *raw.Next == "" {
				break
			}
		}
	}
	c.mu.Lock()
	c.names, c.ids, c.namesAt = names, ids, time.Now()
	c.mu.Unlock()
}

func (c *Client) name(ctx context.Context, kind string, id int64) string {
	c.loadNames(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[kind][id]
}

// ensure returns the id of a tag/correspondent/document type by name, creating it when missing.
func (c *Client) ensure(ctx context.Context, kind, name string) (int64, error) {
	c.loadNames(ctx)
	key := strings.ToLower(strings.TrimSpace(name))
	c.mu.Lock()
	id := c.ids[kind][key]
	c.mu.Unlock()
	if id > 0 {
		return id, nil
	}
	body, _ := json.Marshal(map[string]any{"name": strings.TrimSpace(name), "matching_algorithm": 0})
	resp, err := c.req(ctx, http.MethodPost, "/api/"+kind+"/", nil, bytes.NewReader(body), "application/json")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return 0, err
	}
	c.mu.Lock()
	if c.names[kind] == nil {
		c.names[kind] = map[int64]string{}
		c.ids[kind] = map[string]int64{}
	}
	c.names[kind][created.ID] = created.Name
	c.ids[kind][key] = created.ID
	c.mu.Unlock()
	return created.ID, nil
}

// Names returns all known names of one kind (for filter dropdowns).
func (c *Client) Names(ctx context.Context, kind string) []string {
	c.loadNames(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.names[kind]))
	for _, n := range c.names[kind] {
		out = append(out, n)
	}
	return out
}
