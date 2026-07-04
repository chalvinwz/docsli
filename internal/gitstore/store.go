// Package gitstore implements the document store on top of a plain git
// repository, shelling out to the git CLI. The repo is the only database:
// history is git log, diffs are git diff, and a human can always cd into the
// data directory and use ordinary git tooling.
package gitstore

import "time"

// Identity is the commit author attached to every write.
type Identity struct {
	Name  string
	Email string
}

// DocMeta describes a document in a listing. Authorship fields are derived
// from git history, never stored in the document itself.
type DocMeta struct {
	Path         string    `json:"path"`
	Title        string    `json:"title"`
	Meta         Meta      `json:"meta"`
	Created      time.Time `json:"created"`
	CreatedBy    string    `json:"created_by"`
	LastModified time.Time `json:"last_modified"`
	LastAuthor   string    `json:"last_author"`
}

// Doc is a full document plus the commit that last touched it.
type Doc struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Meta       Meta   `json:"meta"`
	LastCommit Commit `json:"last_commit"`
}

// ListQuery narrows a listing. Zero value lists every non-archived doc.
// Status, Tag, and Type match frontmatter fields case-insensitively.
type ListQuery struct {
	Folder string
	Status string
	Tag    string
	Type   string
}

// SearchHit is one matching line from a full-text search.
type SearchHit struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Commit is one entry of a document's history.
type Commit struct {
	Hash        string    `json:"hash"`
	ShortHash   string    `json:"short_hash"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	Date        time.Time `json:"date"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body,omitempty"`
}

// Store is the full document-store contract. All writes produce exactly one
// commit authored as the given identity, with why as the commit message body.
type Store interface {
	List(q ListQuery) ([]DocMeta, error)
	Read(path string) (Doc, error)
	Search(query string) ([]SearchHit, error)
	Create(path, content, why string, author Identity) error
	// Update replaces the full content. If expectedRev is non-empty it must
	// match the document's current last commit, otherwise a ConflictError is
	// returned (optimistic locking).
	Update(path, content, why, expectedRev string, author Identity) error
	Delete(path, why string, author Identity) error // soft delete → archive/
	History(path string, n int) ([]Commit, error)
	Diff(path, revA, revB string) (string, error)
}
