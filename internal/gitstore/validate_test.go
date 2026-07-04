package gitstore

import (
	"errors"
	"testing"
)

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		allowArchive bool
		wantErr      bool
	}{
		{name: "simple", path: "plan.md"},
		{name: "nested", path: "docs/adr/001-git-store.md"},
		{name: "spaces ok", path: "docs/meeting notes.md"},
		{name: "empty", path: "", wantErr: true},
		{name: "whitespace only", path: "   ", wantErr: true},
		{name: "absolute", path: "/etc/passwd.md", wantErr: true},
		{name: "traversal", path: "../outside.md", wantErr: true},
		{name: "nested traversal", path: "docs/../../outside.md", wantErr: true},
		{name: "non markdown", path: "script.sh", wantErr: true},
		{name: "no extension", path: "docs/plan", wantErr: true},
		{name: "git internals", path: ".git/config.md", wantErr: true},
		{name: "git prefixed segment", path: "docs/.gitignore.md", wantErr: true},
		{name: "backslash", path: "docs\\plan.md", wantErr: true},
		{name: "control char", path: "docs/pl\nan.md", wantErr: true},
		{name: "not normalized", path: "./docs/plan.md", wantErr: true},
		{name: "duplicate slash", path: "docs//plan.md", wantErr: true},
		{name: "archive write blocked", path: "archive/old.md", wantErr: true},
		{name: "archive read allowed", path: "archive/old.md", allowArchive: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path, tt.allowArchive)
			if tt.wantErr && err == nil {
				t.Errorf("validatePath(%q) = nil, want error", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validatePath(%q) = %v, want nil", tt.path, err)
			}
			if err != nil {
				var verr *ValidationError
				if !errors.As(err, &verr) {
					t.Errorf("error is %T, want *ValidationError", err)
				}
			}
		})
	}
}

func TestValidateWhy(t *testing.T) {
	tests := []struct {
		name    string
		why     string
		wantErr bool
	}{
		{name: "good", why: "add ADR documenting the git-as-database decision"},
		{name: "exactly 10", why: "0123456789"},
		{name: "empty", why: "", wantErr: true},
		{name: "too short", why: "fix", wantErr: true},
		{name: "padding does not count", why: "hi        ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWhy(tt.why)
			if tt.wantErr && err == nil {
				t.Errorf("validateWhy(%q) = nil, want error", tt.why)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateWhy(%q) = %v, want nil", tt.why, err)
			}
		})
	}
}
