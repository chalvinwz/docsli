package gitstore

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var (
	john = Identity{Name: "John (agent)", Email: "john-agent@cohort.local"}
	joe  = Identity{Name: "Joe (agent)", Email: "joe-agent@cohort.local"}
)

// seedDoc commits a document directly via git, bypassing the write path, so
// read-path tests do not depend on Create/Update.
func seedDoc(t *testing.T, s *GitStore, path, content string, author Identity, subject, why string) {
	t.Helper()
	full := filepath.Join(s.Dir(), path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, s, "add", "--", path)
	if err := s.commit(context.Background(), author, subject, why); err != nil {
		t.Fatalf("seed commit %s: %v", path, err)
	}
}

// newTestStore opens a store on a fresh temp repo with host git config
// isolated, so commit signing, hooks, or author settings on the developer's
// machine cannot leak into tests.
func newTestStore(t *testing.T) *GitStore {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	s, err := Open(t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// gitOut runs git in the store's repo and fails the test on error.
func gitOut(t *testing.T, s *GitStore, args ...string) string {
	t.Helper()
	out, err := s.r.run(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func commitCount(t *testing.T, s *GitStore) int {
	t.Helper()
	n, err := strconv.Atoi(gitOut(t, s, "rev-list", "--count", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// lastCommitMeta returns author name, email, subject, body of HEAD.
func lastCommitMeta(t *testing.T, s *GitStore) (name, email, subject, body string) {
	t.Helper()
	out := gitOut(t, s, "log", "-1", "--format=%an%x00%ae%x00%s%x00%b")
	parts := strings.SplitN(out, "\x00", 4)
	if len(parts) != 4 {
		t.Fatalf("unexpected log output: %q", out)
	}
	return parts[0], parts[1], parts[2], strings.TrimSpace(parts[3])
}
