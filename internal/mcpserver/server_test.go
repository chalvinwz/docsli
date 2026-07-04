package mcpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalvinwz/docsli/internal/auth"
	"github.com/chalvinwz/docsli/internal/config"
	"github.com/chalvinwz/docsli/internal/gitstore"
)

const (
	aliceToken = "dsl_alice_0123456789abcdef"
	bobToken   = "dsl_bob_0123456789abcdefgh"
)

var testTokens = []config.TokenEntry{
	{Token: aliceToken, Name: "Alice (agent)", Email: "alice-agent@cohort.local"},
	{Token: bobToken, Name: "Bob (agent)", Email: "bob-agent@cohort.local"},
}

// newTestServer boots the full production stack — gitstore on a temp repo,
// MCP server, streamable HTTP handler, bearer-token middleware — on an
// httptest server.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	store, err := gitstore.Open(t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(store, slog.New(slog.DiscardHandler), "test")
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkauth.RequireBearerToken(auth.Verifier(testTokens), nil)(handler))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

type authTransport struct {
	token string
	base  http.RoundTripper
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(req)
}

// connect opens an MCP session authenticated with token.
func connect(t *testing.T, ts *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "docsli-test", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: &authTransport{token: token, base: http.DefaultTransport}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func structured(t *testing.T, res *mcp.CallToolResult, into any) {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
}

func errorText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestTwoAgentCollaboration is the definition-of-done in miniature: Alice
// creates a PRD, Bob updates it, and doc_history shows both commits with
// distinct authors and their why messages.
func TestTwoAgentCollaboration(t *testing.T) {
	ts := newTestServer(t)
	aliceSess := connect(t, ts, aliceToken)
	bobSess := connect(t, ts, bobToken)

	res := callTool(t, aliceSess, "doc_create", map[string]any{
		"path":    "docs/payment-prd.md",
		"content": "# Payment PRD\n\nDraft v1.\n",
		"why":     "kick off the payments project with a first PRD draft",
	})
	if res.IsError {
		t.Fatalf("doc_create failed: %s", errorText(res))
	}
	var created struct {
		Rev string `json:"rev"`
	}
	structured(t, res, &created)
	if created.Rev == "" {
		t.Error("create result missing rev")
	}

	res = callTool(t, bobSess, "doc_read", map[string]any{"path": "docs/payment-prd.md"})
	if res.IsError {
		t.Fatalf("doc_read failed: %s", errorText(res))
	}
	var doc gitstore.Doc
	structured(t, res, &doc)
	if doc.Content != "# Payment PRD\n\nDraft v1.\n" {
		t.Errorf("content = %q", doc.Content)
	}
	if doc.LastCommit.AuthorName != "Alice (agent)" {
		t.Errorf("last author = %q, want Alice", doc.LastCommit.AuthorName)
	}

	res = callTool(t, bobSess, "doc_update", map[string]any{
		"path":         "docs/payment-prd.md",
		"content":      "# Payment PRD\n\nDraft v2 with Bob's edits.\n",
		"why":          "add risk analysis after reviewing the draft",
		"expected_rev": doc.LastCommit.ShortHash,
	})
	if res.IsError {
		t.Fatalf("doc_update failed: %s", errorText(res))
	}

	res = callTool(t, aliceSess, "doc_history", map[string]any{"path": "docs/payment-prd.md"})
	if res.IsError {
		t.Fatalf("doc_history failed: %s", errorText(res))
	}
	var hist struct {
		Commits []gitstore.Commit `json:"commits"`
	}
	structured(t, res, &hist)
	if len(hist.Commits) != 2 {
		t.Fatalf("history has %d commits, want 2", len(hist.Commits))
	}
	if hist.Commits[0].AuthorName != "Bob (agent)" || hist.Commits[1].AuthorName != "Alice (agent)" {
		t.Errorf("authors = %q, %q", hist.Commits[0].AuthorName, hist.Commits[1].AuthorName)
	}
	if !strings.Contains(hist.Commits[0].Body, "risk analysis") {
		t.Errorf("why missing from history: %+v", hist.Commits[0])
	}
}

func TestToolErrorsReachTheAgent(t *testing.T) {
	ts := newTestServer(t)
	sess := connect(t, ts, aliceToken)

	res := callTool(t, sess, "doc_update", map[string]any{
		"path":    "docs/does-not-exist.md",
		"content": "x\n",
		"why":     "update a doc that was never created",
	})
	if !res.IsError {
		t.Fatal("update of missing doc must be a tool error")
	}
	if txt := errorText(res); !strings.Contains(txt, "not found") {
		t.Errorf("error text should guide the agent: %q", txt)
	}

	res = callTool(t, sess, "doc_create", map[string]any{
		"path":    "docs/x.md",
		"content": "x\n",
		"why":     "short", // under the 10-char minimum
	})
	if !res.IsError {
		t.Fatal("trivial why must be rejected")
	}
	if txt := errorText(res); !strings.Contains(txt, "why") {
		t.Errorf("error text should name the why parameter: %q", txt)
	}
}

func TestConflictSurfacesToAgent(t *testing.T) {
	ts := newTestServer(t)
	sess := connect(t, ts, aliceToken)

	callTool(t, sess, "doc_create", map[string]any{
		"path": "docs/c.md", "content": "v1\n", "why": "seed doc for conflict test",
	})
	res := callTool(t, sess, "doc_read", map[string]any{"path": "docs/c.md"})
	var doc gitstore.Doc
	structured(t, res, &doc)
	staleRev := doc.LastCommit.ShortHash

	callTool(t, sess, "doc_update", map[string]any{
		"path": "docs/c.md", "content": "v2\n", "why": "someone else edits in between",
	})

	res = callTool(t, sess, "doc_update", map[string]any{
		"path": "docs/c.md", "content": "v3\n", "why": "guarded update with stale rev",
		"expected_rev": staleRev,
	})
	if !res.IsError {
		t.Fatal("stale expected_rev must conflict")
	}
	if txt := errorText(res); !strings.Contains(txt, "changed since you read it") {
		t.Errorf("conflict text = %q", txt)
	}
}

func TestSearchAndListOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	sess := connect(t, ts, aliceToken)

	callTool(t, sess, "doc_create", map[string]any{
		"path": "docs/adr-001.md", "content": "# ADR 001\n\nWe chose git as the database.\n",
		"why": "record the storage architecture decision",
	})

	res := callTool(t, sess, "doc_search", map[string]any{"query": "GIT AS THE DATABASE"})
	var search struct {
		Count int `json:"count"`
	}
	structured(t, res, &search)
	if search.Count != 1 {
		t.Errorf("search count = %d, want 1 (case-insensitive)", search.Count)
	}

	res = callTool(t, sess, "doc_list", map[string]any{})
	var list struct {
		Docs []gitstore.DocMeta `json:"docs"`
	}
	structured(t, res, &list)
	found := false
	for _, d := range list.Docs {
		if d.Path == "docs/adr-001.md" && d.Title == "ADR 001" {
			found = true
		}
	}
	if !found {
		t.Errorf("doc_list missing created doc: %+v", list.Docs)
	}
}

func TestRejectsBadToken(t *testing.T) {
	ts := newTestServer(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "docsli-test", Version: "test"}, nil)
	_, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: &authTransport{token: "dsl_wrong_0123456789abcd", base: http.DefaultTransport}},
	}, nil)
	if err == nil {
		t.Fatal("connect with unknown token must fail")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("want Unauthorized in error, got: %v", err)
	}
}
