// Package config loads and validates the docsli configuration file.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"

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

// Load reads, defaults, and validates the config file at path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
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
