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

const validTokens = `
tokens:
  - token: "dsl_chalvin_0123456789abcdef"
    name: "Chalvin (agent)"
    email: "chalvin-agent@cohort.local"
  - token: "dsl_bima_0123456789abcdef"
    name: "Bima (agent)"
    email: "bima-agent@cohort.local"
`

func TestLoad(t *testing.T) {
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
    email: "a@cohort.local"
  - token: "dsl_same_0123456789abcdef"
    name: "B"
    email: "b@cohort.local"
`,
			wantErr: "duplicate token",
		},
		{
			name: "short token",
			yaml: "repo_dir: " + repoDir + `
tokens:
  - token: "short"
    name: "A"
    email: "a@cohort.local"
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
    email: "a@cohort.local"
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
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("want error for missing file")
	}
}
