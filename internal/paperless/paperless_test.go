package paperless

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer imitates the few Paperless-ngx endpoints the client uses.
func fakeServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var uploads []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token secret" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("X-Version", "2.14.0")
		if r.URL.Query().Get("query") == "nothing" {
			io.WriteString(w, `{"count":0,"results":[]}`)
			return
		}
		io.WriteString(w, `{"count":1,"results":[{"id":7,"title":"Contract","created":"2026-01-02T00:00:00Z","created_date":"2026-01-02","correspondent":1,"document_type":2,"tags":[3],"original_file_name":"c.pdf"}]}`)
	})
	mux.HandleFunc("/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		uploads = append(uploads, r.FormValue("title")+"|"+r.FormValue("correspondent")+"|"+r.FormValue("document_type")+"|"+strings.Join(r.Form["tags"], ","))
		io.WriteString(w, `"abc-task"`)
	})
	mux.HandleFunc("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"task_id":"abc-task","status":"SUCCESS","result":"Success. New document id 42 created","related_document":"42"}]`)
	})
	for kind, items := range map[string]string{
		"correspondents": `[{"id":1,"name":"Athena Studio"}]`,
		"document_types": `[{"id":2,"name":"Invoice"}]`,
		"tags":           `[{"id":3,"name":"pocket-invoicing"}]`,
	} {
		kind, items := kind, items
		mux.HandleFunc("/api/"+kind+"/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				var in map[string]any
				_ = json.NewDecoder(r.Body).Decode(&in)
				io.WriteString(w, `{"id":99,"name":"`+in["name"].(string)+`"}`)
				return
			}
			io.WriteString(w, `{"next":null,"results":`+items+`}`)
		})
	}
	return httptest.NewServer(mux), &uploads
}

func TestClient(t *testing.T) {
	srv, uploads := fakeServer(t)
	defer srv.Close()
	ctx := context.Background()

	c := New(srv.URL+"/", "secret")
	st := c.Ping(ctx)
	if !st.OK || st.Version != "2.14.0" || st.DocumentCount != 1 {
		t.Fatalf("ping: %+v", st)
	}
	if st := New(srv.URL, "wrong").Ping(ctx); st.OK || !strings.Contains(st.Error, "authentication") {
		t.Fatalf("bad token should fail: %+v", st)
	}

	page, err := c.Search(ctx, "", 1, 25, nil)
	if err != nil || page.Count != 1 || len(page.Results) != 1 {
		t.Fatalf("search: %v %+v", err, page)
	}
	d := page.Results[0]
	if d.Correspondent != "Athena Studio" || d.DocumentType != "Invoice" || len(d.Tags) != 1 || d.Tags[0] != "pocket-invoicing" || d.Created != "2026-01-02" {
		t.Fatalf("names not resolved: %+v", d)
	}
	if !strings.HasSuffix(d.URL, "/documents/7/details") {
		t.Fatalf("url: %s", d.URL)
	}

	task, err := c.Upload(ctx, "inv.pdf", []byte("%PDF"), UploadOptions{Title: "INV-1", Correspondent: "Athena Studio", DocumentType: "Contract", Tags: []string{"pocket-invoicing", "new-tag"}})
	if err != nil || task != "abc-task" {
		t.Fatalf("upload: %v %q", err, task)
	}
	if len(*uploads) != 1 || (*uploads)[0] != "INV-1|1|99|3,99" {
		t.Fatalf("upload fields: %v", *uploads)
	}
	tk, err := c.Task(ctx, task)
	if err != nil || tk.Status != "SUCCESS" || tk.RelatedDocument != 42 {
		t.Fatalf("task: %v %+v", err, tk)
	}
}

func TestUnconfigured(t *testing.T) {
	c := New("", "")
	if c.Configured() {
		t.Fatal("should not be configured")
	}
	if _, err := c.Search(context.Background(), "", 1, 10, nil); err == nil {
		t.Fatal("expected error")
	}
}
