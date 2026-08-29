package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
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

	if _, err := os.Stat(filepath.Join(home, databaseName)); err != nil {
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

	for _, table := range []string{
		"repositories", "repository_aliases", "conversations", "captures", "capture_paths",
		"capture_git_baseline_paths", "episodes", "episode_files",
	} {
		if !databaseObjectExists(t, reopened.db, "table", table) {
			t.Errorf("table %q does not exist", table)
		}
	}
	if databaseObjectExists(t, reopened.db, "table", "transcripts") {
		t.Error("excluded table \"transcripts\" exists")
	}

	if err := reopened.Close(); err != nil {
		t.Fatalf("close before deletion: %v", err)
	}
	if err := os.Remove(filepath.Join(home, databaseName)); err != nil {
		t.Fatalf("delete database: %v", err)
	}
	fresh := openTestStore(t, home)
	defer fresh.Close()
	var freshMigrationCount int
	if err := fresh.db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&freshMigrationCount); err != nil {
		t.Fatal(err)
	}
	if freshMigrationCount != 4 {
		t.Fatalf("fresh migration count = %d, want 4", freshMigrationCount)
	}
}

func TestSchemaIndexes(t *testing.T) {
	t.Parallel()

	database := openTestStore(t, t.TempDir())
	defer database.Close()
	indexes := []string{
		"captures_one_open_per_conversation_idx",
		"captures_repository_status_started_idx",
		"captures_conversation_status_started_idx",
		"episodes_repository_ended_id_idx",
		"episode_files_repository_path_episode_idx",
	}
	for _, index := range indexes {
		if !databaseObjectExists(t, database.db, "index", index) {
			t.Errorf("index %q does not exist", index)
		}
	}
}

func TestGitBaselineSchema(t *testing.T) {
	t.Parallel()

	store := openTestStore(t, t.TempDir())
	defer store.Close()
	for _, column := range []string{"git_start_head", "git_start_head_exists"} {
		var count int
		if err := store.db.QueryRow(`
			SELECT COUNT(*) FROM pragma_table_info('captures') WHERE name = ?`, column,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("captures column %q does not exist", column)
		}
	}
}

func TestOpenDoesNotFallBackWhenHomeCreationFails(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(home, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), home)
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
			got, err := platformHome(test.goos, test.xdg, test.userHome)
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
			store, err := Open(context.Background(), home)
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
	err = migrate(context.Background(), db, migrations)
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
		5, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), home)
	if err == nil || !strings.Contains(err.Error(), "newer than binary") {
		t.Fatalf("Open error = %v, want future version rejection", err)
	}
}

func openTestStore(t *testing.T, home string) *DB {
	t.Helper()
	database, err := Open(context.Background(), home)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return database
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
