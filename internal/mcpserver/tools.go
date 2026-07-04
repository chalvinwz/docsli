package mcpserver

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalvinwz/docsli/internal/auth"
	"github.com/chalvinwz/docsli/internal/gitstore"
)

type handlers struct {
	store gitstore.Store
	log   *slog.Logger
}

// logWrite records every successful write: who did what to which path.
// Never tokens, never content.
func (h *handlers) logWrite(tool, path string, id gitstore.Identity) {
	h.log.Info("write", "tool", tool, "path", path, "author", id.Name, "email", id.Email)
}

// lastRev returns the doc's current short revision, or "" when unavailable;
// write results carry it so agents can chain guarded updates.
func (h *handlers) lastRev(path string) string {
	commits, err := h.store.History(path, 1)
	if err != nil || len(commits) == 0 {
		return ""
	}
	return commits[0].ShortHash
}

type docListIn struct {
	Folder string `json:"folder,omitempty" jsonschema:"Optional subfolder to list, e.g. 'docs'. Pass 'archive' to list soft-deleted docs. Empty lists everything except the archive."`
}

type docListOut struct {
	Docs  []gitstore.DocMeta `json:"docs"`
	Count int                `json:"count"`
}

func (h *handlers) docList(_ context.Context, _ *mcp.CallToolRequest, in docListIn) (*mcp.CallToolResult, docListOut, error) {
	docs, err := h.store.List(in.Folder)
	if err != nil {
		return nil, docListOut{}, err
	}
	return nil, docListOut{Docs: docs, Count: len(docs)}, nil
}

type docReadIn struct {
	Path string `json:"path" jsonschema:"Repo-relative path of the document, e.g. docs/plan.md. Archived docs live under archive/."`
}

func (h *handlers) docRead(_ context.Context, _ *mcp.CallToolRequest, in docReadIn) (*mcp.CallToolResult, gitstore.Doc, error) {
	doc, err := h.store.Read(in.Path)
	if err != nil {
		return nil, gitstore.Doc{}, err
	}
	return nil, doc, nil
}

type docSearchIn struct {
	Query string `json:"query" jsonschema:"Text to search for. Matched case-insensitively as a fixed string, not a regex."`
}

type docSearchOut struct {
	Hits  []gitstore.SearchHit `json:"hits"`
	Count int                  `json:"count"`
}

func (h *handlers) docSearch(_ context.Context, _ *mcp.CallToolRequest, in docSearchIn) (*mcp.CallToolResult, docSearchOut, error) {
	hits, err := h.store.Search(in.Query)
	if err != nil {
		return nil, docSearchOut{}, err
	}
	return nil, docSearchOut{Hits: hits, Count: len(hits)}, nil
}

// writeOut is the shared result of create/update/delete: a confirmation plus
// the new revision for chaining guarded updates.
type writeOut struct {
	Path    string `json:"path"`
	Rev     string `json:"rev,omitempty"`
	Message string `json:"message"`
}

type docCreateIn struct {
	Path    string `json:"path" jsonschema:"Repo-relative path for the new document, must end in .md, e.g. docs/payment-prd.md. Parent folders are created automatically."`
	Content string `json:"content" jsonschema:"Full markdown content of the document. Start with a '# Title' heading so listings show a meaningful title."`
	Why     string `json:"why" jsonschema:"One or two sentences explaining WHY this document is being created. Becomes the permanent git commit message visible to all collaborators. Minimum 10 characters."`
}

func (h *handlers) docCreate(_ context.Context, req *mcp.CallToolRequest, in docCreateIn) (*mcp.CallToolResult, writeOut, error) {
	id, err := auth.IdentityFromRequest(req)
	if err != nil {
		return nil, writeOut{}, err
	}
	if err := h.store.Create(in.Path, in.Content, in.Why, id); err != nil {
		return nil, writeOut{}, err
	}
	h.logWrite("doc_create", in.Path, id)
	return nil, writeOut{Path: in.Path, Rev: h.lastRev(in.Path), Message: "Created " + in.Path}, nil
}

