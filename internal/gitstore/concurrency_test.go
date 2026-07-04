package gitstore

import (
	"fmt"
	"sync"
	"testing"
)

// TestConcurrentWrites exercises the write lock: 10 goroutines create 10
// different docs at once; every write must land as exactly one commit and the
// worktree must end clean.
func TestConcurrentWrites(t *testing.T) {
	s := newTestStore(t)

	const writers = 10
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := fmt.Sprintf("docs/concurrent-%d.md", i)
			errs <- s.Create(path, fmt.Sprintf("# Doc %d\n", i), "concurrency test writes ten docs at once", john)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if n := commitCount(t, s); n != writers+1 {
		t.Errorf("commit count = %d, want %d (bootstrap + %d writes)", n, writers+1, writers)
	}
	if out := gitOut(t, s, "status", "--porcelain"); out != "" {
		t.Errorf("worktree dirty after concurrent writes:\n%s", out)
	}
	docs, err := s.List(ListQuery{Folder: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != writers {
		t.Errorf("listed %d docs, want %d", len(docs), writers)
	}
}
