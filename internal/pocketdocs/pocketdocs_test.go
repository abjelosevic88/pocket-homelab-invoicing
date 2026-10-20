package pocketdocs_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pocketdocs"
	"github.com/abjelosevic88/pocket-homelab-invoicing/internal/pocketdocs/pdtest"
)

func TestUploadListAndNames(t *testing.T) {
	f := pdtest.New(t)
	f.Token("pd_alice", "alice")
	f.Token("pd_bob", "bob")
	athena := f.AddNamed("alice", pocketdocs.Correspondents, "Athena Studio")
	f.AddNamed("bob", pocketdocs.Correspondents, "Bob's Client")
	ctx := context.Background()
	c := pocketdocs.New(f.URL+"/", "pd_alice", nil)

	if ps := c.Ping(ctx); !ps.OK || ps.DocumentCount != 0 {
		t.Fatalf("ping: %+v", ps)
	}
	if got := f.Calls(); got[0] != "Bearer pd_alice" {
		t.Fatalf("auth header %q", got[0])
	}

	// upload: names resolved (existing correspondent, new type and tags), 201 with the document
	d, err := c.Upload(ctx, "INV-1.pdf", []byte("%PDF-1"), pocketdocs.UploadOptions{Title: "Invoice INV-1 – Athena", Created: "2026-10-01",
		Correspondent: "athena studio", DocumentType: "Invoice", Tags: []string{"pocket-invoicing", " ", "2026"}, ExternalID: "invoicing:invoice:1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == 0 || d.CorrespondentID != athena || d.Correspondent != "Athena Studio" || len(d.Tags) != 2 {
		t.Fatalf("uploaded: %+v", d)
	}
	stored := f.Docs("alice")[0]
	if stored.ExternalID != "invoicing:invoice:1" || stored.Created != "2026-10-01" || f.NameOf("alice", *stored.DocumentType) != "Invoice" {
		t.Fatalf("stored: %+v", stored)
	}

	// the same external id again: 409 with the existing document
	_, err = c.Upload(ctx, "INV-1 again.pdf", []byte("%PDF-2"), pocketdocs.UploadOptions{ExternalID: "invoicing:invoice:1"})
	var dup *pocketdocs.DuplicateError
	if !errors.As(err, &dup) || dup.ID != d.ID {
		t.Fatalf("duplicate external id: %v", err)
	}
	// the same file again: 409 too
	if _, err = c.Upload(ctx, "copy.pdf", []byte("%PDF-1"), pocketdocs.UploadOptions{}); !errors.As(err, &dup) || dup.ID != d.ID {
		t.Fatalf("duplicate file: %v", err)
	}

	// list by correspondent, by external id, and the rest
	other, _ := c.Upload(ctx, "other.pdf", []byte("other"), pocketdocs.UploadOptions{Title: "Bank letter", Created: "2026-09-01"})
	p, err := c.List(ctx, pocketdocs.Filter{CorrespondentID: athena}, 1, 10)
	if err != nil || p.Count != 1 || p.Results[0].ID != d.ID {
		t.Fatalf("by correspondent: %v %+v", err, p)
	}
	p, _ = c.List(ctx, pocketdocs.Filter{}, 1, 1)
	if p.Count != 2 || p.Pages != 2 || len(p.Results) != 1 || p.Results[0].ID != d.ID {
		t.Fatalf("newest first, paged: %+v", p)
	}
	if got, err := c.FindByExternalID(ctx, "invoicing:invoice:1"); err != nil || got == nil || got.ID != d.ID {
		t.Fatalf("by external id: %v %+v", err, got)
	}
	if got, _ := c.FindByExternalID(ctx, "invoicing:invoice:2"); got != nil {
		t.Fatalf("unknown external id found %+v", got)
	}
	if got, err := c.Get(ctx, other.ID); err != nil || got.Title != "Bank letter" {
		t.Fatalf("get: %v %+v", err, got)
	}
	resp, err := c.Download(ctx, d.ID, false, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "%PDF-1" || resp.Header.Get("Content-Disposition") != `inline; filename="INV-1.pdf"` {
		t.Fatalf("download: %q %q", b, resp.Header.Get("Content-Disposition"))
	}

	// ensure: existing, new, and one created elsewhere after the names were cached (409 → look again)
	if id, err := c.Ensure(ctx, pocketdocs.Correspondents, "ATHENA STUDIO"); err != nil || id != athena {
		t.Fatalf("ensure existing: %d %v", id, err)
	}
	acme, err := c.Ensure(ctx, pocketdocs.Correspondents, "Acme")
	if err != nil || acme == 0 || f.NameOf("alice", acme) != "Acme" {
		t.Fatalf("ensure new: %d %v", acme, err)
	}
	late := f.AddNamed("alice", pocketdocs.Correspondents, "Late Ltd")
	if id, err := c.Ensure(ctx, pocketdocs.Correspondents, "late ltd"); err != nil || id != late {
		t.Fatalf("ensure after a conflict: %d %v", id, err)
	}
	if e := c.Entries(ctx, pocketdocs.Correspondents); len(e) != 3 || e[0].Name != "Acme" {
		t.Fatalf("entries: %+v", e)
	}

	// another person's token sees none of it
	bob := pocketdocs.New(f.URL, "pd_bob", nil)
	if p, _ := bob.List(ctx, pocketdocs.Filter{}, 1, 10); p.Count != 0 {
		t.Fatalf("bob sees %d documents", p.Count)
	}
	if _, err := bob.Get(ctx, d.ID); err == nil {
		t.Fatal("bob read alice's document")
	}
	if id := bob.Lookup(ctx, pocketdocs.Correspondents, "Athena Studio"); id != 0 {
		t.Fatalf("bob sees alice's correspondent %d", id)
	}

	// a bad token
	if ps := pocketdocs.New(f.URL, "pd_wrong", nil).Ping(ctx); ps.OK || ps.Error == "" {
		t.Fatalf("bad token: %+v", ps)
	}
	if (&pocketdocs.Client{}).Configured() {
		t.Fatal("empty client configured")
	}
}
