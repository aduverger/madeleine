package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		origin  string
		want    string
		wantErr error
	}{
		{name: "HTTPS", origin: "https://github.com/aduverger/madeleine.git", want: "github.com/aduverger/madeleine"},
		{name: "SSH URL", origin: "ssh://git@github.com/aduverger/madeleine.git", want: "github.com/aduverger/madeleine"},
		{name: "SCP", origin: "git@github.com:aduverger/madeleine.git", want: "github.com/aduverger/madeleine"},
		{name: "mixed host case", origin: "HTTPS://GitHub.COM/Owner/Repo.git", want: "github.com/Owner/Repo"},
		{name: "trailing slash", origin: "https://github.com/Owner/Repo/", want: "github.com/Owner/Repo"},
		{name: "no git suffix", origin: "git@github.com:Owner/Repo", want: "github.com/Owner/Repo"},
		{name: "credentials removed", origin: "https://user:secret@GitHub.com/Owner/Repo.git", want: "github.com/Owner/Repo"},
		{name: "port preserved", origin: "ssh://git@Git.Example.com:2222/Owner/Repo.git", want: "git.example.com:2222/Owner/Repo"},
		{name: "local path unsupported", origin: "../another/repo.git", wantErr: errUnsupportedOrigin},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeOrigin(test.origin)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize origin: %v", err)
			}
			if got != test.want {
				t.Fatalf("origin = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveRepositoryNormalAndNoOrigin(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repository with spaces")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init")
	nested := filepath.Join(root, "nested", "directory")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	repository, err := ResolveRepository(context.Background(), nested)
	if err != nil {
		t.Fatalf("ResolveRepository: %v", err)
	}
	wantRoot := canonicalTestPath(t, root)
	if repository.WorktreeRoot != wantRoot {
		t.Fatalf("worktree root = %q, want %q", repository.WorktreeRoot, wantRoot)
	}
	if repository.GitCommonDir != filepath.Join(wantRoot, ".git") {
		t.Fatalf("common dir = %q, want %q", repository.GitCommonDir, filepath.Join(wantRoot, ".git"))
	}
	if repository.Origin != "" {
		t.Fatalf("origin = %q, want empty", repository.Origin)
	}
	if repository.ID != "" {
		t.Fatalf("repository ID = %q, want zero before persistence", repository.ID)
	}
}

func TestResolveRepositoryPreservesTrailingSpace(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repository ")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init")

	repository, err := ResolveRepository(context.Background(), root)
	if err != nil {
		t.Fatalf("ResolveRepository: %v", err)
	}
	wantRoot := canonicalTestPath(t, root)
	if repository.WorktreeRoot != wantRoot {
		t.Fatalf("worktree root = %q, want %q", repository.WorktreeRoot, wantRoot)
	}
	if repository.GitCommonDir != filepath.Join(wantRoot, ".git") {
		t.Fatalf("common dir = %q, want %q", repository.GitCommonDir, filepath.Join(wantRoot, ".git"))
	}
}

func TestResolveRepositoryOrigin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "remote", "add", "origin", "git@GitHub.COM:Owner/Repo.git")

	repository, err := ResolveRepository(context.Background(), root)
	if err != nil {
		t.Fatalf("ResolveRepository: %v", err)
	}
	if repository.Origin != "github.com/Owner/Repo" {
		t.Fatalf("origin = %q", repository.Origin)
	}
}

func TestResolveRepositoryLocalOriginsAreOptional(t *testing.T) {
	t.Parallel()

	origins := []string{
		"../local/repo.git",
		"file:///absolute/path/repo.git",
		"file://localhost/absolute/path/repo.git",
	}
	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			git(t, root, "init")
			git(t, root, "remote", "add", "origin", origin)

			repository, err := ResolveRepository(context.Background(), root)
			if err != nil {
				t.Fatalf("ResolveRepository: %v", err)
			}
			if repository.Origin != "" {
				t.Fatalf("origin = %q, want empty", repository.Origin)
			}
		})
	}
}

func TestResolveRepositoryLinkedWorktree(t *testing.T) {
	t.Parallel()

	mainRoot := t.TempDir()
	git(t, mainRoot, "init")
	if err := os.WriteFile(filepath.Join(mainRoot, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, mainRoot, "add", "README.md")
	git(t, mainRoot, "-c", "user.name=Madeleine Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")

	linkedRoot := filepath.Join(t.TempDir(), "linked worktree")
	git(t, mainRoot, "worktree", "add", "-b", "linked-test", linkedRoot)

	mainRepository, err := ResolveRepository(context.Background(), mainRoot)
	if err != nil {
		t.Fatalf("resolve main worktree: %v", err)
	}
	linkedRepository, err := ResolveRepository(context.Background(), filepath.Join(linkedRoot, "README.md"))
	if err != nil {
		t.Fatalf("resolve linked worktree: %v", err)
	}
	if mainRepository.GitCommonDir != linkedRepository.GitCommonDir {
		t.Fatalf("common dirs differ: %q != %q", mainRepository.GitCommonDir, linkedRepository.GitCommonDir)
	}
	if mainRepository.WorktreeRoot == linkedRepository.WorktreeRoot {
		t.Fatalf("worktree roots unexpectedly equal: %q", mainRepository.WorktreeRoot)
	}

	repeated, err := ResolveRepository(context.Background(), linkedRoot)
	if err != nil {
		t.Fatalf("repeat resolve: %v", err)
	}
	if repeated.WorktreeRoot != linkedRepository.WorktreeRoot || repeated.GitCommonDir != linkedRepository.GitCommonDir {
		t.Fatalf("repeat resolution changed facts: %#v != %#v", repeated, linkedRepository)
	}
}

func TestResolveRepositoryOutsideGit(t *testing.T) {
	t.Parallel()

	_, err := ResolveRepository(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNotGitRepository) {
		t.Fatalf("error = %v, want ErrNotGitRepository", err)
	}
}

func git(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(resolved)
}
