package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	storeDatabaseName  = "madeleine.db"
	maxOpenConnections = 4
)

type Store struct {
	db        *sql.DB
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, options Options) (*Store, error) {
	home, err := resolveStoreHome(options.Home)
	if err != nil {
		return nil, wrapError("resolve store home", options.Home, err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, wrapError("create store home", home, err)
	}

	db, err := openSQLite(filepath.Join(home, storeDatabaseName))
	if err != nil {
		return nil, wrapError("open store", home, err)
	}
	if err := enableWAL(ctx, db); err != nil {
		_ = db.Close()
		return nil, wrapError("enable store WAL", home, err)
	}
	if err := verifyConnectionSettings(ctx, db); err != nil {
		_ = db.Close()
		return nil, wrapError("verify store settings", home, err)
	}
	if err := migrateStore(ctx, db, embeddedMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.db.Close()
	})
	return s.closeErr
}

func resolveStoreHome(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}

	xdgDataHome := ""
	if runtime.GOOS == "linux" {
		xdgDataHome = os.Getenv("XDG_DATA_HOME")
	}
	userHome, err := os.UserHomeDir()
	if err != nil && xdgDataHome == "" {
		return "", err
	}
	return platformStoreHome(runtime.GOOS, xdgDataHome, userHome)
}

func platformStoreHome(goos, xdgDataHome, userHome string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(userHome, "Library", "Application Support", "madeleine"), nil
	case "linux":
		if xdgDataHome != "" {
			return filepath.Join(xdgDataHome, "madeleine"), nil
		}
		return filepath.Join(userHome, ".local", "share", "madeleine"), nil
	default:
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}
}

func openSQLite(databasePath string) (*sql.DB, error) {
	dsn := url.URL{Scheme: "file", Path: databasePath}
	query := dsn.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(ON)")
	query.Set("_txlock", "immediate")
	dsn.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxOpenConnections)
	return db, nil
}

func enableWAL(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for {
		var journalMode string
		err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode)
		if err == nil {
			if !strings.EqualFold(journalMode, "wal") {
				return fmt.Errorf("journal mode is %q, want WAL", journalMode)
			}
			return nil
		}
		if !isSQLiteContention(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func isSQLiteContention(err error) bool {
	var sqliteError interface{ Code() int }
	if !errors.As(err, &sqliteError) {
		return false
	}
	code := sqliteError.Code() & 0xff
	return code == 5 || code == 6
}

func verifyConnectionSettings(ctx context.Context, db *sql.DB) error {
	connection, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()

	var journalMode string
	if err := connection.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return err
	}
	if !strings.EqualFold(journalMode, "wal") {
		return fmt.Errorf("journal mode is %q, want WAL", journalMode)
	}

	var foreignKeys int
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return err
	}
	if foreignKeys != 1 {
		return errors.New("foreign keys are disabled")
	}

	var busyTimeout int
	if err := connection.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return err
	}
	if busyTimeout != 5000 {
		return fmt.Errorf("busy timeout is %d ms, want 5000 ms", busyTimeout)
	}
	return nil
}

func withImmediateTransaction(ctx context.Context, db *sql.DB, operation func(*sql.Tx) error) error {
	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := operation(transaction); err != nil {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return transaction.Commit()
}

func utcTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
