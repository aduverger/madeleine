package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (tx *Tx) FindRepositoryIDByAlias(ctx context.Context, kind, value string) (string, bool, error) {
	var repositoryID string
	err := tx.tx.QueryRowContext(ctx,
		"SELECT repository_id FROM repository_aliases WHERE kind = ? AND value = ?",
		kind, value).Scan(&repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return repositoryID, err == nil, err
}

func (tx *Tx) InsertRepository(ctx context.Context, record RepositoryRecord, createdAt time.Time) error {
	_, err := tx.tx.ExecContext(ctx,
		"INSERT INTO repositories(id, created_at) VALUES (?, ?)", record.ID, timestamp(createdAt))
	return err
}

func (tx *Tx) InsertRepositoryAlias(ctx context.Context, repositoryID, kind, value string, createdAt time.Time) error {
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO repository_aliases(repository_id, kind, value, created_at)
		VALUES (?, ?, ?, ?)`, repositoryID, kind, value, timestamp(createdAt))
	return err
}
