package madeleine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestGitReconciliation(t *testing.T) {
	t.Run("clean tracked file modified by shell", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"tracked.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "tracked-shell")
		writeRepositoryFile(t, root, "tracked.txt", "modified\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "tracked.txt")
		var source string
		if err := store.db.QueryRow(
			"SELECT source FROM capture_paths WHERE capture_id = ? AND path = ?",
			capture.ID, "tracked.txt").Scan(&source); err != nil {
			t.Fatal(err)
		}
		if source != "git" {
			t.Fatalf("path source = %q, want git", source)
		}
	})

	t.Run("untracked and deleted files", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"deleted.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "untracked-deleted")
		writeRepositoryFile(t, root, "new.txt", "new\n")
		if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
			t.Fatal(err)
		}

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "deleted.txt", "new.txt")
	})

	t.Run("staged and staged then worktree changes", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{
			"staged.txt": "initial\n",
			"mixed.txt":  "initial\n",
		})
		capture := startTestCapture(t, store, root, "staged")
		writeRepositoryFile(t, root, "staged.txt", "staged\n")
		git(t, root, "add", "staged.txt")
		writeRepositoryFile(t, root, "mixed.txt", "staged\n")
		git(t, root, "add", "mixed.txt")
		writeRepositoryFile(t, root, "mixed.txt", "worktree\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "mixed.txt", "staged.txt")
	})

	t.Run("dirty start modified again", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"dirty.txt": "initial\n"})
		writeRepositoryFile(t, root, "dirty.txt", "dirty before\n")
		capture := startTestCapture(t, store, root, "dirty-modified")
		writeRepositoryFile(t, root, "dirty.txt", "dirty after\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "dirty.txt")
	})

	t.Run("dirty start restored to HEAD", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"dirty.txt": "initial\n"})
		writeRepositoryFile(t, root, "dirty.txt", "dirty before\n")
		capture := startTestCapture(t, store, root, "dirty-restored")
		writeRepositoryFile(t, root, "dirty.txt", "initial\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "dirty.txt")
	})

	t.Run("unchanged dirty start is absent", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"dirty.txt": "initial\n"})
		writeRepositoryFile(t, root, "dirty.txt", "dirty before\n")
		capture := startTestCapture(t, store, root, "dirty-unchanged")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft)
		assertBaselineRows(t, store, capture.ID, 0)
	})

	t.Run("commits on a changed branch", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"base.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "commits")
		git(t, root, "checkout", "-b", "during-capture")
		writeRepositoryFile(t, root, "first.txt", "first\n")
		commitAllRepositoryFiles(t, root, "first")
		writeRepositoryFile(t, root, "second.txt", "second\n")
		commitAllRepositoryFiles(t, root, "second")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "first.txt", "second.txt")
	})

	t.Run("unborn repository with untracked file", func(t *testing.T) {
		store := openTestStore(t, t.TempDir())
		t.Cleanup(func() { _ = store.Close() })
		root := newTestGitRepository(t, "")
		capture := startTestCapture(t, store, root, "unborn")
		writeRepositoryFile(t, root, "first.txt", "first\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "first.txt")
	})

	t.Run("first commit from unborn repository", func(t *testing.T) {
		store := openTestStore(t, t.TempDir())
		t.Cleanup(func() { _ = store.Close() })
		root := newTestGitRepository(t, "")
		capture := startTestCapture(t, store, root, "first-commit")
		writeRepositoryFile(t, root, "first.txt", "first\n")
		writeRepositoryFile(t, root, "nested/second.txt", "second\n")
		commitAllRepositoryFiles(t, root, "first")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "first.txt", "nested/second.txt")
	})

	t.Run("populated HEAD to clean unborn branch", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{
			"first.txt":         "first\n",
			"nested/second.txt": "second\n",
		})
		capture := startTestCapture(t, store, root, "orphan")
		git(t, root, "checkout", "--orphan", "empty-history")
		git(t, root, "rm", "-rf", ".")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "first.txt", "nested/second.txt")
	})

	t.Run("rename is deletion and addition", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"old.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "rename")
		if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
			t.Fatal(err)
		}

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "new.txt", "old.txt")
	})

	t.Run("tool provenance wins Git overlap", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"both.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "tool-overlap")
		writeRepositoryFile(t, root, "both.txt", "modified\n")
		if err := store.RecordWrite(context.Background(), RecordWriteRequest{
			CaptureID: capture.ID, Path: "both.txt",
		}); err != nil {
			t.Fatal(err)
		}

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "both.txt")
		var source string
		if err := store.db.QueryRow(
			"SELECT source FROM capture_paths WHERE capture_id = ? AND path = ?",
			capture.ID, "both.txt").Scan(&source); err != nil {
			t.Fatal(err)
		}
		if source != "tool" {
			t.Fatalf("path source = %q, want tool", source)
		}
	})

	t.Run("structured write survives full revert", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"reverted.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "tool-reverted")
		writeRepositoryFile(t, root, "reverted.txt", "modified\n")
		if err := store.RecordWrite(context.Background(), RecordWriteRequest{
			CaptureID: capture.ID, Path: "reverted.txt",
		}); err != nil {
			t.Fatal(err)
		}
		writeRepositoryFile(t, root, "reverted.txt", "initial\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, "reverted.txt")
	})

	t.Run("shell-only full revert is absent", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"reverted.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "shell-reverted")
		writeRepositoryFile(t, root, "reverted.txt", "modified\n")
		writeRepositoryFile(t, root, "reverted.txt", "initial\n")

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft)
	})

	t.Run("special filenames", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"base.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "special-paths")
		paths := []string{"space name.txt", "line\nbreak.txt", "café.txt", "-leading.txt"}
		for _, path := range paths {
			writeRepositoryFile(t, root, path, "new\n")
		}

		draft := sealWithoutMutatingGit(t, store, root, capture.ID)
		assertDraftPaths(t, draft, paths...)
	})
}

