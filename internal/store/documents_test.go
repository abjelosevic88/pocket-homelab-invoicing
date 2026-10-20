package store

import (
	"context"
	"testing"
)

func TestDocumentsAndPocketDocumentsLinks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := testUser(t, s, "owner@example.com")
	c := &Client{Name: "Athena Studio", Currency: "EUR"}
	if err := s.CreateClient(ctx, uid, c); err != nil {
		t.Fatal(err)
	}
	d := &Document{Title: "Ugovor", Category: "Contracts", ClientID: &c.ID, DocDate: "2026-01-15", ExpiresAt: "2026-12-31", Filename: "ugovor.pdf", StoredName: "abc.pdf", ContentType: "application/pdf", Size: 10}
	if err := s.CreateDocument(ctx, uid, d); err != nil {
		t.Fatal(err)
	}
	d2 := &Document{Title: "Bank letter", Category: "Bank", Filename: "b.pdf", StoredName: "b.pdf", Size: 1}
	if err := s.CreateDocument(ctx, uid, d2); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(ctx, uid, d.ID)
	if err != nil || got.ClientName != "Athena Studio" || got.Category != "Contracts" {
		t.Fatalf("get: %v %+v", err, got)
	}
	list, _ := s.ListDocuments(ctx, uid, DocumentFilter{Query: "athena"})
	if len(list) != 1 || list[0].ID != d.ID {
		t.Fatalf("search by client name: %+v", list)
	}
	list, _ = s.ListDocuments(ctx, uid, DocumentFilter{Category: "Bank"})
	if len(list) != 1 || list[0].ID != d2.ID {
		t.Fatalf("filter by category: %+v", list)
	}
	list, _ = s.ListDocuments(ctx, uid, DocumentFilter{ClientID: c.ID})
	if len(list) != 1 {
		t.Fatalf("filter by client: %+v", list)
	}
	cats, _ := s.DocumentCategories(ctx, uid)
	if cats["Contracts"] != 1 || cats["Bank"] != 1 {
		t.Fatalf("categories: %v", cats)
	}
	exp, _ := s.ExpiringDocuments(ctx, uid, "2026-12-31")
	if len(exp) != 1 {
		t.Fatalf("expiring: %+v", exp)
	}
	d.Title, d.ClientID = "Ugovor 2026", nil
	if err := s.UpdateDocument(ctx, uid, d); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocument(ctx, uid, d.ID); got.Title != "Ugovor 2026" || got.ClientID != nil {
		t.Fatalf("update: %+v", got)
	}

	// Pocket Documents links: failed → filed, lookup by documents id, cascade on document delete.
	l := &DocsLink{Kind: "document", RefID: d.ID, Title: "Ugovor", Error: "unreachable"}
	if err := s.SaveDocsLink(ctx, uid, l); err != nil {
		t.Fatal(err)
	}
	l.DocumentsID, l.Error = 42, ""
	if err := s.SaveDocsLink(ctx, uid, l); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocsLink(ctx, uid, "document", d.ID); got == nil || got.DocumentsID != 42 || got.Error != "" {
		t.Fatalf("link: %+v", got)
	}
	if by, _ := s.DocsLinkByDocumentsID(ctx, uid, 42); by == nil || by.RefID != d.ID {
		t.Fatalf("by documents id: %+v", by)
	}
	m, _ := s.DocsLinksFor(ctx, uid, "document", []int64{d.ID, d2.ID})
	if len(m) != 1 {
		t.Fatalf("links for: %+v", m)
	}
	if err := s.SaveDocsLink(ctx, uid, &DocsLink{Kind: "quote", RefID: d.ID}); err == nil {
		t.Fatal("unknown kind accepted")
	}

	// Old Paperless links are offered for adoption until the record has a Pocket Documents link.
	if _, err := s.DB.Exec(`INSERT INTO paperless_links (user_id, kind, ref_id, paperless_id) VALUES (?, 'document', ?, 7), (?, 'document', ?, 8)`, uid, d.ID, uid, d2.ID); err != nil {
		t.Fatal(err)
	}
	if adopt, _ := s.PaperlessLinksToAdopt(ctx, uid); len(adopt) != 1 || adopt[0].RefID != d2.ID || adopt[0].PaperlessID != 8 {
		t.Fatalf("to adopt: %+v", adopt)
	}
	if adopt, _ := s.PaperlessLinksToAdopt(ctx, uid+1); len(adopt) != 0 {
		t.Fatalf("someone else's to adopt: %+v", adopt)
	}
	if err := s.DeleteDocsLink(ctx, uid, "document", d2.ID); err != nil {
		t.Fatal(err)
	}
	if adopt, _ := s.PaperlessLinksToAdopt(ctx, uid); len(adopt) != 0 {
		t.Fatalf("unlinked record offered again: %+v", adopt)
	}

	if err := s.DeleteDocument(ctx, uid, d.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetDocsLink(ctx, uid, "document", d.ID); got != nil {
		t.Fatalf("link should be gone: %+v", got)
	}
	if none, _ := s.GetDocsLink(ctx, uid, "invoice", 999); none != nil {
		t.Fatal("expected nil for unknown link")
	}
}
