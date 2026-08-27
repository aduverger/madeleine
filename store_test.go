package madeleine

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenMigratesAndReopensStore(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "private", "madeleine")
	store := openTestStore(t, home)

	if _, err := os.Stat(filepath.Join(home, storeDatabaseName)); err != nil {
		t.Fatalf("database file: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(home)
		if err != nil {
			t.Fatal(err)
		}
		if permissions := info.Mode().Perm(); permissions != 0o700 {
			t.Fatalf("home permissions = %o, want 700", permissions)
		}
	}

	var appliedAt string
	if err := store.db.QueryRow("SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&appliedAt); err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store again: %v", err)
	}

	reopened := openTestStore(t, home)
	var reappliedAt string
	if err := reopened.db.QueryRow("SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&reappliedAt); err != nil {
		t.Fatalf("read reopened migration: %v", err)
	}
	if reappliedAt != appliedAt {
		t.Fatalf("migration timestamp changed from %q to %q", appliedAt, reappliedAt)
	}

	for _, table := range []string{"repositories", "repository_aliases", "conversations"} {
		if !databaseObjectExists(t, reopened.db, "table", table) {
			t.Errorf("table %q does not exist", table)
		}
	}
	for _, excluded := range []string{"captures", "episodes", "transcripts"} {
		if databaseObjectExists(t, reopened.db, "table", excluded) {
			t.Errorf("excluded table %q exists", excluded)
		}
	}

	if err := reopened.Close(); err != nil {
		t.Fatalf("close before deletion: %v", err)
	}
	if err := os.Remove(filepath.Join(home, storeDatabaseName)); err != nil {
		t.Fatalf("delete database: %v", err)
	}
	fresh := openTestStore(t, home)
	defer fresh.Close()
	var freshMigrationCount int
	if err := fresh.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&freshMigrationCount); err != nil {
		t.Fatal(err)
	}
	if freshMigrationCount != 1 {
		t.Fatalf("fresh migration count = %d, want 1", freshMigrationCount)
	}
}

func TestOpenDoesNotFallBackWhenHomeCreationFails(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(home, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), Options{Home: home})
	if err == nil || !strings.Contains(err.Error(), "create store home") {
		t.Fatalf("Open error = %v, want create store home error", err)
	}
}

func TestPlatformStoreHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goos     string
		xdg      string
		userHome string
		want     string
		wantErr  bool
	}{
		{
			name: "macOS", goos: "darwin", xdg: "/ignored", userHome: "/Users/alex",
			want: filepath.Join("/Users/alex", "Library", "Application Support", "madeleine"),
		},
		{
			name: "Linux XDG", goos: "linux", xdg: "/data", userHome: "/home/alex",
			want: filepath.Join("/data", "madeleine"),
		},
		{
			name: "Linux fallback", goos: "linux", userHome: "/home/alex",
			want: filepath.Join("/home/alex", ".local", "share", "madeleine"),
		},
		{name: "unsupported", goos: "windows", userHome: `C:\Users\alex`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := platformStoreHome(test.goos, test.xdg, test.userHome)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("home = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEveryConnectionUsesRequiredSettings(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()

	connections := make([]*sql.Conn, maxOpenConnections)
	for index := range connections {
		connection, err := store.db.Conn(context.Background())
		if err != nil {
			t.Fatalf("connection %d: %v", index, err)
		}
		connections[index] = connection
		defer connection.Close()
	}
	for index, connection := range connections {
		if err := verifySingleConnection(context.Background(), connection); err != nil {
			t.Errorf("connection %d: %v", index, err)
		}
	}
}

func TestConcurrentOpenAppliesFirstMigrationOnce(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	start := make(chan struct{})
	errorsByStore := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			store, err := Open(context.Background(), Options{Home: home})
			if err == nil {
				err = store.Close()
			}
			errorsByStore <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsByStore; err != nil {
			t.Fatalf("concurrent Open: %v", err)
		}
	}

	store := openTestStore(t, home)
	defer store.Close()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration rows = %d, want 1", count)
	}
}

func TestFailingMigrationRollsBackOnlyThatMigration(t *testing.T) {
	t.Parallel()

	db, err := openSQLite(filepath.Join(t.TempDir(), "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	migrations := fstest.MapFS{
		"migrations/001_stable.sql": {Data: []byte("CREATE TABLE stable (id INTEGER PRIMARY KEY);")},
		"migrations/002_fails.sql": {Data: []byte(`
			CREATE TABLE rolled_back (id INTEGER PRIMARY KEY);
			INSERT INTO table_that_does_not_exist(id) VALUES (1);
		`)},
	}
	err = migrateStore(context.Background(), db, migrations)
	if err == nil || !strings.Contains(err.Error(), "002_fails.sql") {
		t.Fatalf("migration error = %v, want failing migration context", err)
	}
	if !databaseObjectExists(t, db, "table", "stable") {
		t.Fatal("successful earlier migration was rolled back")
	}
	if databaseObjectExists(t, db, "table", "rolled_back") {
		t.Fatal("failed migration left its table behind")
	}
	var versions []int
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	if len(versions) != 1 || versions[0] != 1 {
		t.Fatalf("applied versions = %v, want [1]", versions)
	}
}

func TestOpenRejectsFutureMigrationVersion(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	store := openTestStore(t, home)
	if _, err := store.db.Exec(
		"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
		2, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), Options{Home: home})
	if err == nil || !strings.Contains(err.Error(), "newer than binary") {
		t.Fatalf("Open error = %v, want future version rejection", err)
	}
}

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
	stores := make([]*Store, 6)
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
	stores := make([]*Store, 6)
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

func newTestGitRepository(t *testing.T, origin string) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init")
	if origin != "" {
		git(t, root, "remote", "add", "origin", origin)
	}
	return root
}

func openTestStore(t *testing.T, home string) *Store {
	t.Helper()
	store, err := Open(context.Background(), Options{Home: home})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func verifySingleConnection(ctx context.Context, connection *sql.Conn) error {
	var journalMode string
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return err
	}
	if !strings.EqualFold(journalMode, "wal") {
		return errors.New("journal mode is not WAL")
	}
	var foreignKeys, busyTimeout int
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return err
	}
	if foreignKeys != 1 || busyTimeout != 5000 {
		return errors.New("connection pragmas are not configured")
	}
	return nil
}

func databaseObjectExists(t *testing.T, db *sql.DB, objectType, name string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?",
		objectType, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}
