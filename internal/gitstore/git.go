package gitstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runner executes git commands in a fixed repository directory. Arguments are
// always passed as an argv slice — never through a shell — so paths with
// spaces or metacharacters are safe by construction.
type runner struct {
	dir string
}

func (r runner) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// run executes git and returns stdout with the trailing newline removed.
// Any non-zero exit is an error carrying git's stderr.
func (r runner) run(ctx context.Context, args ...string) (string, error) {
	out, err := r.runRaw(ctx, args...)
	return strings.TrimRight(out, "\n"), err
}

// runRaw is run without output trimming, for commands whose exact byte output
// matters (git show of file content).
func (r runner) runRaw(ctx context.Context, args ...string) (string, error) {
	var stdout, stderr strings.Builder
	cmd := r.command(ctx, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// runExit executes git and returns stdout plus the exit code. Exit codes are
// meaningful for commands like git grep (1 = no matches) and cat-file -e
// (1 = object missing); only failures to run at all are errors.
func (r runner) runExit(ctx context.Context, args ...string) (string, int, error) {
	var stdout, stderr strings.Builder
	cmd := r.command(ctx, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode(), nil
	}
	return "", -1, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
}
