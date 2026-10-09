package store

import (
	"context"
	"testing"
)

func TestDocumentsAndPaperlessLinks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := &Client{Name: "Athena Studio", Currency: "EUR"}
	if err := s.CreateClient(ctx, c); err != nil {
		t.Fatal(err)
	}
	d := &Document{Title: "Ugovor", Category: "Contracts", ClientID: &c.ID, DocDate: "2026-01-15", ExpiresAt: "2026-12-31", Filename: "ugovor.pdf", StoredName: "abc.pdf", ContentType: "application/pdf", Size: 10}
	if err := s.CreateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	d2 := &Document{Title: "Bank letter", Category: "Bank", Filename: "b.pdf", StoredName: "b.pdf", Size: 1}
	if err := s.CreateDocument(ctx, d2); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(ctx, d.ID)
	if err != nil || got.ClientName != "Athena Studio" || got.Category != "Contracts" {
		t.Fatalf("get: %v %+v", err, got)
	}
	list, _ := s.ListDocuments(ctx, DocumentFilter{Query: "athena"})
	if len(list) != 1 || list[0].ID != d.ID {
		t.Fatalf("search by client name: %+v", list)
	}
	list, _ = s.ListDocuments(ctx, DocumentFilter{Category: "Bank"})
	if len(list) != 1 || list[0].ID != d2.ID {
		t.Fatalf("filter by category: %+v", list)
	}
	list, _ = s.ListDocuments(ctx, DocumentFilter{ClientID: c.ID})
	if len(list) != 1 {
		t.Fatalf("filter by client: %+v", list)
	}
	cats, _ := s.DocumentCategories(ctx)
	if cats["Contracts"] != 1 || cats["Bank"] != 1 {
		t.Fatalf("categories: %v", cats)
	}
	exp, _ := s.ExpiringDocuments(ctx, "2026-12-31")
	if len(exp) != 1 {
		t.Fatalf("expiring: %+v", exp)
	}
	d.Title, d.ClientID = "Ugovor 2026", nil
	if err := s.UpdateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocument(ctx, d.ID); got.Title != "Ugovor 2026" || got.ClientID != nil {
		t.Fatalf("update: %+v", got)
	}

	// Paperless links: pending → resolved, lookup by paperless id, cascade on document delete.
	l := &PaperlessLink{Kind: "document", RefID: d.ID, TaskID: "task-1", Title: "Ugovor"}
	if err := s.SavePaperlessLink(ctx, l); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.PendingPaperlessLinks(ctx)
	if len(pending) != 1 {
		t.Fatalf("pending: %+v", pending)
	}
	l.PaperlessID, l.TaskID = 42, ""
	if err := s.SavePaperlessLink(ctx, l); err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.PendingPaperlessLinks(ctx); len(pending) != 0 {
		t.Fatalf("still pending: %+v", pending)
	}
	if got, _ := s.GetPaperlessLink(ctx, "document", d.ID); got == nil || got.PaperlessID != 42 {
		t.Fatalf("link: %+v", got)
	}
	if by, _ := s.PaperlessLinkByPaperlessID(ctx, 42); by == nil || by.RefID != d.ID {
		t.Fatalf("by paperless id: %+v", by)
	}
	m, _ := s.PaperlessLinksFor(ctx, "document", []int64{d.ID, d2.ID})
	if len(m) != 1 {
		t.Fatalf("links for: %+v", m)
	}
	if err := s.DeleteDocument(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPaperlessLink(ctx, "document", d.ID); got != nil {
		t.Fatalf("link should be gone: %+v", got)
	}
	if none, _ := s.GetPaperlessLink(ctx, "invoice", 999); none != nil {
		t.Fatal("expected nil for unknown link")
	}
}
