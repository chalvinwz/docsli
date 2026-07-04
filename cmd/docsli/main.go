// Command docsli serves a git-backed document store to AI agents over MCP.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/chalvinwz/docsli/internal/config"
	"github.com/chalvinwz/docsli/internal/gitstore"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	configPath := flag.String("config", "config.yml", "path to the YAML config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("docsli", version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(*configPath, logger); err != nil {
		logger.Error("fatal", "err", err.Error())
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	store, err := gitstore.Open(cfg.RepoDir, logger)
	if err != nil {
		return err
	}
	logger.Info("repository ready", "dir", store.Dir(), "mirror", cfg.Mirror.Enabled)

	// MCP server over streamable HTTP lands in a later milestone.
	logger.Info("docsli configured", "listen", cfg.Listen, "version", version)
	return nil
}
