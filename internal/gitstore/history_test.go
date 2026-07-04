package gitstore

import (
	"errors"
	"strings"
	"testing"
)

func seedThreeRevisions(t *testing.T, s *GitStore) {
	t.Helper()
	seedDoc(t, s, "docs/adr.md", "v1\n", alice, "create: docs/adr.md", "first draft of the ADR")
	seedDoc(t, s, "docs/adr.md", "v2\n", bob, "update: docs/adr.md", "second revision with feedback")
	seedDoc(t, s, "docs/adr.md", "v3\n", alice, "update: docs/adr.md", "final wording agreed in review")
}

func TestHistoryNewestFirst(t *testing.T) {
	s := newTestStore(t)
	seedThreeRevisions(t, s)

	commits, err := s.History("docs/adr.md", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("got %d commits, want 3", len(commits))
	}
	if commits[0].Body != "final wording agreed in review" {
		t.Errorf("newest first violated: commits[0] = %+v", commits[0])
	}
	if commits[2].Subject != "create: docs/adr.md" {
		t.Errorf("oldest last violated: commits[2] = %+v", commits[2])
	}
	if commits[0].AuthorName != alice.Name || commits[1].AuthorName != bob.Name {
		t.Errorf("authors wrong: %s, %s", commits[0].AuthorName, commits[1].AuthorName)
	}
	for i, c := range commits {
		if c.Hash == "" || c.ShortHash == "" || c.Date.IsZero() {
			t.Errorf("commit %d missing fields: %+v", i, c)
		}
	}
	if !commits[0].Date.After(commits[2].Date) && !commits[0].Date.Equal(commits[2].Date) {
		t.Errorf("dates not descending: %v .. %v", commits[0].Date, commits[2].Date)
	}
}

func TestHistoryTruncatesToN(t *testing.T) {
	s := newTestStore(t)
	seedThreeRevisions(t, s)

	commits, err := s.History("docs/adr.md", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if commits[0].Body != "final wording agreed in review" {
		t.Error("truncation must keep newest commits")
	}
}

func TestHistoryDefaultN(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/one.md", "x\n", alice, "create: docs/one.md", "single revision doc")

	commits, err := s.History("docs/one.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
}

func TestHistoryNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.History("docs/never-existed.md", 10)
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
}

func TestDiff(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/d.md", "old line\n", alice, "create: docs/d.md", "first version for diff test")
	revA := gitOut(t, s, "rev-parse", "HEAD")
	seedDoc(t, s, "docs/d.md", "new line\n", bob, "update: docs/d.md", "second version for diff test")

	out, err := s.Diff("docs/d.md", revA, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-old line") || !strings.Contains(out, "+new line") {
		t.Errorf("diff missing expected hunks:\n%s", out)
	}

	same, err := s.Diff("docs/d.md", "HEAD", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if same != "" {
		t.Errorf("identical revs must produce empty diff, got:\n%s", same)
	}
}

func TestDiffRejectsBadRevs(t *testing.T) {
	s := newTestStore(t)
	seedDoc(t, s, "docs/d.md", "x\n", alice, "create: docs/d.md", "doc for bad rev test")

	for _, rev := range []string{"", "-v", "--output=/tmp/x", "deadbeef000000"} {
		_, err := s.Diff("docs/d.md", rev, "")
		var re *RevisionError
		if !errors.As(err, &re) {
			t.Errorf("rev %q: want RevisionError, got %v", rev, err)
		}
	}
}
