package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/chalvinwz/docsli/internal/config"
)

var testTokens = []config.TokenEntry{
	{Token: "dsl_alice_0123456789abcdef", Name: "Alice (agent)", Email: "alice-agent@cohort.local"},
	{Token: "dsl_bob_0123456789abcdefgh", Name: "Bob (agent)", Email: "bob-agent@cohort.local"},
}

func TestVerifier(t *testing.T) {
	verify := Verifier(testTokens)

	info, err := verify(context.Background(), "dsl_bob_0123456789abcdefgh", nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Extra["name"] != "Bob (agent)" || info.Extra["email"] != "bob-agent@cohort.local" {
		t.Errorf("identity = %v", info.Extra)
	}
	if info.Expiration.IsZero() {
		t.Error("expiration must be non-zero or the SDK middleware rejects the token")
	}

	_, err = verify(context.Background(), "dsl_unknown_0123456789abc", nil)
	if !errors.Is(err, sdkauth.ErrInvalidToken) {
		t.Errorf("unknown token must unwrap to ErrInvalidToken, got %v", err)
	}
}

func TestMiddleware(t *testing.T) {
	handler := sdkauth.RequireBearerToken(Verifier(testTokens), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "valid token", authHeader: "Bearer dsl_alice_0123456789abcdef", wantStatus: http.StatusOK},
		{name: "missing header", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "unknown token", authHeader: "Bearer dsl_wrong_0123456789abcde", wantStatus: http.StatusUnauthorized},
		{name: "not bearer", authHeader: "Basic dXNlcjpwYXNz", wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
