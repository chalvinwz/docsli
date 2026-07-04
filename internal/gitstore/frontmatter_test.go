package gitstore

import (
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantMeta  Meta
		wantBody  string
		wantFound bool
		wantErr   string // substring; empty means no error
	}{
		{
			name:      "no frontmatter",
			content:   "# Just a doc\n",
			wantBody:  "# Just a doc\n",
			wantFound: false,
		},
		{
			name:      "status only",
			content:   "---\nstatus: draft\n---\n\n# Doc\n",
			wantMeta:  Meta{Status: "draft"},
			wantBody:  "# Doc\n",
			wantFound: true,
		},
		{
			name:      "full metadata",
			content:   "---\nstatus: approved\ntags: [payments, billing]\ntype: prd\n---\n\n# PRD\n",
			wantMeta:  Meta{Status: "approved", Tags: []string{"payments", "billing"}, Type: "prd"},
			wantBody:  "# PRD\n",
			wantFound: true,
		},
		{
			name:      "closing delimiter at EOF",
			content:   "---\nstatus: draft\n---",
			wantMeta:  Meta{Status: "draft"},
			wantBody:  "",
			wantFound: true,
		},
		{
			name:      "unclosed block",
			content:   "---\nstatus: draft\n\n# Doc\n",
			wantFound: true,
			wantErr:   "not closed",
		},
		{
			name:      "invalid status",
			content:   "---\nstatus: wip\n---\n\nx\n",
			wantFound: true,
			wantErr:   "status",
		},
		{
			name:      "invalid type",
			content:   "---\nstatus: draft\ntype: memo\n---\n\nx\n",
			wantFound: true,
			wantErr:   "type",
		},
		{
			name:      "unknown key rejected",
			content:   "---\nstatus: draft\nauthor: someone\n---\n\nx\n",
			wantFound: true,
			wantErr:   "status/tags/type",
		},
		{
			name:      "empty tag rejected",
			content:   "---\nstatus: draft\ntags: [\"\"]\n---\n\nx\n",
			wantFound: true,
			wantErr:   "tags",
		},
		{
			name:      "missing status",
			content:   "---\ntype: adr\n---\n\nx\n",
			wantFound: true,
			wantErr:   "status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, body, found, err := parseFrontmatter(tt.content)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if meta.Status != tt.wantMeta.Status || meta.Type != tt.wantMeta.Type || len(meta.Tags) != len(tt.wantMeta.Tags) {
				t.Errorf("meta = %+v, want %+v", meta, tt.wantMeta)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}
