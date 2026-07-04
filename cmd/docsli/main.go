// Command docsli serves a git-backed document store to AI agents over MCP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalvinwz/docsli/internal/auth"
	"github.com/chalvinwz/docsli/internal/config"
	"github.com/chalvinwz/docsli/internal/gitstore"
	"github.com/chalvinwz/docsli/internal/mcpserver"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Mirror pushes run in the background; on shutdown Run makes a final
	// flush attempt before the WaitGroup releases.
	var pusherWG sync.WaitGroup
	if cfg.Mirror.Enabled {
		pusher := gitstore.NewPusher(store, cfg.Mirror.Remote, logger)
		store.SetOnWrite(pusher.Notify)
		pusherWG.Add(1)
		go func() {
			defer pusherWG.Done()
			pusher.Run(ctx)
		}()
		logger.Info("mirror push enabled", "remote", cfg.Mirror.Remote)
	}

	srv := mcpserver.New(store, logger, version)
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkauth.RequireBearerToken(auth.Verifier(cfg.Tokens), nil)(handler))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, "ok")
	})

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("docsli listening", "addr", cfg.Listen, "endpoint", "/mcp", "version", version)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		pusherWG.Wait()
		return nil
	}
}
