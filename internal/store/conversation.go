package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (tx *Tx) FindConversationID(ctx context.Context, repositoryID, harness, externalID string) (string, bool, error) {
	var conversationID string
	err := tx.tx.QueryRowContext(ctx, `
		SELECT id FROM conversations
		WHERE repository_id = ? AND harness = ? AND external_id = ?`,
		repositoryID, harness, externalID).Scan(&conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return conversationID, true, nil
}

func (tx *Tx) InsertConversation(
	ctx context.Context,
	id, repositoryID, harness, externalID string,
	createdAt time.Time,
) error {
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO conversations(
			id, repository_id, harness, external_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		id, repositoryID, harness, externalID, timestamp(createdAt), timestamp(createdAt))
	return err
}
