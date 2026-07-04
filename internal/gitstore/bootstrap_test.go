package gitstore

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenBootstrapsFreshRepo(t *testing.T) {
	s := newTestStore(t)

	for _, f := range []string{"README.md", "archive/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(s.Dir(), f)); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	if n := commitCount(t, s); n != 1 {
		t.Errorf("want exactly 1 bootstrap commit, got %d", n)
	}
	name, email, subject, _ := lastCommitMeta(t, s)
	if name != systemName || email != systemEmail {
		t.Errorf("bootstrap commit author = %s <%s>, want %s <%s>", name, email, systemName, systemEmail)
	}
	if !strings.HasPrefix(subject, "init:") {
		t.Errorf("bootstrap subject = %q, want init: prefix", subject)
	}
	if out := gitOut(t, s, "status", "--porcelain"); out != "" {
		t.Errorf("worktree dirty after bootstrap:\n%s", out)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	reopened, err := Open(s.Dir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if n := commitCount(t, reopened); n != 1 {
		t.Errorf("reopen added commits: got %d, want 1", n)
	}
}

func TestOpenRefusesDirtyRepo(t *testing.T) {
	s := newTestStore(t)
	if err := os.WriteFile(filepath.Join(s.Dir(), "stray.md"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(s.Dir(), slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("want uncommitted-changes error, got: %v", err)
	}
}
