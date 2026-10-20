// Package pocketdocs is a small client for the Pocket Documents REST API
// (/api/v1, Authorization: Bearer <token>).
//
// The token is either a person's own pd_ API token (a direct connection) or a
// short-lived Pocket Core link token (pcx_) with the documents:read and
// documents:write scopes. Only what Pocket Invoicing needs is covered: listing
// and fetching documents, file / thumbnail downloads, uploads with metadata,
// and resolving tag / correspondent / document-type names (created when missing).
package pocketdocs

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kinds of named objects.
const (
	Correspondents = "correspondents"
	DocumentTypes  = "document_types"
	Tags           = "tags"
)

// Client talks to one person's Pocket Documents.
type Client struct {
	BaseURL string // API host, e.g. http://pocket-documents:8080 (no trailing slash, no /api/v1)
	Token   string
	HTTP    *http.Client
	names   *Names
}

// New returns a client. names may be shared between clients of the same person
// and instance (Core link tokens change on every use, the names do not); nil
// gives the client a cache of its own.
func New(baseURL, token string, names *Names) *Client {
	if names == nil {
		names = &Names{}
	}
	return &Client{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Token: strings.TrimSpace(token), HTTP: &http.Client{Timeout: 60 * time.Second}, names: names}
}

// Configured reports whether URL and token are present.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// Names caches one person's tags, correspondents and document types for a few minutes.
type Names struct {
	mu    sync.Mutex
	names map[string]map[int64]string // kind -> id -> name
	ids   map[string]map[string]int64 // kind -> lower(name) -> id
	at    time.Time
}

// Document is a trimmed-down Pocket Documents record with names resolved.
type Document struct {
	ID               int64    `json:"id"`
	Title            string   `json:"title"`
	Created          string   `json:"created"` // YYYY-MM-DD
	Added            string   `json:"added"`
	Modified         string   `json:"modified"`
	Correspondent    string   `json:"correspondent"`
	CorrespondentID  int64    `json:"correspondent_id"`
	DocumentType     string   `json:"document_type"`
	Tags             []string `json:"tags"`
	OriginalFileName string   `json:"original_file_name"`
	ASN              *int64   `json:"asn"`
	PageCount        int      `json:"page_count"`
	Status           string   `json:"status"`
	ExternalID       string   `json:"external_id,omitempty"`
	URL              string   `json:"url"` // browser link, filled by the caller
}

// Page is one page of results.
type Page struct {
	Count   int        `json:"count"`
	Page    int        `json:"page"`
	Pages   int        `json:"pages"`
	Results []Document `json:"results"`
}

// Status is what the connection test returns.
type Status struct {
	OK            bool   `json:"ok"`
	DocumentCount int    `json:"document_count"`
	Error         string `json:"error,omitempty"`
}

// UploadOptions carries the metadata sent with a file. Names are resolved to
// ids and created in Pocket Documents when missing.
type UploadOptions struct {
	Title         string
	Created       string // YYYY-MM-DD
	Correspondent string // name
	DocumentType  string // name
	Tags          []string
	ExternalID    string // e.g. invoicing:invoice:42 — a second upload with it answers 409
}

// DuplicateError is Pocket Documents refusing an upload because the same
// file, or a document with the same external id, is already there.
type DuplicateError struct {
	ID      int64
	Message string
}

func (e *DuplicateError) Error() string {
	if e.Message != "" {
		return "pocket documents: " + e.Message
	}
	return fmt.Sprintf("pocket documents: already there as #%d", e.ID)
}

// HTTPError is a non-2xx answer.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return "pocket documents: " + e.Message }

