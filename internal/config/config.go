// Package config loads and validates the docsli configuration, from a YAML
// file, DOCSLI_* environment variables, or both (env wins).
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration, loaded from a YAML file.
type Config struct {
	Listen  string       `yaml:"listen"`
	RepoDir string       `yaml:"repo_dir"`
	Mirror  Mirror       `yaml:"mirror"`
	Tokens  []TokenEntry `yaml:"tokens"`
}

// Mirror configures the optional one-way push to a git remote.
type Mirror struct {
	Enabled bool   `yaml:"enabled"`
	Remote  string `yaml:"remote"`
}

// TokenEntry maps a bearer token to the identity its commits are authored as.
type TokenEntry struct {
	Token string `yaml:"token"`
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

const minTokenLen = 16

// envVars are the environment overrides, applied on top of the config file.
// When the file is absent but any of these is set, docsli runs on env config
// alone — the twelve-factor path for docker compose.
var envVars = []string{
	"DOCSLI_LISTEN",
	"DOCSLI_REPO_DIR",
	"DOCSLI_MIRROR_ENABLED",
	"DOCSLI_MIRROR_REMOTE",
	"DOCSLI_TOKENS",
}

// Load builds the configuration from the YAML file at path (if it exists)
// plus DOCSLI_* environment variables (which win), then defaults and
// validates it.
func Load(path string) (*Config, error) {
	var cfg Config
	f, err := os.Open(path)
	switch {
	case err == nil:
		defer func() { _ = f.Close() }()
		dec := yaml.NewDecoder(f)
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	case os.IsNotExist(err) && envConfigured():
		// No file, but DOCSLI_* env vars are present: env-only config.
	default:
		return nil, fmt.Errorf("open config: %w (or configure via DOCSLI_* environment variables)", err)
	}

	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

func envConfigured() bool {
	for _, v := range envVars {
		if os.Getenv(v) != "" {
			return true
		}
	}
	return false
}

// applyEnv overlays DOCSLI_* environment variables onto the config.
// DOCSLI_TOKENS replaces the whole token list.
func (c *Config) applyEnv() error {
	var errs []error
	if v := os.Getenv("DOCSLI_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("DOCSLI_REPO_DIR"); v != "" {
		c.RepoDir = v
	}
	if v := os.Getenv("DOCSLI_MIRROR_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("DOCSLI_MIRROR_ENABLED: %q is not a boolean", v))
		} else {
			c.Mirror.Enabled = b
		}
	}
	if v := os.Getenv("DOCSLI_MIRROR_REMOTE"); v != "" {
		c.Mirror.Remote = v
	}
	if v := os.Getenv("DOCSLI_TOKENS"); v != "" {
		tokens, err := parseTokensEnv(v)
		if err != nil {
			errs = append(errs, err)
		} else {
			c.Tokens = tokens
		}
	}
	return errors.Join(errs...)
}

// parseTokensEnv parses DOCSLI_TOKENS: semicolon-separated entries of
// token:name:email, e.g.
//
//	dsl_abc123...:Chalvin (agent):chalvin-agent@example.com;dsl_def456...:John (agent):john-agent@example.com
func parseTokensEnv(s string) ([]TokenEntry, error) {
	var out []TokenEntry
	for _, raw := range strings.Split(s, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("DOCSLI_TOKENS: entry %q must be token:name:email", raw)
		}
		out = append(out, TokenEntry{
			Token: strings.TrimSpace(parts[0]),
			Name:  strings.TrimSpace(parts[1]),
			Email: strings.TrimSpace(parts[2]),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("DOCSLI_TOKENS is set but contains no entries")
	}
	return out, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.Mirror.Remote == "" {
		c.Mirror.Remote = "origin"
	}
}

// Validate reports every problem it finds, joined into one error.
func (c *Config) Validate() error {
	var errs []error

	if c.RepoDir == "" {
		errs = append(errs, errors.New("repo_dir is required"))
	} else if parent := filepath.Dir(c.RepoDir); parent != "." {
		if st, err := os.Stat(parent); err != nil || !st.IsDir() {
			errs = append(errs, fmt.Errorf("repo_dir parent directory %q does not exist", parent))
		}
	}

	if len(c.Tokens) == 0 {
		errs = append(errs, errors.New("at least one token is required"))
	}
	seen := make(map[string]int, len(c.Tokens))
	for i, t := range c.Tokens {
		label := fmt.Sprintf("tokens[%d]", i)
		if t.Name != "" {
			label = fmt.Sprintf("tokens[%d] (%s)", i, t.Name)
		} else {
			errs = append(errs, fmt.Errorf("%s: name is required", label))
		}
		if len(t.Token) < minTokenLen {
			errs = append(errs, fmt.Errorf("%s: token must be at least %d characters", label, minTokenLen))
		}
		if t.Token != "" {
			if j, dup := seen[t.Token]; dup {
				errs = append(errs, fmt.Errorf("%s: duplicate token, same value as tokens[%d]", label, j))
			} else {
				seen[t.Token] = i
			}
		}
		if _, err := mail.ParseAddress(t.Email); err != nil {
			errs = append(errs, fmt.Errorf("%s: invalid email %q", label, t.Email))
		}
	}

	if c.Mirror.Enabled && c.Mirror.Remote == "" {
		errs = append(errs, errors.New("mirror.remote is required when mirror.enabled is true"))
	}

	return errors.Join(errs...)
}
