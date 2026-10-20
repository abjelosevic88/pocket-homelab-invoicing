// Package pdtest is a fake Pocket Documents for tests: the handful of /api/v1
// routes Pocket Invoicing uses, with every token belonging to one person and
// every answer scoped to that person, like the real app.
package pdtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Doc is a stored document.
type Doc struct {
	ID            int64   `json:"id"`
	Person        string  `json:"-"`
	Title         string  `json:"title"`
	Created       string  `json:"created"`
	Correspondent *int64  `json:"correspondent"`
	DocumentType  *int64  `json:"document_type"`
	Tags          []int64 `json:"tags"`
	OriginalName  string  `json:"original_name"`
	ExternalID    string  `json:"external_id,omitempty"`
	Status        string  `json:"status"`
	CorrName      string  `json:"correspondent_name,omitempty"`
	Checksum      string  `json:"-"`
	Data          []byte  `json:"-"`
}

// Named is a tag, correspondent or document type.
type Named struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Person string `json:"-"`
	Kind   string `json:"-"`
}

// Server is the fake.
type Server struct {
	*httptest.Server
	mu     sync.Mutex
	tokens map[string]string // bearer token -> person
	docs   []*Doc
	names  []*Named
	nextID int64
	auth   []string // Authorization headers, in order
}

// New starts a fake Pocket Documents; Close runs with the test's cleanup.
func New(t *testing.T) *Server {
	f := &Server{tokens: map[string]string{}, nextID: 100}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// Token lets a bearer token act as a person.
func (f *Server) Token(token, person string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[token] = person
}

// Calls returns the Authorization headers seen so far.
func (f *Server) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.auth...)
}

// AddNamed creates a tag / correspondent / document type for a person.
func (f *Server) AddNamed(person, kind, name string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addNamed(person, kind, name).ID
}

func (f *Server) addNamed(person, kind, name string) *Named {
	f.nextID++
	n := &Named{ID: f.nextID, Name: name, Person: person, Kind: kind}
	f.names = append(f.names, n)
	return n
}

// AddDoc stores a document for a person.
func (f *Server) AddDoc(person string, d Doc) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	d.ID, d.Person = f.nextID, person
	if d.Status == "" {
		d.Status = "done"
	}
	f.docs = append(f.docs, &d)
	return d.ID
}

// Docs returns a person's documents.
func (f *Server) Docs(person string) []Doc {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Doc
	for _, d := range f.docs {
		if d.Person == person {
			out = append(out, *d)
		}
	}
	return out
}

// NameOf is the name of a person's tag / correspondent / type by id ("" when not theirs).
func (f *Server) NameOf(person string, id int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.names {
		if n.ID == id && n.Person == person {
			return n.Name
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Server) view(d *Doc) Doc {
	v := *d
	v.CorrName = ""
	if d.Correspondent != nil {
		for _, n := range f.names {
			if n.ID == *d.Correspondent {
				v.CorrName = n.Name
			}
		}
	}
	if v.Tags == nil {
		v.Tags = []int64{}
	}
	return v
}

func (f *Server) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := r.Header.Get("Authorization")
	f.auth = append(f.auth, h)
	person, ok := f.tokens[strings.TrimPrefix(h, "Bearer ")]
	if !ok || !strings.HasPrefix(h, "Bearer ") {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/v1")
	parts := strings.Split(strings.Trim(p, "/"), "/")
	switch {
	case p == "/documents" && r.Method == http.MethodGet:
		f.list(w, r, person)
	case p == "/documents" && r.Method == http.MethodPost:
		f.upload(w, r, person)
	case p == "/taxonomy" && r.Method == http.MethodGet:
		out := map[string][]Named{"tags": {}, "correspondents": {}, "document_types": {}}
		for _, n := range f.names {
			if n.Person == person {
				out[n.Kind] = append(out[n.Kind], *n)
			}
		}
		writeJSON(w, http.StatusOK, out)
	case len(parts) == 1 && (parts[0] == "tags" || parts[0] == "correspondents" || parts[0] == "document_types"):
		kind := parts[0]
		if r.Method == http.MethodGet {
			out := []Named{}
			for _, n := range f.names {
				if n.Person == person && n.Kind == kind {
					out = append(out, *n)
				}
			}
			writeJSON(w, http.StatusOK, out)
			return
		}
		var in struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		for _, n := range f.names {
			if n.Person == person && n.Kind == kind && strings.EqualFold(n.Name, strings.TrimSpace(in.Name)) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "that name is already in use"})
				return
			}
		}
		writeJSON(w, http.StatusCreated, f.addNamed(person, kind, strings.TrimSpace(in.Name)))
	case len(parts) >= 2 && parts[0] == "documents" && r.Method == http.MethodGet:
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		var d *Doc
		for _, x := range f.docs {
			if x.ID == id && x.Person == person {
				d = x
			}
		}
		if d == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		switch {
		case len(parts) == 2:
			writeJSON(w, http.StatusOK, map[string]any{"document": f.view(d), "prev": 0, "next": 0})
		case parts[2] == "download":
			disp := "attachment"
			if r.URL.Query().Get("inline") == "1" {
				disp = "inline"
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disp, d.OriginalName))
			_, _ = w.Write(d.Data)
		case parts[2] == "thumb":
			w.Header().Set("Content-Type", "image/webp")
			_, _ = io.WriteString(w, "thumb-of-"+strconv.FormatInt(d.ID, 10))
		default:
			http.NotFound(w, r)
		}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no route " + r.Method + " " + p})
	}
}

