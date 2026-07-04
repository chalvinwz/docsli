package gitstore

import (
	"fmt"
	"strings"
)

// The error messages below are read by AI agents, not humans: each one states
// what went wrong and which tool call to make next.

// NotFoundError reports a missing document, with close-match suggestions.
type NotFoundError struct {
	Path        string
	Suggestions []string
}

func (e *NotFoundError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "doc %q not found.", e.Path)
	if len(e.Suggestions) > 0 {
		fmt.Fprintf(&b, " Did you mean: %s?", strings.Join(e.Suggestions, ", "))
	}
	b.WriteString(" Use doc_list to browse available docs.")
	return b.String()
}

// ExistsError reports a create colliding with an existing document.
type ExistsError struct {
	Path string
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("doc %q already exists. Use doc_update to modify it, or choose a different path.", e.Path)
}

// ValidationError reports an invalid parameter value.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// NoChangeError reports an update whose content is identical to the current
// version; committing it would violate one-write-one-commit.
type NoChangeError struct {
	Path string
}

func (e *NoChangeError) Error() string {
	return fmt.Sprintf("doc %q already has exactly this content; nothing to update.", e.Path)
}

// RevisionError reports an unknown or unsafe git revision parameter.
type RevisionError struct {
	Rev string
}

func (e *RevisionError) Error() string {
	return fmt.Sprintf("unknown revision %q. Use doc_history to list valid revisions for this doc.", e.Rev)
}

// ConflictError reports an optimistic-locking failure: the document changed
// after the caller read it.
type ConflictError struct {
	Path       string
	CurrentRev string
	LastAuthor string
	LastWhy    string
}

func (e *ConflictError) Error() string {
	msg := fmt.Sprintf("doc %q changed since you read it — it is now at revision %s, last changed by %s", e.Path, e.CurrentRev, e.LastAuthor)
	if e.LastWhy != "" {
		msg += fmt.Sprintf(" (%q)", e.LastWhy)
	}
	return msg + ". Call doc_read again, merge your changes into the latest content, and retry with the new expected_rev."
}
