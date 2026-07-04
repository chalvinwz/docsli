// Package mcpserver exposes the document store as MCP tools. Tool and
// parameter descriptions are written for the AI agents that call them: each
// one says when to use the tool and how to recover from errors.
package mcpserver

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalvinwz/docsli/internal/gitstore"
)

// New returns an MCP server with all eight doc_* tools registered.
func New(store gitstore.Store, logger *slog.Logger, version string) *mcp.Server {
	if logger == nil {
		logger = slog.Default()
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "docsli", Version: version}, nil)
	h := &handlers{store: store, log: logger}

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_list",
		Description: "List the shared markdown documents: path, title, last-modified time, and last author. " +
			"Use this first to see what already exists and where docs live before creating or searching. " +
			"Archived (deleted) docs are hidden unless you pass folder='archive'.",
	}, h.docList)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_read",
		Description: "Read the full markdown content of one document, plus its last commit (author, date, why, revision). " +
			"Always read a doc before updating it, and pass last_commit.short_hash as expected_rev to doc_update " +
			"so you never overwrite a change someone else made in the meantime. " +
			"Deleted docs remain readable at their archive/ path.",
	}, h.docRead)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_search",
		Description: "Case-insensitive full-text search across all non-archived documents. " +
			"Returns matching lines with path and line number. " +
			"Use this to find documents by content when you do not know the exact path.",
	}, h.docSearch)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_create",
		Description: "Create a new markdown document and commit it under your identity. " +
			"Fails if the path is already taken — use doc_update for existing docs. " +
			"Parent folders are created automatically. " +
			"The why parameter is mandatory: it becomes the permanent commit message every collaborator sees.",
	}, h.docCreate)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_update",
		Description: "Replace the ENTIRE content of an existing document — send the complete new document, not a fragment or diff. " +
			"Fails if the doc does not exist (use doc_create for new docs) or if the content is unchanged. " +
			"Strongly recommended: pass expected_rev from your last doc_read; the update is then rejected with " +
			"a conflict if someone changed the doc since you read it, instead of silently overwriting their work.",
	}, h.docUpdate)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_delete",
		Description: "Soft-delete a document: it is moved into archive/ in one commit and stays readable " +
			"via doc_read at its archive/ path. Nothing is ever erased from history, so deletion is recoverable. " +
			"The why parameter is mandatory and becomes the permanent commit message.",
	}, h.docDelete)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_history",
		Description: "List the commits that touched one document, newest first: revision hashes, author, date, " +
			"and each change's why message. Use this to understand how a doc evolved, who changed it, and to find " +
			"revisions for doc_diff.",
	}, h.docHistory)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "doc_diff",
		Description: "Unified diff of one document between two revisions (get revisions from doc_history). " +
			"rev_b defaults to HEAD, the current version, so passing only rev_a shows everything that changed since then.",
	}, h.docDiff)

	return srv
}
