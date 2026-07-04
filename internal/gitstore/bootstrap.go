package gitstore

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	systemName  = "docsli"
	systemEmail = "docsli@system.local"
	opTimeout   = 30 * time.Second
)

const seedReadme = `---
status: approved
type: note
---

# Docs

This repository is managed by [docsli](https://github.com/chalvinwz/docsli).
Every change is an ordinary git commit: browse history with ` + "`git log`" + `,
compare versions with ` + "`git diff`" + `. Deleted docs live under archive/.
`

// GitStore implements Store on a git repository via the git CLI.
//
// Concurrency: writes take the write lock for their whole check→write→commit
// sequence, so concurrent MCP calls never race on the git index. Reads take
// the read lock because List reads worktree files and must not observe a
// half-written one.
type GitStore struct {
	dir string
	r   runner
	mu  sync.RWMutex
	log *slog.Logger

	// onWrite, when set, is called after every successful commit. The mirror
	// pusher hooks in here; it must not block.
	onWrite func()
}

// Open returns a store for dir, bootstrapping a fresh git repo (seed README,
// archive/.gitkeep, initial commit as the system identity) when dir is not a
// repo yet. A pre-existing repo with uncommitted changes is refused: reads
// rely on the worktree matching HEAD.
func Open(dir string, logger *slog.Logger) (*GitStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create repo dir: %w", err)
	}

	s := &GitStore{dir: abs, r: runner{dir: abs}, log: logger}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if _, err := s.r.run(ctx, "rev-parse", "--git-dir"); err != nil {
		logger.Info("initializing new docs repository", "dir", abs)
		if _, err := s.r.run(ctx, "init", "--initial-branch=main"); err != nil {
			return nil, err
		}
	}

	// Repo-local identity so plain `git commit` inside the data dir works
	// even in containers; per-write authorship is set per commit anyway.
	for key, val := range map[string]string{"user.name": systemName, "user.email": systemEmail} {
		if out, _ := s.r.run(ctx, "config", "--get", key); out == "" {
			if _, err := s.r.run(ctx, "config", key, val); err != nil {
				return nil, err
			}
		}
	}

	if code, err := s.headExists(ctx); err != nil {
		return nil, err
	} else if code != 0 {
		if err := s.seed(ctx); err != nil {
			return nil, fmt.Errorf("bootstrap initial commit: %w", err)
		}
	} else {
		out, err := s.r.run(ctx, "status", "--porcelain")
		if err != nil {
			return nil, err
		}
		if out != "" {
			return nil, fmt.Errorf("repo at %s has uncommitted changes; commit or clean them with plain git before starting docsli:\n%s", abs, out)
		}
	}
	return s, nil
}

// headExists reports via exit code whether the repo has any commit (0 = yes).
func (s *GitStore) headExists(ctx context.Context) (int, error) {
	_, code, err := s.r.runExit(ctx, "rev-parse", "--verify", "-q", "HEAD")
	return code, err
}

func (s *GitStore) seed(ctx context.Context) error {
	readme := filepath.Join(s.dir, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		if err := os.WriteFile(readme, []byte(seedReadme), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(s.dir, archiveDir), 0o755); err != nil {
		return err
	}
	keep := filepath.Join(s.dir, archiveDir, ".gitkeep")
	if err := os.WriteFile(keep, nil, 0o644); err != nil {
		return err
	}
	if _, err := s.r.run(ctx, "add", "-A"); err != nil {
		return err
	}
	return s.commit(ctx, Identity{Name: systemName, Email: systemEmail}, "init: bootstrap docs repository", "")
}

// commit records staged changes as one commit authored by author. Subject and
// body map to the two -m flags; committer identity is overridden with -c so
// host-level git config never leaks into the audit trail.
func (s *GitStore) commit(ctx context.Context, author Identity, subject, body string) error {
	args := []string{
		"-c", "user.name=" + author.Name,
		"-c", "user.email=" + author.Email,
		"-c", "commit.gpgsign=false",
		"commit",
		"--author=" + author.Name + " <" + author.Email + ">",
		"-m", subject,
	}
	if body != "" {
		args = append(args, "-m", body)
	}
	if _, err := s.r.run(ctx, args...); err != nil {
		return err
	}
	if s.onWrite != nil {
		s.onWrite()
	}
	return nil
}

// SetOnWrite registers a post-commit hook (used by the mirror pusher). Call
// before serving traffic; not safe to change concurrently with writes.
func (s *GitStore) SetOnWrite(fn func()) { s.onWrite = fn }

// Dir returns the absolute path of the underlying repository.
func (s *GitStore) Dir() string { return s.dir }

func (s *GitStore) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}
