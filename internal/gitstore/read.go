package gitstore

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// historyFormat emits one record per commit: NUL-separated fields, terminated
// by \x1e. The record separator is load-bearing: %b bodies are multiline.
const historyFormat = "--format=%H%x00%h%x00%an%x00%ae%x00%aI%x00%s%x00%b%x1e"

// List returns metadata for every document under folder ("" = whole repo).
// archive/ is excluded unless folder points into it.
func (s *GitStore) List(folder string) ([]DocMeta, error) {
	folder = strings.Trim(folder, "/")
	if folder != "" {
		if err := validateFolder(folder); err != nil {
			return nil, err
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	args := []string{"ls-files", "-z"}
	if folder != "" {
		args = append(args, "--", folder+"/")
	}
	out, err := s.r.runRaw(ctx, args...)
	if err != nil {
		return nil, err
	}

	includeArchive := folder == archiveDir || strings.HasPrefix(folder, archiveDir+"/")
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || !strings.HasSuffix(p, ".md") {
			continue
		}
		if !includeArchive && strings.HasPrefix(p, archiveDir+"/") {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	changes, err := s.lastChanges(ctx, paths)
	if err != nil {
		return nil, err
	}

	docs := make([]DocMeta, 0, len(paths))
	for _, p := range paths {
		ch := changes[p]
		docs = append(docs, DocMeta{
			Path:         p,
			Title:        s.titleOf(p),
			LastModified: ch.date,
			LastAuthor:   ch.author,
		})
	}
	return docs, nil
}

// validateFolder checks a listing folder: same hygiene as document paths but
// without the .md and archive rules.
func validateFolder(folder string) error {
	switch {
	case strings.ContainsRune(folder, '\\'):
		return &ValidationError{Field: "folder", Reason: "use forward slashes, not backslashes"}
	case hasControlChars(folder):
		return &ValidationError{Field: "folder", Reason: "folder contains control characters"}
	case filepath.IsAbs(folder):
		return &ValidationError{Field: "folder", Reason: "absolute paths are not allowed; use a repo-relative folder like docs"}
	case !filepath.IsLocal(folder):
		return &ValidationError{Field: "folder", Reason: "folder escapes the repository; .. traversal is not allowed"}
	case filepath.Clean(folder) != folder:
		return &ValidationError{Field: "folder", Reason: "use a normalized folder like docs, without ./ or duplicate slashes"}
	}
	for _, seg := range strings.Split(folder, "/") {
		if strings.HasPrefix(strings.ToLower(seg), ".git") {
			return &ValidationError{Field: "folder", Reason: "folders touching .git are not allowed"}
		}
	}
	return nil
}

type changeInfo struct {
	author string
	date   time.Time
}

// lastChanges resolves last author and date for all paths in ONE git log
// walk instead of one subprocess per file. The first (newest) commit block
// naming a path wins; the walk stops early once every path is annotated.
func (s *GitStore) lastChanges(ctx context.Context, paths []string) (map[string]changeInfo, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	out, err := s.r.runRaw(ctx, "-c", "core.quotePath=false", "log", "--format=%x01%an%x00%aI", "--name-only")
	if err != nil {
		return nil, err
	}

	info := make(map[string]changeInfo, len(paths))
	remaining := len(paths)
	for _, block := range strings.Split(out, "\x01") {
		if remaining == 0 {
			break
		}
		lines := strings.Split(block, "\n")
		header := strings.SplitN(lines[0], "\x00", 2)
		if len(header) != 2 {
			continue
		}
		date, err := time.Parse(time.RFC3339, header[1])
		if err != nil {
			continue
		}
		for _, f := range lines[1:] {
			if f == "" || !want[f] {
				continue
			}
			if _, done := info[f]; done {
				continue
			}
			info[f] = changeInfo{author: header[0], date: date}
			remaining--
		}
	}
	return info, nil
}

// titleOf returns the first "# " heading of the worktree file, or the
// filename without extension. Only the first 4KB are examined.
func (s *GitStore) titleOf(path string) string {
	fallback := strings.TrimSuffix(filepath.Base(path), ".md")
	f, err := os.Open(filepath.Join(s.dir, path))
	if err != nil {
		return fallback
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(io.LimitReader(f, 4096))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if title, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(title)
		}
	}
	return fallback
}

// Read returns the committed content of path plus its last commit. Archived
// docs are readable via their archive/ path.
func (s *GitStore) Read(path string) (Doc, error) {
	if err := validatePath(path, true); err != nil {
		return Doc{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	content, err := s.r.runRaw(ctx, "show", "HEAD:"+path)
	if err != nil {
		if isMissingPathErr(err) {
			return Doc{}, s.notFound(ctx, path)
		}
		return Doc{}, err
	}
	commits, err := s.historyLocked(ctx, path, 1)
	if err != nil {
		return Doc{}, err
	}
	return Doc{Path: path, Content: content, LastCommit: commits[0]}, nil
}

func isMissingPathErr(err error) bool {
	msg := err.Error()
	for _, marker := range []string{"does not exist", "invalid object name", "exists on disk, but not in"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// Search runs a case-insensitive fixed-string search across all non-archived
// markdown docs. No matches is an empty result, not an error.
func (s *GitStore) Search(query string) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, &ValidationError{Field: "query", Reason: "search query is empty"}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	// -z NUL-terminates the path (and line number) so parsing survives ":"
	// inside matched text; exit 1 means no matches.
	out, code, err := s.r.runExit(ctx,
		"grep", "-I", "-i", "-n", "-F", "-z", "-e", query, "--", "*.md", ":^"+archiveDir)
	if err != nil {
		return nil, err
	}
	switch code {
	case 0:
	case 1:
		return nil, nil
	default:
		return nil, fmt.Errorf("git grep failed with exit code %d", code)
	}

	var hits []SearchHit
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		lineNo, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		hits = append(hits, SearchHit{Path: parts[0], Line: lineNo, Text: strings.TrimSpace(parts[2])})
	}
	return hits, nil
}

// History returns the newest-first commits touching path, up to n (default 10).
func (s *GitStore) History(path string, n int) ([]Commit, error) {
	if err := validatePath(path, true); err != nil {
		return nil, err
	}
	if n <= 0 {
		n = 10
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, cancel := s.opCtx()
	defer cancel()
	return s.historyLocked(ctx, path, n)
}

func (s *GitStore) historyLocked(ctx context.Context, path string, n int) ([]Commit, error) {
	out, err := s.r.runRaw(ctx,
		"log", "-n", strconv.Itoa(n), "--follow", historyFormat, "--", path)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 7)
		if len(f) != 7 {
			return nil, fmt.Errorf("unexpected git log record: %q", rec)
		}
		date, err := time.Parse(time.RFC3339, f[4])
		if err != nil {
			return nil, fmt.Errorf("parse commit date: %w", err)
		}
		commits = append(commits, Commit{
			Hash:        f[0],
			ShortHash:   f[1],
			AuthorName:  f[2],
			AuthorEmail: f[3],
			Date:        date,
			Subject:     f[5],
			Body:        strings.TrimSpace(f[6]),
		})
	}
	if len(commits) == 0 {
		// git log exits 0 with empty output for a path that never existed.
		return nil, s.notFound(ctx, path)
	}
	return commits, nil
}

// revPattern is deliberately strict: hashes, branch names, HEAD~n and
// rev^ suffixes. No leading dash, so revisions can never be parsed as flags.
var revPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./~^-]*$`)

// Diff returns the unified diff of path between two revisions
// (revB defaults to HEAD). Identical revisions yield an empty string.
func (s *GitStore) Diff(path, revA, revB string) (string, error) {
	if err := validatePath(path, true); err != nil {
		return "", err
	}
	if revB == "" {
		revB = "HEAD"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx, cancel := s.opCtx()
	defer cancel()

	for _, rev := range []string{revA, revB} {
		if !revPattern.MatchString(rev) {
			return "", &RevisionError{Rev: rev}
		}
		if _, code, err := s.r.runExit(ctx, "rev-parse", "--verify", "-q", rev+"^{commit}"); err != nil {
			return "", err
		} else if code != 0 {
			return "", &RevisionError{Rev: rev}
		}
	}
	return s.r.runRaw(ctx, "diff", revA, revB, "--", path)
}

// notFound builds a NotFoundError with close-match suggestions.
func (s *GitStore) notFound(ctx context.Context, path string) error {
	return &NotFoundError{Path: path, Suggestions: s.suggest(ctx, path)}
}

// suggest ranks tracked .md files by basename similarity to the missing path:
// exact basename elsewhere, then substring overlap, then small edit distance.
func (s *GitStore) suggest(ctx context.Context, path string) []string {
	out, err := s.r.runRaw(ctx, "ls-files", "-z")
	if err != nil {
		return nil
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ".md"))
	if base == "" {
		return nil
	}

	type candidate struct {
		path  string
		score int
	}
	var cands []candidate
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || !strings.HasSuffix(p, ".md") || p == path {
			continue
		}
		b := strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".md"))
		switch {
		case b == base:
			cands = append(cands, candidate{p, 0})
		case strings.Contains(b, base) || strings.Contains(base, b):
			cands = append(cands, candidate{p, 1})
		default:
			if d := levenshtein(base, b); d <= 3 {
				cands = append(cands, candidate{p, 1 + d})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score < cands[j].score })
	if len(cands) > 3 {
		cands = cands[:3]
	}
	suggestions := make([]string, len(cands))
	for i, c := range cands {
		suggestions[i] = c.path
	}
	return suggestions
}

// levenshtein computes edit distance with the classic two-row DP.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}
