package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func migrate(ctx context.Context, db *sql.DB, source fs.FS) error {
	migrations, err := loadMigrations(source)
	if err != nil {
		return fmt.Errorf("load store migrations: %w", err)
	}
	latestVersion := migrations[len(migrations)-1].version

	for _, migration := range migrations {
		err := withImmediateTransaction(ctx, db, func(transaction *sql.Tx) error {
			if _, err := transaction.ExecContext(ctx, `
				CREATE TABLE IF NOT EXISTS schema_migrations (
					version INTEGER PRIMARY KEY,
					applied_at TEXT NOT NULL
				)`); err != nil {
				return err
			}

			appliedVersions, err := readAppliedMigrationVersions(ctx, transaction)
			if err != nil {
				return err
			}
			for version := range appliedVersions {
				if version > latestVersion {
					return fmt.Errorf("database schema version %d is newer than binary version %d", version, latestVersion)
				}
			}
			if appliedVersions[migration.version] {
				return nil
			}

			if _, err := transaction.ExecContext(ctx, migration.sql); err != nil {
				return err
			}
			_, err = transaction.ExecContext(ctx,
				"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
				migration.version, timestamp(time.Now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("apply store migration %q: %w", migration.name, err)
		}
	}
	return nil
}

func loadMigrations(source fs.FS) ([]migration, error) {
	names, err := fs.Glob(source, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no migrations found")
	}

	migrations := make([]migration, 0, len(names))
	versions := make(map[int]string, len(names))
	for _, name := range names {
		base := path.Base(name)
		versionText, _, found := strings.Cut(base, "_")
		if !found {
			return nil, fmt.Errorf("migration %q has no numeric prefix", name)
		}
		version, err := strconv.Atoi(versionText)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration %q has an invalid version", name)
		}
		if existing, duplicate := versions[version]; duplicate {
			return nil, fmt.Errorf("migrations %q and %q use version %d", existing, name, version)
		}
		contents, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		versions[version] = name
		migrations = append(migrations, migration{version: version, name: name, sql: string(contents)})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func (db *DB) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := db.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func readAppliedMigrationVersions(ctx context.Context, transaction *sql.Tx) (map[int]bool, error) {
	rows, err := transaction.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions[version] = true
	}
	return versions, rows.Err()
}

func withImmediateTransaction(ctx context.Context, db *sql.DB, operation func(*sql.Tx) error) error {
	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := operation(transaction); err != nil {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w; rollback: %v", err, rollbackErr)
		}
		return err
	}
	return transaction.Commit()
}
