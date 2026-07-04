package gitstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreate(t *testing.T) {
	s := newTestStore(t)

	err := s.Create("docs/prd.md", "# PRD\n\nBody.\n", "initial PRD for the payments project", john)
	if err != nil {
		t.Fatal(err)
	}

	if n := commitCount(t, s); n != 2 {
		t.Errorf("commit count = %d, want 2 (bootstrap + create)", n)
	}
	name, email, subject, body := lastCommitMeta(t, s)
	if name != john.Name || email != john.Email {
		t.Errorf("author = %s <%s>, want %s <%s>", name, email, john.Name, john.Email)
	}
	if subject != "create: docs/prd.md" {
		t.Errorf("subject = %q", subject)
	}
	if body != "initial PRD for the payments project" {
		t.Errorf("why not in commit body: %q", body)
	}
	doc, err := s.Read("docs/prd.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Content != "# PRD\n\nBody.\n" {
		t.Errorf("content = %q", doc.Content)
	}
	if out := gitOut(t, s, "status", "--porcelain"); out != "" {
		t.Errorf("worktree dirty after create:\n%s", out)
	}
}

func TestCreateNormalizesTrailingNewline(t *testing.T) {
	s := newTestStore(t)
	if err := s.Create("docs/n.md", "no newline", "content without trailing newline", john); err != nil {
		t.Fatal(err)
	}
	doc, err := s.Read("docs/n.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Content != "no newline\n" {
		t.Errorf("content = %q, want trailing newline added", doc.Content)
	}
}

func TestCreateExisting(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/a.md", "v1\n")

	err := s.Create("docs/a.md", "v2\n", "attempt to create over existing doc", joe)
	var ee *ExistsError
	if !errors.As(err, &ee) {
		t.Fatalf("want ExistsError, got %v", err)
	}
	if n := commitCount(t, s); n != 2 {
		t.Errorf("failed create must not commit; count = %d", n)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)

	tests := []struct {
		name string
		path string
		why  string
	}{
		{name: "traversal path", path: "../evil.md", why: "long enough rationale"},
		{name: "non markdown", path: "docs/x.txt", why: "long enough rationale"},
		{name: "archive write", path: "archive/x.md", why: "long enough rationale"},
		{name: "why too short", path: "docs/x.md", why: "short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Create(tt.path, "content\n", tt.why, john)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want ValidationError, got %v", err)
			}
		})
	}
	if n := commitCount(t, s); n != 1 {
		t.Errorf("rejected writes must not commit; count = %d", n)
	}
}

func TestUpdate(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/a.md", "v1\n")

	if err := s.Update("docs/a.md", "v2\n", "revise after review feedback", "", joe); err != nil {
		t.Fatal(err)
	}

	doc, err := s.Read("docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Content != "v2\n" {
		t.Errorf("content = %q, want full replacement", doc.Content)
	}
	name, _, subject, body := lastCommitMeta(t, s)
	if name != joe.Name || subject != "update: docs/a.md" || body != "revise after review feedback" {
		t.Errorf("commit = %s / %q / %q", name, subject, body)
	}
	if n := commitCount(t, s); n != 3 {
		t.Errorf("commit count = %d, want 3", n)
	}
}

func TestUpdateMissing(t *testing.T) {
	s := newTestStore(t)
	err := s.Update("docs/nope.md", "x\n", "update of a doc that does not exist", "", john)
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
}

func TestUpdateNoChange(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/a.md", "same\n")

	err := s.Update("docs/a.md", "same\n", "no-op update with identical content", "", joe)
	var nc *NoChangeError
	if !errors.As(err, &nc) {
		t.Fatalf("want NoChangeError, got %v", err)
	}
	if n := commitCount(t, s); n != 2 {
		t.Errorf("no-change update must not commit; count = %d", n)
	}
	if out := gitOut(t, s, "status", "--porcelain"); out != "" {
		t.Errorf("worktree dirty after no-change update:\n%s", out)
	}
}

func TestUpdateExpectedRev(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/a.md", "v1\n")
	staleRev := gitOut(t, s, "rev-parse", "--short", "HEAD")

	// Matching rev succeeds.
	if err := s.Update("docs/a.md", "v2\n", "update with correct expected rev", "", joe); err != nil {
		t.Fatal(err)
	}
	currentRev := gitOut(t, s, "rev-parse", "--short", "HEAD")
	if err := s.Update("docs/a.md", "v3\n", "update guarded by current rev", currentRev, john); err != nil {
		t.Fatalf("update with matching expected_rev: %v", err)
	}

	// Stale rev conflicts and changes nothing.
	before := commitCount(t, s)
	err := s.Update("docs/a.md", "v4\n", "update guarded by stale rev", staleRev, joe)
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	if ce.LastAuthor != john.Name || ce.CurrentRev == "" {
		t.Errorf("conflict details = %+v", ce)
	}
	if !strings.Contains(ce.Error(), "expected_rev") {
		t.Errorf("conflict message should tell the agent how to recover: %v", ce)
	}
	if commitCount(t, s) != before {
		t.Error("conflicted update must not commit")
	}
	doc, _ := s.Read("docs/a.md")
	if doc.Content != "v3\n" {
		t.Errorf("conflicted update must not change content, got %q", doc.Content)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/old.md", "# Old\n")

	if err := s.Delete("docs/old.md", "superseded by the new architecture doc", john); err != nil {
		t.Fatal(err)
	}

	_, _, subject, body := lastCommitMeta(t, s)
	if subject != "archive: docs/old.md" || body == "" {
		t.Errorf("commit = %q / %q", subject, body)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "docs/old.md")); !os.IsNotExist(err) {
		t.Error("original path must be gone from the worktree")
	}

	docs, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.Path == "docs/old.md" {
			t.Error("deleted doc still listed")
		}
	}

	// Soft delete: still readable from archive/.
	doc, err := s.Read("archive/docs/old.md")
	if err != nil {
		t.Fatalf("archived doc must stay readable: %v", err)
	}
	if doc.Content != "# Old\n" {
		t.Errorf("archived content = %q", doc.Content)
	}
	if out := gitOut(t, s, "status", "--porcelain"); out != "" {
		t.Errorf("worktree dirty after delete:\n%s", out)
	}
}

func TestDeleteMissing(t *testing.T) {
	s := newTestStore(t)
	err := s.Delete("docs/nope.md", "delete of a doc that does not exist", john)
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
}

func TestDeleteAlreadyArchived(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/a.md", "x\n")
	if err := s.Delete("docs/a.md", "first archive of this doc", john); err != nil {
		t.Fatal(err)
	}
	err := s.Delete("archive/docs/a.md", "attempt to delete an archived doc", john)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestDeleteArchiveCollision(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, "docs/a.md", "first\n")
	if err := s.Delete("docs/a.md", "archive the first incarnation", john); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, "docs/a.md", "second\n")
	if err := s.Delete("docs/a.md", "archive the second incarnation", john); err != nil {
		t.Fatal(err)
	}

	docs, err := s.List("archive")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("want 2 archived docs, got %+v", docs)
	}
}

func mustCreate(t *testing.T, s *GitStore, path, content string) {
	t.Helper()
	if err := s.Create(path, content, "seed document for write-path tests", john); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
}
