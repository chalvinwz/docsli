// Package auth maps bearer tokens to commit identities. It plugs into the
// MCP SDK's RequireBearerToken middleware: the verifier resolves a token to a
// TokenInfo carrying the identity, and tool handlers recover it from the
// request's Extra field (per-request context values do not survive the
// transport's jsonrpc handling).
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalvinwz/docsli/internal/config"
	"github.com/chalvinwz/docsli/internal/gitstore"
)

// tokenTTL satisfies the SDK middleware's requirement for a non-zero
// expiration. Static tokens are re-verified on every request, so the value
// only needs to outlive a single request.
const tokenTTL = time.Hour

type entry struct {
	hash  [sha256.Size]byte
	name  string
	email string
}

// Verifier returns a TokenVerifier for the configured static tokens. The
// presented token is compared in constant time against every entry — no map
// lookup, no early exit — so timing reveals nothing about token values.
func Verifier(tokens []config.TokenEntry) sdkauth.TokenVerifier {
	entries := make([]entry, len(tokens))
	for i, t := range tokens {
		entries[i] = entry{hash: sha256.Sum256([]byte(t.Token)), name: t.Name, email: t.Email}
	}
	return func(_ context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		presented := sha256.Sum256([]byte(token))
		match := -1
		for i := range entries {
			if subtle.ConstantTimeCompare(presented[:], entries[i].hash[:]) == 1 && match < 0 {
				match = i
			}
		}
		if match < 0 {
			// Must unwrap to ErrInvalidToken so the middleware answers 401.
			return nil, fmt.Errorf("unknown bearer token: %w", sdkauth.ErrInvalidToken)
		}
		e := entries[match]
		return &sdkauth.TokenInfo{
			UserID:     e.name,
			Expiration: time.Now().Add(tokenTTL),
			Extra:      map[string]any{"name": e.name, "email": e.email},
		}, nil
	}
}

// ErrNoIdentity means a tool call arrived without the middleware-attached
// identity; with auth correctly mounted in front of the MCP handler this
// cannot happen.
var ErrNoIdentity = errors.New("request carries no authenticated identity")

// IdentityFromRequest recovers the commit identity that Verifier attached.
func IdentityFromRequest(req *mcp.CallToolRequest) (gitstore.Identity, error) {
	extra := req.GetExtra()
	if extra == nil || extra.TokenInfo == nil {
		return gitstore.Identity{}, ErrNoIdentity
	}
	name, _ := extra.TokenInfo.Extra["name"].(string)
	email, _ := extra.TokenInfo.Extra["email"].(string)
	if name == "" || email == "" {
		return gitstore.Identity{}, ErrNoIdentity
	}
	return gitstore.Identity{Name: name, Email: email}, nil
}
