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
	return conversationID, err == nil, err
}

func (tx *Tx) InsertConversation(
	ctx context.Context,
	id, repositoryID, harness, externalID, transcriptRef string,
	createdAt time.Time,
) error {
	storedTranscriptRef := sql.NullString{String: transcriptRef, Valid: transcriptRef != ""}
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO conversations(
			id, repository_id, harness, external_id, transcript_ref, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, repositoryID, harness, externalID, storedTranscriptRef,
		timestamp(createdAt), timestamp(createdAt))
	return err
}

func (tx *Tx) UpdateConversationTranscript(ctx context.Context, id, transcriptRef string, updatedAt time.Time) error {
	_, err := tx.tx.ExecContext(ctx, `
		UPDATE conversations SET transcript_ref = ?, updated_at = ? WHERE id = ?`,
		transcriptRef, timestamp(updatedAt), id)
	return err
}
