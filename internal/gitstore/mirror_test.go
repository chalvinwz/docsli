package gitstore

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// newMirroredStore wires a store to a local bare repo acting as the remote,
// so mirror behavior is tested without any network.
func newMirroredStore(t *testing.T) (*GitStore, runner) {
	t.Helper()
	s := newTestStore(t)
	bareDir := t.TempDir()
	bare := runner{dir: bareDir}
	if _, err := bare.run(context.Background(), "init", "--bare", "--initial-branch=main"); err != nil {
		t.Fatal(err)
	}
	gitOut(t, s, "remote", "add", "origin", bareDir)
	return s, bare
}

// startPusher runs p until test cleanup, and — crucially — waits for Run to
// return before cleanup proceeds: the final flush push must not race the
// TempDir removal.
func startPusher(t *testing.T, p *Pusher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func remoteHead(t *testing.T, bare runner) string {
	t.Helper()
	out, err := bare.run(context.Background(), "rev-parse", "HEAD")
	if err != nil {
		return "" // no commits on the remote yet
	}
	return out
}

func waitForSync(t *testing.T, s *GitStore, bare runner) {
	t.Helper()
	localHead := gitOut(t, s, "rev-parse", "HEAD")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if remoteHead(t, bare) == localHead {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("remote never caught up: local %s, remote %s", localHead, remoteHead(t, bare))
}

func TestPusherMirrorsCommitOnNotify(t *testing.T) {
	s, bare := newMirroredStore(t)
	p := NewPusher(s, "origin", slog.New(slog.DiscardHandler))
	s.SetOnWrite(p.Notify)
	startPusher(t, p)

	if err := s.Create("docs/mirrored.md", "# Mirrored\n", "verify commits reach the mirror", john); err != nil {
		t.Fatal(err)
	}
	waitForSync(t, s, bare)
}

func TestPusherTickerCatchesUp(t *testing.T) {
	s, bare := newMirroredStore(t)
	// Commit while no pusher is running — simulates a push that failed.
	seedDoc(t, s, "docs/late.md", "# Late\n", john, "create: docs/late.md", "commit made before pusher started")

	p := NewPusher(s, "origin", slog.New(slog.DiscardHandler))
	p.tick = 20 * time.Millisecond
	startPusher(t, p)

	// No Notify: only the ticker can pick this up.
	waitForSync(t, s, bare)
}

func TestAheadCheck(t *testing.T) {
	s, _ := newMirroredStore(t)
	p := NewPusher(s, "origin", slog.New(slog.DiscardHandler))

	if !p.aheadOfRemote() {
		t.Error("before first push the tracking ref is missing; must count as ahead")
	}
	p.push()
	if p.aheadOfRemote() {
		t.Error("after a successful push the store must not report ahead")
	}
}

func TestPushFailureDoesNotFailWrites(t *testing.T) {
	s := newTestStore(t)
	// Remote points at a path that does not exist: every push fails.
	gitOut(t, s, "remote", "add", "origin", "/nonexistent/docsli-mirror.git")
	p := NewPusher(s, "origin", slog.New(slog.DiscardHandler))
	s.SetOnWrite(p.Notify)
	startPusher(t, p)

	if err := s.Create("docs/ok.md", "# OK\n", "write must succeed even when mirror is down", john); err != nil {
		t.Fatalf("write failed because of mirror: %v", err)
	}
	if n := commitCount(t, s); n != 2 {
		t.Errorf("commit count = %d, want 2", n)
	}
}
