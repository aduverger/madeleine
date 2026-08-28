package madeleine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryMatchingAndAliasAttachment(t *testing.T) {
	t.Parallel()

	t.Run("worktree root", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, t.TempDir())
		defer store.Close()
		root := newTestGitRepository(t, "")

		first, err := store.ResolveRepository(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		var aliasCount int
		if err := store.db.QueryRow(
			"SELECT COUNT(*) FROM repository_aliases WHERE repository_id = ?", first.ID,
		).Scan(&aliasCount); err != nil {
			t.Fatal(err)
		}
		if aliasCount != 2 {
			t.Fatalf("no-origin alias count = %d, want 2", aliasCount)
		}
		if _, err := store.db.Exec(
			"DELETE FROM repository_aliases WHERE kind = ? AND value = ?",
			repositoryAliasCommonGitDir, first.GitCommonDir); err != nil {
			t.Fatal(err)
		}

		second, err := store.ResolveRepository(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if second.ID != first.ID {
			t.Fatalf("repository ID = %q, want %q", second.ID, first.ID)
		}
	})

	t.Run("common Git directory", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, t.TempDir())
		defer store.Close()
		mainRoot := newTestGitRepository(t, "")
		if err := os.WriteFile(filepath.Join(mainRoot, "README.md"), []byte("initial\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, mainRoot, "add", "README.md")
		git(t, mainRoot, "-c", "user.name=Madeleine Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
		linkedRoot := filepath.Join(t.TempDir(), "linked")
		git(t, mainRoot, "worktree", "add", "-b", "plan2-linked", linkedRoot)

		mainRepository, err := store.ResolveRepository(context.Background(), mainRoot)
		if err != nil {
			t.Fatal(err)
		}
		linkedRepository, err := store.ResolveRepository(context.Background(), linkedRoot)
		if err != nil {
			t.Fatal(err)
		}
		if linkedRepository.ID != mainRepository.ID {
			t.Fatalf("linked repository ID = %q, want %q", linkedRepository.ID, mainRepository.ID)
		}
		if linkedRepository.WorktreeRoot == mainRepository.WorktreeRoot {
			t.Fatal("linked repository did not return its current worktree root")
		}
	})

	t.Run("normalized origin", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, t.TempDir())
		defer store.Close()
		firstRoot := newTestGitRepository(t, "git@github.com:Owner/Project.git")
		secondRoot := newTestGitRepository(t, "https://github.com/Owner/Project.git")

		first, err := store.ResolveRepository(context.Background(), firstRoot)
		if err != nil {
			t.Fatal(err)
		}
		second, err := store.ResolveRepository(context.Background(), secondRoot)
		if err != nil {
			t.Fatal(err)
		}
		if second.ID != first.ID {
			t.Fatalf("clone repository ID = %q, want %q", second.ID, first.ID)
		}
		if second.WorktreeRoot == first.WorktreeRoot {
			t.Fatal("clone resolution did not return its current root")
		}
		var aliasCount int
		if err := store.db.QueryRow(
			"SELECT COUNT(*) FROM repository_aliases WHERE repository_id = ?", first.ID,
		).Scan(&aliasCount); err != nil {
			t.Fatal(err)
		}
		if aliasCount != 5 {
			t.Fatalf("aliases after clone attachment = %d, want 5", aliasCount)
		}
	})
}

