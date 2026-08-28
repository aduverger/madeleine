package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) getOrCreateConversation(
	ctx context.Context,
	repositoryID RepositoryID,
	key ConversationKey,
	transcriptRef string,
) (ConversationID, error) {
	if key.Harness == "" || key.ExternalID == "" {
		return "", wrapError("get or create conversation", key.ExternalID, ErrInvalidState)
	}

	var conversationID ConversationID
	err := withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		err := transaction.QueryRowContext(ctx, `
			SELECT id FROM conversations
			WHERE repository_id = ? AND harness = ? AND external_id = ?`,
			repositoryID, key.Harness, key.ExternalID).Scan(&conversationID)
		switch {
		case err == nil:
			if transcriptRef == "" {
				return nil
			}
			_, err = transaction.ExecContext(ctx, `
				UPDATE conversations
				SET transcript_ref = ?, updated_at = ?
				WHERE id = ?`, transcriptRef, utcTimestamp(), conversationID)
			return err
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		newID, err := newConversationID()
		if err != nil {
			return err
		}
		conversationID = newID
		createdAt := utcTimestamp()
		storedTranscriptRef := sql.NullString{String: transcriptRef, Valid: transcriptRef != ""}
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO conversations(
				id, repository_id, harness, external_id, transcript_ref, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			conversationID, repositoryID, key.Harness, key.ExternalID,
			storedTranscriptRef, createdAt, createdAt)
		return err
	})
	if err != nil {
		return "", wrapError("get or create conversation", key.ExternalID, err)
	}
	return conversationID, nil
}
