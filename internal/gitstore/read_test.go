package gitstore

import (
	"errors"
	"strings"
	"testing"
)

func TestList(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/prd.md", "# Payment PRD\n\nBody.\n", john, "create: docs/prd.md", "initial PRD draft for payments")
	seedDoc(t, s, "notes/scratch.md", "no heading here\n", john, "create: notes/scratch.md", "scratch notes for planning")
	seedDoc(t, s, "notes/scratch.md", "no heading, edited\n", joe, "update: notes/scratch.md", "joe refines the notes")
	seedDoc(t, s, "archive/old.md", "# Old\n", john, "archive: old.md", "seed an archived doc")

	docs, err := s.List(ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]DocMeta{}
	for _, d := range docs {
		byPath[d.Path] = d
	}

	if _, hasArchived := byPath["archive/old.md"]; hasArchived {
		t.Error("List(\"\") must exclude archive/")
	}
	prd, ok := byPath["docs/prd.md"]
	if !ok {
		t.Fatal("docs/prd.md missing from listing")
	}
	if prd.Title != "Payment PRD" {
		t.Errorf("title = %q, want heading text", prd.Title)
	}
	if prd.LastAuthor != john.Name {
		t.Errorf("prd last author = %q, want %q", prd.LastAuthor, john.Name)
	}
	scratch := byPath["notes/scratch.md"]
	if scratch.Title != "scratch" {
		t.Errorf("headingless title = %q, want filename fallback", scratch.Title)
	}
	if scratch.LastAuthor != joe.Name {
		t.Errorf("scratch last author = %q, want %q (latest committer)", scratch.LastAuthor, joe.Name)
	}
	if scratch.CreatedBy != john.Name {
		t.Errorf("scratch created_by = %q, want %q (original committer)", scratch.CreatedBy, john.Name)
	}
	if scratch.LastModified.IsZero() || scratch.Created.IsZero() {
		t.Error("LastModified/Created not populated")
	}
	if scratch.Created.After(scratch.LastModified) {
		t.Errorf("created %v after last modified %v", scratch.Created, scratch.LastModified)
	}
}

func TestListFolderFilter(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/a.md", "# A\n", john, "create: docs/a.md", "seed doc a for tests")
	seedDoc(t, s, "notes/b.md", "# B\n", john, "create: notes/b.md", "seed doc b for tests")

	docs, err := s.List(ListQuery{Folder: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "docs/a.md" {
		t.Errorf("List(docs) = %+v, want just docs/a.md", docs)
	}
}

func TestListMetadataFilters(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/draft-note.md", "# Draft\n") // auto-injected draft
	if err := s.Create("docs/adr-001.md",
		"---\nstatus: approved\ntags: [Payments]\ntype: adr\n---\n\n# ADR 001\n",
		"seed an approved adr for filter tests", john); err != nil {
		t.Fatal(err)
	}

	byStatus, err := s.List(ListQuery{Status: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	// Seed README is approved too, so look for exactly our adr among them.
	foundADR := false
	for _, d := range byStatus {
		if d.Meta.Status != "approved" {
			t.Errorf("status filter leaked %+v", d)
		}
		if d.Path == "docs/adr-001.md" {
			foundADR = true
		}
	}
	if !foundADR {
		t.Error("approved filter missed docs/adr-001.md")
	}

	byTag, err := s.List(ListQuery{Tag: "payments"}) // case-insensitive
	if err != nil {
		t.Fatal(err)
	}
	if len(byTag) != 1 || byTag[0].Path != "docs/adr-001.md" {
		t.Errorf("tag filter = %+v, want just the adr", byTag)
	}

	byType, err := s.List(ListQuery{Type: "adr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byType) != 1 || byType[0].Path != "docs/adr-001.md" {
		t.Errorf("type filter = %+v, want just the adr", byType)
	}

	drafts, err := s.List(ListQuery{Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Path != "docs/draft-note.md" {
		t.Errorf("draft filter = %+v, want just the injected-draft doc", drafts)
	}
}

func TestListArchiveFolder(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "archive/old.md", "# Old\n", john, "archive: old.md", "seed archived doc")

	docs, err := s.List(ListQuery{Folder: "archive"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "archive/old.md" {
		t.Errorf("List(archive) = %+v, want archive/old.md", docs)
	}
}

func TestListRejectsTraversal(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.List(ListQuery{Folder: "../outside"}); err == nil {
		t.Error("List with traversal folder must fail")
	}
}

func TestRead(t *testing.T) {
	s := newTestStore(t)
	content := "# Read Me\n\nExact content, trailing newline preserved.\n"
	seedDoc(t, s, "docs/read.md", content, john, "create: docs/read.md", "seed for read test")

	doc, err := s.Read("docs/read.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Content != content {
		t.Errorf("content mismatch:\n got %q\nwant %q", doc.Content, content)
	}
	if doc.LastCommit.AuthorName != john.Name || doc.LastCommit.Subject != "create: docs/read.md" {
		t.Errorf("last commit = %+v", doc.LastCommit)
	}
	if doc.LastCommit.ShortHash == "" {
		t.Error("ShortHash empty; agents need it for expected_rev")
	}
}

func TestReadNotFoundSuggests(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/setup.md", "# Setup\n", john, "create: docs/setup.md", "seed for suggestion test")

	_, err := s.Read("docs/setp.md")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
	if len(nf.Suggestions) == 0 || nf.Suggestions[0] != "docs/setup.md" {
		t.Errorf("suggestions = %v, want docs/setup.md first", nf.Suggestions)
	}
	if !strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("error text should carry suggestions: %v", err)
	}
}

func TestReadArchivedDoc(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "archive/gone.md", "# Gone\n", john, "archive: gone.md", "seed archived for read")

	doc, err := s.Read("archive/gone.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Content != "# Gone\n" {
		t.Errorf("content = %q", doc.Content)
	}
}

func TestSearch(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/prd.md", "# PRD\n\nWe chose PostgreSQL for storage.\n", john, "create: docs/prd.md", "seed searchable doc")
	seedDoc(t, s, "archive/hidden.md", "PostgreSQL in the archive\n", john, "archive: hidden.md", "seed archived doc")

	hits, err := s.Search("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want exactly one (case-insensitive, archive excluded)", hits)
	}
	h := hits[0]
	if h.Path != "docs/prd.md" || h.Line != 3 || !strings.Contains(h.Text, "PostgreSQL") {
		t.Errorf("hit = %+v", h)
	}
}

func TestSearchNoMatches(t *testing.T) {
	s := newTestStore(t)
	hits, err := s.Search("nothing-matches-this")
	if err != nil {
		t.Fatalf("no matches must not be an error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %+v, want none", hits)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Search("  "); err == nil {
		t.Error("empty query must fail validation")
	}
}