func TestRepositoryAliasConflictDoesNotMerge(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	firstRoot := newTestGitRepository(t, "")
	secondRoot := newTestGitRepository(t, "")
	first, err := store.ResolveRepository(context.Background(), firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ResolveRepository(context.Background(), secondRoot)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.Exec(
		"DELETE FROM repository_aliases WHERE kind = ? AND value = ?",
		repositoryAliasWorktreeRoot, first.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
		UPDATE repository_aliases SET value = ?
		WHERE repository_id = ? AND kind = ?`,
		first.WorktreeRoot, second.ID, repositoryAliasWorktreeRoot); err != nil {
		t.Fatal(err)
	}

	_, err = store.ResolveRepository(context.Background(), firstRoot)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("ResolveRepository error = %v, want ErrConflict", err)
	}
	var repositoryCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM repositories").Scan(&repositoryCount); err != nil {
		t.Fatal(err)
	}
	if repositoryCount != 2 {
		t.Fatalf("repository count = %d, want 2", repositoryCount)
	}
}

func TestConcurrentRepositoryGetOrCreate(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := newTestGitRepository(t, "")
	stores := make([]*testService, 6)
	for index := range stores {
		stores[index] = openTestStore(t, home)
		defer stores[index].Close()
	}

	type result struct {
		id  RepositoryID
		err error
	}
	results := make(chan result, len(stores))
	start := make(chan struct{})
	for _, store := range stores {
		go func() {
			<-start
			repository, err := store.ResolveRepository(context.Background(), root)
			results <- result{id: repository.ID, err: err}
		}()
	}
	close(start)

	var repositoryID RepositoryID
	for range stores {
		result := <-results
		if result.err != nil {
			t.Fatalf("ResolveRepository: %v", result.err)
		}
		if repositoryID == "" {
			repositoryID = result.id
		} else if result.id != repositoryID {
			t.Fatalf("repository ID = %q, want %q", result.id, repositoryID)
		}
	}
	var count int
	if err := stores[0].db.QueryRow("SELECT COUNT(*) FROM repositories").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("repository count = %d, want 1", count)
	}
}

func TestRepositoryResolutionAcrossShortLivedProcesses(t *testing.T) {
	if os.Getenv("MADELEINE_STORE_HELPER") == "1" {
		store, err := Open(context.Background(), Options{Home: os.Getenv("MADELEINE_TEST_HOME")})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		repository, err := store.ResolveRepository(context.Background(), os.Getenv("MADELEINE_TEST_ROOT"))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("repository-id=%s", repository.ID)
		return
	}

	home := t.TempDir()
	root := newTestGitRepository(t, "")
	type result struct {
		output string
		err    error
	}
	results := make(chan result, 4)
	start := make(chan struct{})
	for range 4 {
		go func() {
			<-start
			command := exec.Command(os.Args[0], "-test.run=^TestRepositoryResolutionAcrossShortLivedProcesses$", "-test.v")
			command.Env = append(os.Environ(),
				"MADELEINE_STORE_HELPER=1",
				"MADELEINE_TEST_HOME="+home,
				"MADELEINE_TEST_ROOT="+root,
			)
			output, err := command.CombinedOutput()
			results <- result{output: string(output), err: err}
		}()
	}
	close(start)

	var repositoryID string
	for range 4 {
		result := <-results
		if result.err != nil {
			t.Fatalf("helper process: %v\n%s", result.err, result.output)
		}
		var processRepositoryID string
		for _, line := range strings.Split(result.output, "\n") {
			if _, value, found := strings.Cut(line, "repository-id="); found {
				processRepositoryID = strings.TrimSpace(value)
				break
			}
		}
		if processRepositoryID == "" {
			t.Fatalf("helper output has no repository ID:\n%s", result.output)
		}
		if repositoryID == "" {
			repositoryID = processRepositoryID
		} else if processRepositoryID != repositoryID {
			t.Fatalf("repository ID = %q, want %q", processRepositoryID, repositoryID)
		}
	}

	store := openTestStore(t, home)
	defer store.Close()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM repositories").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("repository count = %d, want 1", count)
	}
}

func TestConversationGetOrCreateAndTranscriptUpdate(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	repository, err := store.ResolveRepository(context.Background(), newTestGitRepository(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	key := ConversationKey{Harness: HarnessPi, ExternalID: "session-1"}

	conversationID, err := store.getOrCreateConversation(context.Background(), repository.ID, key, "first.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var createdAt, firstUpdatedAt string
	if err := store.db.QueryRow(`
		SELECT created_at, updated_at FROM conversations WHERE id = ?`, conversationID,
	).Scan(&createdAt, &firstUpdatedAt); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)

	reusedID, err := store.getOrCreateConversation(context.Background(), repository.ID, key, "second.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if reusedID != conversationID {
		t.Fatalf("reused conversation ID = %q, want %q", reusedID, conversationID)
	}
	var transcriptRef, secondUpdatedAt string
	if err := store.db.QueryRow(`
		SELECT transcript_ref, updated_at FROM conversations WHERE id = ?`, conversationID,
	).Scan(&transcriptRef, &secondUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if transcriptRef != "second.jsonl" {
		t.Fatalf("transcript reference = %q, want second.jsonl", transcriptRef)
	}
	firstTime, err := time.Parse(time.RFC3339Nano, firstUpdatedAt)
	if err != nil {
		t.Fatalf("parse first updated_at: %v", err)
	}
	secondTime, err := time.Parse(time.RFC3339Nano, secondUpdatedAt)
	if err != nil {
		t.Fatalf("parse second updated_at: %v", err)
	}
	if !secondTime.After(firstTime) {
		t.Fatalf("updated_at did not advance: %q then %q", firstUpdatedAt, secondUpdatedAt)
	}
	if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
		t.Fatalf("created_at is not RFC3339Nano: %v", err)
	}

	if _, err := store.getOrCreateConversation(context.Background(), repository.ID, key, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(
		"SELECT transcript_ref FROM conversations WHERE id = ?", conversationID,
	).Scan(&transcriptRef); err != nil {
		t.Fatal(err)
	}
	if transcriptRef != "second.jsonl" {
		t.Fatalf("empty update cleared transcript reference to %q", transcriptRef)
	}
}

func TestConversationRejectsEmptyKeyFields(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	keys := []ConversationKey{
		{ExternalID: "session"},
		{Harness: HarnessPi},
	}
	for _, key := range keys {
		_, err := store.getOrCreateConversation(context.Background(), "repository", key, "")
		if !errors.Is(err, ErrInvalidState) {
			t.Errorf("key %#v error = %v, want ErrInvalidState", key, err)
		}
	}
}

func TestConcurrentConversationGetOrCreate(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	stores := make([]*testService, 6)
	for index := range stores {
		stores[index] = openTestStore(t, home)
		defer stores[index].Close()
	}
	repository, err := stores[0].ResolveRepository(context.Background(), newTestGitRepository(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	key := ConversationKey{Harness: HarnessPi, ExternalID: "shared-session"}

	type result struct {
		id  ConversationID
		err error
	}
	results := make(chan result, len(stores))
	start := make(chan struct{})
	for _, store := range stores {
		go func() {
			<-start
			id, err := store.getOrCreateConversation(context.Background(), repository.ID, key, "session.jsonl")
			results <- result{id: id, err: err}
		}()
	}
	close(start)

	var conversationID ConversationID
	for range stores {
		result := <-results
		if result.err != nil {
			t.Fatalf("getOrCreateConversation: %v", result.err)
		}
		if conversationID == "" {
			conversationID = result.id
		} else if result.id != conversationID {
			t.Fatalf("conversation ID = %q, want %q", result.id, conversationID)
		}
	}
	var count int
	if err := stores[0].db.QueryRow("SELECT COUNT(*) FROM conversations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("conversation count = %d, want 1", count)
	}
}