func TestStartCaptureDoesNotMutateGit(t *testing.T) {
	store, root := newGitReconcileTest(t, map[string]string{"tracked.txt": "initial\n"})
	writeRepositoryFile(t, root, "tracked.txt", "dirty\n")
	before := readObservableGitState(t, root)
	startTestCapture(t, store, root, "non-mutating-start")
	after := readObservableGitState(t, root)
	assertObservableGitStateEqual(t, before, after)
}

func TestRepeatedSealDoesNotRerunGit(t *testing.T) {
	store, root := newGitReconcileTest(t, map[string]string{"tracked.txt": "initial\n"})
	capture := startTestCapture(t, store, root, "repeat-seal")
	writeRepositoryFile(t, root, "tracked.txt", "modified\n")
	first := sealWithoutMutatingGit(t, store, root, capture.ID)

	gitDirectory := filepath.Join(root, ".git")
	hiddenGitDirectory := filepath.Join(root, ".git-hidden")
	if err := os.Rename(gitDirectory, hiddenGitDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(hiddenGitDirectory, gitDirectory) })
	second, err := store.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: capture.ID, EndCursor: "different-end",
	})
	if err != nil {
		t.Fatalf("repeated SealCapture reran Git: %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("repeated draft = %#v, want %#v", second, first)
	}
}

