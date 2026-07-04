package gitstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var _ Store = (*GitStore)(nil)

// Create adds a new document and commits it as author. It fails with
// ExistsError when the path is already taken.
func (s *GitStore) Create(path, content, why string, author Identity) error {
	if err := validatePath(path, false); err != nil {
		return err
	}
	if err := validateWhy(why); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	exists, err := s.pathInHead(ctx, path)
	if err != nil {
		return err
	}
	if exists {
		return &ExistsError{Path: path}
	}

	if err := s.writeWorktree(path, content); err != nil {
		return err
	}
	if err := s.stageAndCommit(ctx, path, "create: "+path, why, author); err != nil {
		s.rollback(ctx, path)
		return err
	}
	return nil
}

// Update replaces the full content of an existing document. A non-empty
// expectedRev must match the doc's current last commit (optimistic locking);
// identical content is rejected so every commit represents a real change.
func (s *GitStore) Update(path, content, why, expectedRev string, author Identity) error {
	if err := validatePath(path, false); err != nil {
		return err
	}
	if err := validateWhy(why); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	exists, err := s.pathInHead(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return s.notFound(ctx, path)
	}

	if expectedRev != "" {
		if err := s.checkExpectedRev(ctx, path, expectedRev); err != nil {
			return err
		}
	}

	if err := s.writeWorktree(path, content); err != nil {
		return err
	}
	if _, err := s.r.run(ctx, "add", "--", path); err != nil {
		s.rollback(ctx, path)
		return err
	}
	// Nothing staged means the new content is byte-identical to HEAD.
	if _, code, err := s.r.runExit(ctx, "diff", "--cached", "--quiet"); err != nil {
		s.rollback(ctx, path)
		return err
	} else if code == 0 {
		return &NoChangeError{Path: path}
	}
	if err := s.commit(ctx, author, "update: "+path, why); err != nil {
		s.rollback(ctx, path)
		return err
	}
	return nil
}

func (s *GitStore) checkExpectedRev(ctx context.Context, path, expectedRev string) error {
	if !revPattern.MatchString(expectedRev) {
		return &RevisionError{Rev: expectedRev}
	}
	commits, err := s.historyLocked(ctx, path, 1)
	if err != nil {
		return err
	}
	last := commits[0]
	if strings.HasPrefix(last.Hash, expectedRev) {
		return nil
	}
	why := last.Body
	if why == "" {
		why = last.Subject
	}
	return &ConflictError{
		Path:       path,
		CurrentRev: last.ShortHash,
		LastAuthor: last.AuthorName,
		LastWhy:    why,
	}
}

// Delete soft-deletes: git mv into archive/ plus one commit. Nothing is ever
// removed from history; the doc stays readable at its archive/ path.
func (s *GitStore) Delete(path, why string, author Identity) error {
	// allowArchive=false also rejects deleting already-archived docs.
	if err := validatePath(path, false); err != nil {
		return err
	}
	if err := validateWhy(why); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	exists, err := s.pathInHead(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return s.notFound(ctx, path)
	}

	target, err := s.archiveTarget(ctx, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(s.dir, target)), 0o755); err != nil {
		return err
	}
	// git mv stages the rename itself.
	if _, err := s.r.run(ctx, "mv", "--", path, target); err != nil {
		s.rollback(ctx, target)
		return err
	}
	if err := s.commit(ctx, author, "archive: "+path, why); err != nil {
		s.rollback(ctx, target)
		return err
	}
	return nil
}

// archiveTarget picks archive/<path>, adding a unix-timestamp suffix when a
// previous archive of the same path already occupies it.
func (s *GitStore) archiveTarget(ctx context.Context, path string) (string, error) {
	target := archiveDir + "/" + path
	occupied, err := s.pathInHead(ctx, target)
	if err != nil {
		return "", err
	}
	if !occupied {
		return target, nil
	}
	suffixed := fmt.Sprintf("%s/%s-%s.md",
		archiveDir,
		strings.TrimSuffix(path, ".md"),
		strconv.FormatInt(time.Now().Unix(), 10),
	)
	return suffixed, nil
}

// pathInHead reports whether path exists in the HEAD tree.
func (s *GitStore) pathInHead(ctx context.Context, path string) (bool, error) {
	_, code, err := s.r.runExit(ctx, "cat-file", "-e", "HEAD:"+path)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// writeWorktree writes content to the worktree file, normalizing a missing
// trailing newline so the repo stays friendly to plain git tooling.
func (s *GitStore) writeWorktree(path, content string) error {
	full := filepath.Join(s.dir, path)
	rel, err := filepath.Rel(s.dir, full)
	if err != nil || rel != path {
		return &ValidationError{Field: "path", Reason: "path escapes the repository"}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func (s *GitStore) stageAndCommit(ctx context.Context, path, subject, why string, author Identity) error {
	if _, err := s.r.run(ctx, "add", "--", path); err != nil {
		return err
	}
	return s.commit(ctx, author, subject, why)
}

// rollback restores worktree and index to HEAD after a failed write, so a
// broken operation can never leave the repo dirty for the next caller.
func (s *GitStore) rollback(ctx context.Context, path string) {
	if _, err := s.r.run(ctx, "reset", "--hard", "HEAD"); err != nil {
		s.log.Warn("rollback reset failed", "path", path, "err", err.Error())
	}
	if _, err := s.r.run(ctx, "clean", "-fd", "--", path); err != nil {
		s.log.Warn("rollback clean failed", "path", path, "err", err.Error())
	}
}
