package gitcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunCapturesStreamsSeparately(t *testing.T) {
	t.Parallel()

	executable := writeExecutable(t, `#!/bin/sh
printf 'stdout:%s' "$2"
printf 'warning only' >&2
`)
	argument := `$(touch should-not-exist)`
	output, err := Run(context.Background(), executable, t.TempDir(), []string{"status", argument}, time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := string(output), "stdout:"+argument; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunReturnsSanitizedBoundedCommandError(t *testing.T) {
	t.Parallel()

	executable := writeExecutable(t, `#!/bin/sh
printf 'https://token:secret@example.com/repo.git?access_token=query-secret password=plain-secret\033[31m\n' >&2
i=0
while [ "$i" -lt 5000 ]; do printf x >&2; i=$((i + 1)); done
exit 23
`)
	_, err := Run(context.Background(), executable, t.TempDir(), []string{"fetch"}, 5*time.Second)
	if err == nil {
		t.Fatal("Run error = nil, want command error")
	}
	var commandError *CommandError
	if !errors.As(err, &commandError) {
		t.Fatalf("error type = %T, want *CommandError", err)
	}
	if commandError.ExitStatus != 23 {
		t.Fatalf("exit status = %d, want 23", commandError.ExitStatus)
	}
	for _, secret := range []string{"token:secret", "query-secret", "plain-secret"} {
		if strings.Contains(commandError.Stderr, secret) {
			t.Fatalf("stderr contains credential %q: %q", secret, commandError.Stderr)
		}
	}
	if strings.ContainsRune(commandError.Stderr, '\x1b') || strings.ContainsRune(commandError.Stderr, '\n') {
		t.Fatalf("stderr contains unsanitized control characters: %q", commandError.Stderr)
	}
	if len(commandError.Stderr) > MaxStderrBytes+32 {
		t.Fatalf("stderr length = %d, want bounded output", len(commandError.Stderr))
	}
	if !strings.HasSuffix(commandError.Stderr, "…") {
		t.Fatalf("stderr = %q, want truncation marker", commandError.Stderr)
	}
}

func TestRunUsesWorkingDirectoryInsteadOfInheritedGitOverrides(t *testing.T) {
	executable := writeExecutable(t, `#!/bin/sh
printf '%s|%s|%s' "${GIT_DIR-unset}" "${GIT_WORK_TREE-unset}" "${GIT_INDEX_FILE-unset}"
`)
	t.Setenv("GIT_DIR", "/wrong/git-dir")
	t.Setenv("GIT_WORK_TREE", "/wrong/worktree")
	t.Setenv("GIT_INDEX_FILE", "/wrong/index")

	output, err := Run(context.Background(), executable, t.TempDir(), []string{"status"}, time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := string(output), "unset|unset|unset"; got != want {
		t.Fatalf("repository override environment = %q, want %q", got, want)
	}
}

func TestRunAppliesTimeout(t *testing.T) {
	t.Parallel()

	executable := writeExecutable(t, `#!/bin/sh
while :; do :; done
`)
	started := time.Now()
	_, err := Run(context.Background(), executable, t.TempDir(), []string{"rev-parse"}, 30*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestRunTimeoutClosesInheritedPipes(t *testing.T) {
	t.Parallel()

	executable := writeExecutable(t, `#!/bin/sh
(sleep 2) &
while :; do :; done
`)
	started := time.Now()
	_, err := Run(context.Background(), executable, t.TempDir(), []string{"status"}, 30*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout with inherited pipes took %s", elapsed)
	}
}

func writeExecutable(t *testing.T, contents string) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "fake-git")
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	return path
}
