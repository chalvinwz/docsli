package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// clearEnv shields tests from DOCSLI_* variables on the host and gives each
// test a clean slate to set its own.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, v := range envVars {
		t.Setenv(v, "")
	}
}

const validTokens = `
tokens:
  - token: "dsl_chalvin_0123456789abcdef"
    name: "Chalvin (agent)"
    email: "chalvin-agent@example.com"
  - token: "dsl_john_0123456789abcdef"
    name: "John (agent)"
    email: "john-agent@example.com"
`

func TestLoad(t *testing.T) {
	clearEnv(t)
	repoDir := filepath.Join(t.TempDir(), "docs")

	tests := []struct {
		name    string
		yaml    string
		wantErr string // substring; empty means success
	}{
		{
			name: "valid",
			yaml: "listen: \":9090\"\nrepo_dir: " + repoDir + "\n" + validTokens,
		},
		{
			name: "defaults applied",
			yaml: "repo_dir: " + repoDir + "\n" + validTokens,
		},
		{
			name:    "missing repo_dir",
			yaml:    validTokens,
			wantErr: "repo_dir is required",
		},
		{
			name:    "missing repo parent",
			yaml:    "repo_dir: /nonexistent-docsli-parent/docs\n" + validTokens,
			wantErr: "does not exist",
		},
		{
			name:    "no tokens",
			yaml:    "repo_dir: " + repoDir + "\ntokens: []\n",
			wantErr: "at least one token",
		},
		{
			name: "duplicate token",
			yaml: "repo_dir: " + repoDir + `
tokens:
  - token: "dsl_same_0123456789abcdef"
    name: "A"
    email: "a@example.com"
  - token: "dsl_same_0123456789abcdef"
    name: "B"
    email: "b@example.com"
`,
			wantErr: "duplicate token",
		},
		{
			name: "short token",
			yaml: "repo_dir: " + repoDir + `
tokens:
  - token: "short"
    name: "A"
    email: "a@example.com"
`,
			wantErr: "at least 16 characters",
		},
		{
			name: "invalid email",
			yaml: "repo_dir: " + repoDir + `
tokens:
  - token: "dsl_a_0123456789abcdef"
    name: "A"
    email: "not-an-email"
`,
			wantErr: "invalid email",
		},
		{
			name: "missing name",
			yaml: "repo_dir: " + repoDir + `
tokens:
  - token: "dsl_a_0123456789abcdef"
    name: ""
    email: "a@example.com"
`,
			wantErr: "name is required",
		},
		{
			name:    "unknown field",
			yaml:    "repo_dir: " + repoDir + "\nbogus: true\n" + validTokens,
			wantErr: "bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, tt.yaml))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Listen == "" || cfg.Mirror.Remote == "" {
				t.Errorf("defaults not applied: %+v", cfg)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	clearEnv(t)
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("want error for missing file when no env config is set")
	}
}

func TestLoadEnvOnly(t *testing.T) {
	clearEnv(t)
	repoDir := filepath.Join(t.TempDir(), "docs")
	t.Setenv("DOCSLI_LISTEN", ":9999")
	t.Setenv("DOCSLI_REPO_DIR", repoDir)
	t.Setenv("DOCSLI_TOKENS",
		"dsl_chalvin_0123456789abcdef:Chalvin (agent):chalvin-agent@example.com; "+
			"dsl_john_0123456789abcdef:John (agent):john-agent@example.com")

	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9999" || cfg.RepoDir != repoDir {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Tokens) != 2 {
		t.Fatalf("tokens = %+v, want 2", cfg.Tokens)
	}
	if cfg.Tokens[1].Name != "John (agent)" || cfg.Tokens[1].Email != "john-agent@example.com" {
		t.Errorf("token[1] = %+v", cfg.Tokens[1])
	}
	if cfg.Mirror.Remote != "origin" {
		t.Errorf("defaults must still apply, mirror.remote = %q", cfg.Mirror.Remote)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	repoDir := filepath.Join(t.TempDir(), "docs")
	path := writeConfig(t, "listen: \":8080\"\nrepo_dir: "+repoDir+"\n"+validTokens)

	t.Setenv("DOCSLI_LISTEN", ":7777")
	t.Setenv("DOCSLI_TOKENS", "dsl_env_0123456789abcdefgh:Env (agent):env-agent@example.com")
	t.Setenv("DOCSLI_MIRROR_ENABLED", "true")
	t.Setenv("DOCSLI_MIRROR_REMOTE", "backup")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":7777" {
		t.Errorf("listen = %q, env must win", cfg.Listen)
	}
	if len(cfg.Tokens) != 1 || cfg.Tokens[0].Name != "Env (agent)" {
		t.Errorf("DOCSLI_TOKENS must replace the file's token list: %+v", cfg.Tokens)
	}
	if !cfg.Mirror.Enabled || cfg.Mirror.Remote != "backup" {
		t.Errorf("mirror = %+v", cfg.Mirror)
	}
}

func TestLoadEnvErrors(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "docs")

	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "malformed tokens", key: "DOCSLI_TOKENS", value: "just-a-token-no-identity", wantErr: "token:name:email"},
		{name: "bad bool", key: "DOCSLI_MIRROR_ENABLED", value: "yep", wantErr: "not a boolean"},
		{name: "invalid email still validated", key: "DOCSLI_TOKENS", value: "dsl_x_0123456789abcdef:X:not-an-email", wantErr: "invalid email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("DOCSLI_REPO_DIR", repoDir)
			t.Setenv(tt.key, tt.value)
			if tt.key != "DOCSLI_TOKENS" {
				t.Setenv("DOCSLI_TOKENS", "dsl_ok_0123456789abcdefg:OK (agent):ok-agent@example.com")
			}
			_, err := Load(filepath.Join(t.TempDir(), "nope.yml"))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