type docUpdateIn struct {
	Path        string `json:"path" jsonschema:"Repo-relative path of the existing document to update."`
	Content     string `json:"content" jsonschema:"The COMPLETE new markdown content. This replaces the whole document, so include everything that should remain, not just your changes."`
	Why         string `json:"why" jsonschema:"One or two sentences explaining WHY you are making this change. Becomes the permanent git commit message. Minimum 10 characters."`
	ExpectedRev string `json:"expected_rev,omitempty" jsonschema:"Strongly recommended: the last_commit.short_hash you got from doc_read. If the doc changed since, the update is rejected with the current revision instead of overwriting someone else's work. Omit to force last-write-wins."`
}

func (h *handlers) docUpdate(_ context.Context, req *mcp.CallToolRequest, in docUpdateIn) (*mcp.CallToolResult, writeOut, error) {
	id, err := auth.IdentityFromRequest(req)
	if err != nil {
		return nil, writeOut{}, err
	}
	if err := h.store.Update(in.Path, in.Content, in.Why, in.ExpectedRev, id); err != nil {
		return nil, writeOut{}, err
	}
	h.logWrite("doc_update", in.Path, id)
	return nil, writeOut{Path: in.Path, Rev: h.lastRev(in.Path), Message: "Updated " + in.Path}, nil
}

type docDeleteIn struct {
	Path string `json:"path" jsonschema:"Repo-relative path of the document to soft-delete. It moves to archive/ and remains readable there."`
	Why  string `json:"why" jsonschema:"One or two sentences explaining WHY this document is being archived. Becomes the permanent git commit message. Minimum 10 characters."`
}

func (h *handlers) docDelete(_ context.Context, req *mcp.CallToolRequest, in docDeleteIn) (*mcp.CallToolResult, writeOut, error) {
	id, err := auth.IdentityFromRequest(req)
	if err != nil {
		return nil, writeOut{}, err
	}
	if err := h.store.Delete(in.Path, in.Why, id); err != nil {
		return nil, writeOut{}, err
	}
	h.logWrite("doc_delete", in.Path, id)
	return nil, writeOut{Path: in.Path, Message: "Archived " + in.Path + "; it remains readable under archive/"}, nil
}

type docHistoryIn struct {
	Path string `json:"path" jsonschema:"Repo-relative path of the document."`
	N    int    `json:"n,omitempty" jsonschema:"Maximum number of commits to return, newest first. Defaults to 10."`
}

type docHistoryOut struct {
	Path    string            `json:"path"`
	Commits []gitstore.Commit `json:"commits"`
}

func (h *handlers) docHistory(_ context.Context, _ *mcp.CallToolRequest, in docHistoryIn) (*mcp.CallToolResult, docHistoryOut, error) {
	commits, err := h.store.History(in.Path, in.N)
	if err != nil {
		return nil, docHistoryOut{}, err
	}
	return nil, docHistoryOut{Path: in.Path, Commits: commits}, nil
}

type docDiffIn struct {
	Path string `json:"path" jsonschema:"Repo-relative path of the document."`
	RevA string `json:"rev_a" jsonschema:"Older revision to compare from — a hash from doc_history."`
	RevB string `json:"rev_b,omitempty" jsonschema:"Newer revision to compare to. Defaults to HEAD, the current version."`
}

type docDiffOut struct {
	Path string `json:"path"`
	RevA string `json:"rev_a"`
	RevB string `json:"rev_b"`
	Diff string `json:"diff"`
}

func (h *handlers) docDiff(_ context.Context, _ *mcp.CallToolRequest, in docDiffIn) (*mcp.CallToolResult, docDiffOut, error) {
	diff, err := h.store.Diff(in.Path, in.RevA, in.RevB)
	if err != nil {
		return nil, docDiffOut{}, err
	}
	revB := in.RevB
	if revB == "" {
		revB = "HEAD"
	}
	return nil, docDiffOut{Path: in.Path, RevA: in.RevA, RevB: revB, Diff: diff}, nil
}