func TestGitFailureAndPersistenceFailureLeaveCaptureOpen(t *testing.T) {
	t.Run("Git failure is recoverable", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"base.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "git-failure")
		writeRepositoryFile(t, root, "new.txt", "new\n")
		gitDirectory := filepath.Join(root, ".git")
		hiddenGitDirectory := filepath.Join(root, ".git-hidden")
		if err := os.Rename(gitDirectory, hiddenGitDirectory); err != nil {
			t.Fatal(err)
		}
		_, err := store.SealCapture(context.Background(), SealCaptureRequest{
			CaptureID: capture.ID, EndCursor: "end",
		})
		if err == nil {
			t.Fatal("SealCapture error = nil, want Git failure")
		}
		got, getErr := store.GetCapture(context.Background(), capture.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if got.Status != CaptureStatusOpen || got.EndCursor != "" || got.EndedAt != nil {
			t.Fatalf("Capture after Git failure = %#v", got)
		}
		if err := os.Rename(hiddenGitDirectory, gitDirectory); err != nil {
			t.Fatal(err)
		}
		assertDraftPaths(t, sealWithoutMutatingGit(t, store, root, capture.ID), "new.txt")
	})

	t.Run("path persistence failure rolls back", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"base.txt": "initial\n"})
		capture := startTestCapture(t, store, root, "persistence-failure")
		writeRepositoryFile(t, root, "new.txt", "new\n")
		if _, err := store.db.Exec(`
			CREATE TRIGGER reject_git_path BEFORE INSERT ON capture_paths
			WHEN NEW.source = 'git' BEGIN SELECT RAISE(ABORT, 'reject Git path'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SealCapture(context.Background(), SealCaptureRequest{
			CaptureID: capture.ID, EndCursor: "end",
		}); err == nil {
			t.Fatal("SealCapture error = nil, want persistence failure")
		}
		got, err := store.GetCapture(context.Background(), capture.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != CaptureStatusOpen || got.EndCursor != "" || got.EndedAt != nil {
			t.Fatalf("Capture after persistence failure = %#v", got)
		}
		if _, err := store.db.Exec("DROP TRIGGER reject_git_path"); err != nil {
			t.Fatal(err)
		}
		assertDraftPaths(t, sealWithoutMutatingGit(t, store, root, capture.ID), "new.txt")
	})
}

func TestStartCapturePersistsBaselineAtomically(t *testing.T) {
	store, root := newGitReconcileTest(t, map[string]string{"dirty.txt": "initial\n"})
	writeRepositoryFile(t, root, "dirty.txt", "dirty\n")
	if _, err := store.db.Exec(`
		CREATE TRIGGER reject_git_baseline BEFORE INSERT ON capture_git_baseline_paths
		BEGIN SELECT RAISE(ABORT, 'reject Git baseline'); END`); err != nil {
		t.Fatal(err)
	}

	_, err := store.StartCapture(context.Background(), StartCaptureRequest{
		RepositoryRoot:  root,
		ConversationKey: ConversationKey{Harness: HarnessPi, ExternalID: "baseline-failure"},
		StartCursor:     "start",
	})
	if err == nil {
		t.Fatal("StartCapture error = nil, want baseline persistence failure")
	}
	var captures int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM captures").Scan(&captures); err != nil {
		t.Fatal(err)
	}
	if captures != 0 {
		t.Fatalf("Capture count = %d, want 0", captures)
	}
}

func TestTerminalCapturesDeleteGitBaseline(t *testing.T) {
	t.Run("abandoned", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"dirty.txt": "initial\n"})
		writeRepositoryFile(t, root, "dirty.txt", "dirty\n")
		capture := startTestCapture(t, store, root, "abandoned-baseline")
		assertBaselineRows(t, store, capture.ID, 1)
		if err := store.AbandonCapture(context.Background(), capture.ID); err != nil {
			t.Fatal(err)
		}
		assertBaselineRows(t, store, capture.ID, 0)
	})

	t.Run("published", func(t *testing.T) {
		store, root := newGitReconcileTest(t, map[string]string{"dirty.txt": "initial\n"})
		writeRepositoryFile(t, root, "dirty.txt", "dirty before\n")
		capture := startTestCapture(t, store, root, "published-baseline")
		writeRepositoryFile(t, root, "dirty.txt", "dirty after\n")
		if _, err := store.SealCapture(context.Background(), SealCaptureRequest{
			CaptureID: capture.ID, EndCursor: "end",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PublishEpisode(context.Background(), PublishEpisodeRequest{
			CaptureID: capture.ID, L1: "Changed the dirty file.", L2: "The dirty file changed during the Capture.",
		}); err != nil {
			t.Fatal(err)
		}
		assertBaselineRows(t, store, capture.ID, 0)
	})
}

type observableGitState struct {
	head      []byte
	status    []byte
	index     []byte
	indexInfo os.FileInfo
}

func newGitReconcileTest(t *testing.T, files map[string]string) (*testService, string) {
	t.Helper()
	store := openTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	root := newTestGitRepository(t, "")
	for path, contents := range files {
		writeRepositoryFile(t, root, path, contents)
	}
	commitAllRepositoryFiles(t, root, "initial")
	return store, root
}

func writeRepositoryFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAllRepositoryFiles(t *testing.T, root, message string) {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "-c", "user.name=Madeleine Test", "-c", "user.email=test@example.com", "commit", "-m", message)
}

func sealWithoutMutatingGit(t *testing.T, store *testService, root string, captureID CaptureID) FinalizationDraft {
	t.Helper()
	before := readObservableGitState(t, root)
	draft, err := store.SealCapture(context.Background(), SealCaptureRequest{
		CaptureID: captureID, EndCursor: "end",
	})
	if err != nil {
		t.Fatalf("SealCapture: %v", err)
	}
	after := readObservableGitState(t, root)
	assertObservableGitStateEqual(t, before, after)
	return draft
}

func assertObservableGitStateEqual(t *testing.T, before, after observableGitState) {
	t.Helper()
	if !bytes.Equal(after.head, before.head) || !bytes.Equal(after.status, before.status) ||
		!bytes.Equal(after.index, before.index) {
		t.Fatalf("Git state changed during observation\nbefore: head=%q status=%q\nafter: head=%q status=%q",
			before.head, before.status, after.head, after.status)
	}
	if before.indexInfo != nil && after.indexInfo != nil {
		if !os.SameFile(before.indexInfo, after.indexInfo) ||
			!before.indexInfo.ModTime().Equal(after.indexInfo.ModTime()) {
			t.Fatal("Git index was replaced or rewritten during observation")
		}
	}
}

func readObservableGitState(t *testing.T, root string) observableGitState {
	t.Helper()
	head, err := runTestGit(root, "--no-optional-locks", "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			t.Fatalf("read Git HEAD: %v", err)
		}
		head = nil
	}
	status, err := runTestGit(root, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		t.Fatalf("read Git status: %v", err)
	}
	indexPath := filepath.Join(root, ".git", "index")
	index, err := os.ReadFile(indexPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	indexInfo, err := os.Stat(indexPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return observableGitState{head: head, status: status, index: index, indexInfo: indexInfo}
}

func runTestGit(root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	return command.Output()
}

func assertDraftPaths(t *testing.T, draft FinalizationDraft, want ...string) {
	t.Helper()
	sort.Strings(want)
	if len(draft.Paths) != len(want) {
		t.Fatalf("draft paths = %q, want %q", draft.Paths, want)
	}
	for index := range want {
		if draft.Paths[index] != want[index] {
			t.Fatalf("draft paths = %q, want %q", draft.Paths, want)
		}
	}
	if len(want) == 0 {
		if !draft.Empty || draft.Status != CaptureStatusAbandoned {
			t.Fatalf("empty draft = %#v", draft)
		}
		return
	}
	if draft.Empty || draft.Status != CaptureStatusPendingSummary {
		t.Fatalf("non-empty draft = %#v", draft)
	}
}

func assertBaselineRows(t *testing.T, store *testService, captureID CaptureID, want int) {
	t.Helper()
	var got int
	if err := store.db.QueryRow(
		"SELECT COUNT(*) FROM capture_git_baseline_paths WHERE capture_id = ?", captureID,
	).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("baseline row count = %d, want %d", got, want)
	}
}
