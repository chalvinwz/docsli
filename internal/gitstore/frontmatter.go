package gitstore

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Meta is the YAML frontmatter every document carries. It holds only what
// git cannot derive — lifecycle status, tags, doc type. Authors and dates
// deliberately live in commits, not here.
type Meta struct {
	Status string   `yaml:"status" json:"status"`
	Tags   []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Type   string   `yaml:"type,omitempty" json:"type,omitempty"`
}

var (
	validStatuses = []string{"draft", "in-review", "approved", "superseded"}
	validTypes    = []string{"prd", "adr", "proposal", "note"}
)

const fmDelim = "---"

// defaultFrontmatter is injected when doc_create receives content without a
// frontmatter block.
const defaultFrontmatter = fmDelim + "\nstatus: draft\n" + fmDelim + "\n\n"

// parseFrontmatter splits content into validated Meta and the markdown body.
// found is false when content has no frontmatter block at all; a block that
// exists but fails to parse or validate is an error.
func parseFrontmatter(content string) (meta Meta, body string, found bool, err error) {
	rest, ok := strings.CutPrefix(content, fmDelim+"\n")
	if !ok {
		return Meta{}, content, false, nil
	}
	block, after, ok := strings.Cut(rest, "\n"+fmDelim+"\n")
	if !ok {
		if strings.HasSuffix(rest, "\n"+fmDelim) {
			block, after = strings.TrimSuffix(rest, "\n"+fmDelim), ""
		} else {
			return Meta{}, content, true, &ValidationError{
				Field:  "content",
				Reason: "frontmatter block is not closed; end it with a --- line",
			}
		}
	}

	dec := yaml.NewDecoder(strings.NewReader(block))
	dec.KnownFields(true)
	if err := dec.Decode(&meta); err != nil {
		return Meta{}, content, true, &ValidationError{
			Field:  "content",
			Reason: fmt.Sprintf("frontmatter is not valid YAML with only status/tags/type keys: %v", err),
		}
	}
	if err := meta.validate(); err != nil {
		return Meta{}, content, true, err
	}
	return meta, strings.TrimPrefix(after, "\n"), true, nil
}

func (m Meta) validate() error {
	if !slices.Contains(validStatuses, m.Status) {
		return &ValidationError{
			Field:  "content",
			Reason: fmt.Sprintf("frontmatter status %q must be one of: %s", m.Status, strings.Join(validStatuses, ", ")),
		}
	}
	if m.Type != "" && !slices.Contains(validTypes, m.Type) {
		return &ValidationError{
			Field:  "content",
			Reason: fmt.Sprintf("frontmatter type %q must be one of: %s", m.Type, strings.Join(validTypes, ", ")),
		}
	}
	for _, tag := range m.Tags {
		if strings.TrimSpace(tag) == "" {
			return &ValidationError{Field: "content", Reason: "frontmatter tags must not be empty strings"}
		}
	}
	return nil
}
