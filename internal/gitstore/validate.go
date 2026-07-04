package gitstore

import (
	"path/filepath"
	"strings"
)

const minWhyLen = 10

// archiveDir is where soft-deleted documents live.
const archiveDir = "archive"

// validatePath enforces the path rules shared by every tool: repo-relative,
// normalized, .md only, no traversal, no .git internals. allowArchive permits
// reading archived docs; writes into archive/ are always managed by Delete.
func validatePath(p string, allowArchive bool) error {
	switch {
	case strings.TrimSpace(p) == "":
		return &ValidationError{Field: "path", Reason: "path is empty; use a repo-relative path like docs/plan.md"}
	case strings.ContainsRune(p, '\\'):
		return &ValidationError{Field: "path", Reason: "use forward slashes, not backslashes"}
	case hasControlChars(p):
		return &ValidationError{Field: "path", Reason: "path contains control characters"}
	case filepath.IsAbs(p) || strings.HasPrefix(p, "/"):
		return &ValidationError{Field: "path", Reason: "absolute paths are not allowed; use a repo-relative path like docs/plan.md"}
	case !strings.HasSuffix(p, ".md"):
		return &ValidationError{Field: "path", Reason: "only markdown files are allowed; the path must end in .md"}
	case !filepath.IsLocal(p):
		return &ValidationError{Field: "path", Reason: "path escapes the repository; .. traversal is not allowed"}
	case filepath.Clean(p) != p:
		return &ValidationError{Field: "path", Reason: "use a normalized path without ./ or duplicate slashes, like docs/plan.md"}
	}
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(strings.ToLower(seg), ".git") {
			return &ValidationError{Field: "path", Reason: "paths touching .git are not allowed"}
		}
	}
	if !allowArchive && (p == archiveDir || strings.HasPrefix(p, archiveDir+"/")) {
		return &ValidationError{Field: "path", Reason: "writing into archive/ is not allowed; archived docs are managed by doc_delete"}
	}
	return nil
}

func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validateWhy enforces the mandatory rationale attached to every write.
func validateWhy(why string) error {
	if len(strings.TrimSpace(why)) < minWhyLen {
		return &ValidationError{
			Field:  "why",
			Reason: "explain the reasoning behind this change in at least 10 characters; it becomes the permanent git commit message",
		}
	}
	return nil
}