func (c *Client) req(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (*http.Response, error) {
	if !c.Configured() {
		return nil, errors.New("pocket documents is not configured")
	}
	u := c.BaseURL + "/api/v1" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	r, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+c.Token)
	r.Header.Set("Accept", "application/json")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return nil, fmt.Errorf("pocket documents: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error       string `json:"error"`
			DuplicateOf int64  `json:"duplicate_of"`
		}
		_ = json.Unmarshal(b, &e)
		if resp.StatusCode == http.StatusConflict && e.DuplicateOf > 0 {
			return nil, &DuplicateError{ID: e.DuplicateOf, Message: e.Error}
		}
		msg := e.Error
		if msg == "" {
			msg = strings.TrimSpace(string(b))
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			msg = "authentication failed (check the API token)"
		case http.StatusForbidden:
			msg = firstNonEmpty(msg, "not allowed")
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, &HTTPError{Status: resp.StatusCode, Message: fmt.Sprintf("%s %s → %d %s", method, path, resp.StatusCode, msg)}
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.req(ctx, http.MethodGet, path, q, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) sendJSON(ctx context.Context, method, path string, in, out any) error {
	b, _ := json.Marshal(in)
	resp, err := c.req(ctx, method, path, nil, bytes.NewReader(b), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping checks the connection and returns the person's document count.
func (c *Client) Ping(ctx context.Context) Status {
	var page struct {
		Count int `json:"count"`
	}
	if err := c.getJSON(ctx, "/documents", url.Values{"limit": {"1"}, "no_facets": {"1"}}, &page); err != nil {
		return Status{Error: err.Error()}
	}
	return Status{OK: true, DocumentCount: page.Count}
}

type rawDoc struct {
	ID                int64   `json:"id"`
	Title             string  `json:"title"`
	Created           string  `json:"created"`
	AddedAt           string  `json:"added_at"`
	ModifiedAt        string  `json:"modified_at"`
	Correspondent     *int64  `json:"correspondent"`
	DocumentType      *int64  `json:"document_type"`
	Tags              []int64 `json:"tags"`
	OriginalName      string  `json:"original_name"`
	ASN               *int64  `json:"asn"`
	PageCount         int     `json:"page_count"`
	Status            string  `json:"status"`
	ExternalID        string  `json:"external_id"`
	CorrespondentName string  `json:"correspondent_name"`
	DocumentTypeName  string  `json:"document_type_name"`
}

func (c *Client) convert(ctx context.Context, r rawDoc) Document {
	d := Document{ID: r.ID, Title: r.Title, Created: r.Created, Added: r.AddedAt, Modified: r.ModifiedAt, OriginalFileName: r.OriginalName,
		ASN: r.ASN, PageCount: r.PageCount, Status: r.Status, ExternalID: r.ExternalID, Tags: []string{},
		Correspondent: r.CorrespondentName, DocumentType: r.DocumentTypeName}
	if len(d.Created) > 10 {
		d.Created = d.Created[:10]
	}
	if r.Correspondent != nil {
		d.CorrespondentID = *r.Correspondent
		if d.Correspondent == "" {
			d.Correspondent = c.Name(ctx, Correspondents, *r.Correspondent)
		}
	}
	if r.DocumentType != nil && d.DocumentType == "" {
		d.DocumentType = c.Name(ctx, DocumentTypes, *r.DocumentType)
	}
	for _, t := range r.Tags {
		if n := c.Name(ctx, Tags, t); n != "" {
			d.Tags = append(d.Tags, n)
		}
	}
	return d
}

// Filter narrows a listing.
type Filter struct {
	Query           string // search syntax of Pocket Documents
	CorrespondentID int64
	ExternalID      string
}

// List returns one page of the person's documents, newest first.
func (c *Client) List(ctx context.Context, f Filter, page, pageSize int) (*Page, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 25
	}
	q := url.Values{"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa((page - 1) * pageSize)}, "no_facets": {"1"}}
	if s := strings.TrimSpace(f.Query); s != "" {
		q.Set("q", s)
	} else {
		q.Set("sort", "-created")
	}
	if f.CorrespondentID > 0 {
		q.Set("correspondent", strconv.FormatInt(f.CorrespondentID, 10))
	}
	if f.ExternalID != "" {
		q.Set("external_id", f.ExternalID)
	}
	var raw struct {
		Count   int      `json:"count"`
		Results []rawDoc `json:"results"`
	}
	if err := c.getJSON(ctx, "/documents", q, &raw); err != nil {
		return nil, err
	}
	out := &Page{Count: raw.Count, Page: page, Results: []Document{}}
	out.Pages = (raw.Count + pageSize - 1) / pageSize
	for _, r := range raw.Results {
		out.Results = append(out.Results, c.convert(ctx, r))
	}
	return out, nil
}

// FindByExternalID returns the person's live document with that external id, or nil.
func (c *Client) FindByExternalID(ctx context.Context, ext string) (*Document, error) {
	p, err := c.List(ctx, Filter{ExternalID: ext}, 1, 1)
	if err != nil {
		return nil, err
	}
	for _, d := range p.Results {
		if d.ExternalID == ext { // an older Documents without the filter would list anything
			return &d, nil
		}
	}
	return nil, nil
}

// Get returns one document.
func (c *Client) Get(ctx context.Context, id int64) (*Document, error) {
	var r struct {
		Document rawDoc `json:"document"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/documents/%d", id), nil, &r); err != nil {
		return nil, err
	}
	d := c.convert(ctx, r.Document)
	return &d, nil
}

// Download streams the archived PDF (or the original file). The caller closes the body.
func (c *Client) Download(ctx context.Context, id int64, original, inline bool) (*http.Response, error) {
	q := url.Values{}
	if original {
		q.Set("original", "1")
	}
	if inline {
		q.Set("inline", "1")
	}
	return c.req(ctx, http.MethodGet, fmt.Sprintf("/documents/%d/download", id), q, nil, "")
}

// Thumbnail streams the thumbnail image. The caller closes the body.
func (c *Client) Thumbnail(ctx context.Context, id int64) (*http.Response, error) {
	return c.req(ctx, http.MethodGet, fmt.Sprintf("/documents/%d/thumb", id), nil, nil, "")
}

// Upload adds a file with its metadata and returns the new document. Pocket
// Documents answers at once (OCR runs afterwards); when the same file or the
// same external id is already there the error is a *DuplicateError carrying
// the existing document's id.
func (c *Client) Upload(ctx context.Context, filename string, data []byte, o UploadOptions) (*Document, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("document", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	field := func(k, v string) {
		if v != "" {
			_ = mw.WriteField(k, v)
		}
	}
	field("title", o.Title)
	field("created", o.Created)
	field("external_id", o.ExternalID)
	if o.Correspondent != "" {
		if id, err := c.Ensure(ctx, Correspondents, o.Correspondent); err == nil && id > 0 {
			field("correspondent", strconv.FormatInt(id, 10))
		}
	}
	if o.DocumentType != "" {
		if id, err := c.Ensure(ctx, DocumentTypes, o.DocumentType); err == nil && id > 0 {
			field("document_type", strconv.FormatInt(id, 10))
		}
	}
	var tags []string
	for _, t := range o.Tags {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if id, err := c.Ensure(ctx, Tags, t); err == nil && id > 0 {
			tags = append(tags, strconv.FormatInt(id, 10))
		}
	}
	field("tags", strings.Join(tags, ","))
	mw.Close()
	resp, err := c.req(ctx, http.MethodPost, "/documents", nil, &buf, mw.FormDataContentType())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r rawDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("pocket documents: upload answer: %w", err)
	}
	if r.ID == 0 {
		return nil, errors.New("pocket documents: upload accepted but no document id returned")
	}
	d := c.convert(ctx, r)
	return &d, nil
}

// ---------- names (tags, correspondents, document types) ----------

type entry struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (c *Client) loadNames(ctx context.Context, force bool) {
	n := c.names
	n.mu.Lock()
	fresh := !force && n.names != nil && time.Since(n.at) < 5*time.Minute
	n.mu.Unlock()
	if fresh {
		return
	}
	var tax struct {
		Tags           []entry `json:"tags"`
		Correspondents []entry `json:"correspondents"`
		DocumentTypes  []entry `json:"document_types"`
	}
	if err := c.getJSON(ctx, "/taxonomy", nil, &tax); err != nil {
		return
	}
	names := map[string]map[int64]string{}
	ids := map[string]map[string]int64{}
	for kind, list := range map[string][]entry{Tags: tax.Tags, Correspondents: tax.Correspondents, DocumentTypes: tax.DocumentTypes} {
		names[kind] = map[int64]string{}
		ids[kind] = map[string]int64{}
		for _, e := range list {
			names[kind][e.ID] = e.Name
			ids[kind][strings.ToLower(strings.TrimSpace(e.Name))] = e.ID
		}
	}
	n.mu.Lock()
	n.names, n.ids, n.at = names, ids, time.Now()
	n.mu.Unlock()
}

func (c *Client) remember(kind string, id int64, name string) {
	n := c.names
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.names == nil {
		n.names, n.ids, n.at = map[string]map[int64]string{}, map[string]map[string]int64{}, time.Now()
	}
	if n.names[kind] == nil {
		n.names[kind], n.ids[kind] = map[int64]string{}, map[string]int64{}
	}
	n.names[kind][id] = name
	n.ids[kind][strings.ToLower(strings.TrimSpace(name))] = id
}

// Name returns the display name for an id, or "".
func (c *Client) Name(ctx context.Context, kind string, id int64) string {
	c.loadNames(ctx, false)
	c.names.mu.Lock()
	defer c.names.mu.Unlock()
	return c.names.names[kind][id]
}

// Lookup returns the id of an existing object by name (case-insensitive), or 0.
func (c *Client) Lookup(ctx context.Context, kind, name string) int64 {
	c.loadNames(ctx, false)
	c.names.mu.Lock()
	defer c.names.mu.Unlock()
	return c.names.ids[kind][strings.ToLower(strings.TrimSpace(name))]
}

// Ensure returns the id of a tag / correspondent / document type by name,
// creating it when missing (a 409 means someone else just created it: look again).
func (c *Client) Ensure(ctx context.Context, kind, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("pocket documents: a name is required")
	}
	if id := c.Lookup(ctx, kind, name); id > 0 {
		return id, nil
	}
	var created entry
	err := c.sendJSON(ctx, http.MethodPost, "/"+kind, map[string]any{"name": name, "matching_algorithm": 0}, &created)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusConflict {
		c.loadNames(ctx, true)
		if id := c.Lookup(ctx, kind, name); id > 0 {
			return id, nil
		}
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	if created.ID == 0 {
		return 0, errors.New("pocket documents: created without an id")
	}
	c.remember(kind, created.ID, firstNonEmpty(created.Name, name))
	return created.ID, nil
}

// Entry is a named object (tag, correspondent, document type).
type Entry struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Entries returns all known objects of one kind, sorted by name.
func (c *Client) Entries(ctx context.Context, kind string) []Entry {
	c.loadNames(ctx, false)
	c.names.mu.Lock()
	defer c.names.mu.Unlock()
	out := make([]Entry, 0, len(c.names.names[kind]))
	for id, n := range c.names.names[kind] {
		out = append(out, Entry{ID: id, Name: n})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Refresh reloads the names now (after objects were created or renamed elsewhere).
func (c *Client) Refresh(ctx context.Context) { c.loadNames(ctx, true) }

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