func (f *Server) list(w http.ResponseWriter, r *http.Request, person string) {
	q := r.URL.Query()
	var out []Doc
	for _, d := range f.docs {
		if d.Person != person {
			continue
		}
		if c := q.Get("correspondent"); c != "" && (d.Correspondent == nil || strconv.FormatInt(*d.Correspondent, 10) != c) {
			continue
		}
		if e := q.Get("external_id"); e != "" && d.ExternalID != e {
			continue
		}
		if s := q.Get("q"); s != "" && !strings.Contains(strings.ToLower(d.Title), strings.ToLower(s)) {
			continue
		}
		out = append(out, f.view(d))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	total := len(out)
	off, _ := strconv.Atoi(q.Get("offset"))
	lim, _ := strconv.Atoi(q.Get("limit"))
	if lim <= 0 {
		lim = 50
	}
	if off > len(out) {
		off = len(out)
	}
	out = out[off:]
	if len(out) > lim {
		out = out[:lim]
	}
	if out == nil {
		out = []Doc{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out, "count": total})
}

func (f *Server) upload(w http.ResponseWriter, r *http.Request, person string) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad multipart body"})
		return
	}
	file, hdr, err := r.FormFile("document")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file in the request"})
		return
	}
	data, _ := io.ReadAll(file)
	sum := sha256.Sum256(data)
	d := &Doc{Person: person, Title: r.FormValue("title"), Created: r.FormValue("created"), OriginalName: hdr.Filename, ExternalID: r.FormValue("external_id"), Checksum: hex.EncodeToString(sum[:]), Data: data, Status: "pending", Tags: []int64{}}
	for _, x := range f.docs {
		if x.Person == person && ((d.ExternalID != "" && x.ExternalID == d.ExternalID) || x.Checksum == d.Checksum) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("this file is already in your documents as #%d “%s”", x.ID, x.Title), "duplicate_of": x.ID})
			return
		}
	}
	own := func(field string) *int64 {
		v := r.FormValue(field)
		if v == "" {
			return nil
		}
		id, _ := strconv.ParseInt(v, 10, 64)
		for _, n := range f.names {
			if n.ID == id && n.Person == person {
				return &id
			}
		}
		return nil // someone else's id: ignored, like a foreign key that does not resolve
	}
	d.Correspondent, d.DocumentType = own("correspondent"), own("document_type")
	for _, t := range strings.Split(r.FormValue("tags"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			d.Tags = append(d.Tags, id)
		}
	}
	if d.Title == "" {
		d.Title = hdr.Filename
	}
	f.nextID++
	d.ID = f.nextID
	f.docs = append(f.docs, d)
	writeJSON(w, http.StatusCreated, f.view(d))
}
