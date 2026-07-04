package gitstore

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

const (
	pushTimeout      = 30 * time.Second
	defaultPushEvery = 60 * time.Second
)

// Pusher mirrors the repository to a git remote, strictly one-way: the server
// pushes, nobody else ever does. A failed push never fails the write that
// triggered it — it is logged and retried by the next notification or tick.
// Credentials come from the process environment (SSH key, credential helper),
// never from configuration.
type Pusher struct {
	r      runner
	remote string
	// notify has capacity 1: a full buffer means a push is already pending,
	// and that push will include this commit too.
	notify chan struct{}
	pushMu sync.Mutex
	log    *slog.Logger
	tick   time.Duration
}

// NewPusher creates a pusher for the store's repository. The remote must
// already be configured in the repo (git remote add ...).
func NewPusher(s *GitStore, remote string, logger *slog.Logger) *Pusher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pusher{
		r:      s.r,
		remote: remote,
		notify: make(chan struct{}, 1),
		log:    logger,
		tick:   defaultPushEvery,
	}
}

// Notify wakes the pusher after a commit. Non-blocking, safe from any
// goroutine; wire it via GitStore.SetOnWrite.
func (p *Pusher) Notify() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

// Run pushes on demand and on a ticker — the ticker is the catch-all that
// retries pushes which previously failed — until ctx is canceled, then makes
// one final flush attempt so a graceful shutdown leaves the mirror current.
func (p *Pusher) Run(ctx context.Context) {
	ticker := time.NewTicker(p.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if p.aheadOfRemote() {
				p.push()
			}
			return
		case <-p.notify:
			p.push()
		case <-ticker.C:
			if p.aheadOfRemote() {
				p.push()
			}
		}
	}
}

func (p *Pusher) push() {
	p.pushMu.Lock()
	defer p.pushMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	if _, err := p.r.run(ctx, "push", p.remote, "HEAD"); err != nil {
		p.log.Warn("mirror push failed; will retry", "remote", p.remote, "err", err.Error())
		return
	}
	p.log.Debug("mirror push ok", "remote", p.remote)
}

// aheadOfRemote reports whether local HEAD has commits the remote-tracking
// ref lacks. Any error — including the tracking ref not existing before the
// very first push — counts as ahead, so pushing errs on the safe side.
func (p *Pusher) aheadOfRemote() bool {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	branch, err := p.r.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return true
	}
	out, err := p.r.run(ctx, "rev-list", "--count", p.remote+"/"+branch+"..HEAD")
	if err != nil {
		return true
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return true
	}
	return n > 0
}
