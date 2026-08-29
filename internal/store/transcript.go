package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const transcriptSelect = `
	SELECT id, capture_id, repository_id, conversation_id, harness,
		format_version, source_start_cursor, source_end_cursor,
		compact_text, created_at, published_at
	FROM transcripts`

func (tx *Tx) InsertTranscript(
	ctx context.Context,
	record TranscriptRecord,
	entries []TranscriptEntryRecord,
) error {
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO transcripts(
			id, capture_id, repository_id, conversation_id, harness,
			format_version, source_start_cursor, source_end_cursor, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.CaptureID, record.RepositoryID, record.ConversationID,
		record.Harness, record.FormatVersion, record.SourceStartCursor,
		record.SourceEndCursor, timestamp(record.CreatedAt))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := tx.tx.ExecContext(ctx, `
			INSERT INTO transcript_entries(transcript_id, position, kind, content_json)
			VALUES (?, ?, ?, ?)`, record.ID, entry.Position, entry.Kind, entry.ContentJSON); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) GetTranscriptByCapture(ctx context.Context, captureID string) (TranscriptRecord, bool, error) {
	return scanOptionalTranscript(tx.tx.QueryRowContext(ctx,
		transcriptSelect+" WHERE capture_id = ?", captureID))
}

func (db *DB) GetTranscript(ctx context.Context, repositoryID, transcriptID string) (TranscriptRecord, bool, error) {
	return scanOptionalTranscript(db.db.QueryRowContext(ctx,
		transcriptSelect+" WHERE repository_id = ? AND id = ?", repositoryID, transcriptID))
}

func (tx *Tx) TranscriptEntries(ctx context.Context, transcriptID string) ([]TranscriptEntryRecord, error) {
	return queryTranscriptEntries(ctx, tx.tx, transcriptID, 0, -1)
}

func (db *DB) TranscriptEntries(
	ctx context.Context,
	transcriptID string,
	offset, limit int,
) ([]TranscriptEntryRecord, error) {
	return queryTranscriptEntries(ctx, db.db, transcriptID, offset, limit)
}

func queryTranscriptEntries(
	ctx context.Context,
	querier episodeQuerier,
	transcriptID string,
	offset, limit int,
) ([]TranscriptEntryRecord, error) {
	rows, err := querier.QueryContext(ctx, `
		SELECT position, kind, content_json
		FROM transcript_entries
		WHERE transcript_id = ? AND position >= ?
		ORDER BY position
		LIMIT ?`, transcriptID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]TranscriptEntryRecord, 0)
	for rows.Next() {
		var entry TranscriptEntryRecord
		if err := rows.Scan(&entry.Position, &entry.Kind, &entry.ContentJSON); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (tx *Tx) PublishTranscript(ctx context.Context, transcriptID, compactText string, publishedAt time.Time) (bool, error) {
	result, err := tx.tx.ExecContext(ctx, `
		UPDATE transcripts SET compact_text = ?, published_at = ?
		WHERE id = ? AND compact_text IS NULL`, compactText, timestamp(publishedAt), transcriptID)
	if err != nil {
		return false, err
	}
	return oneRowAffected(result)
}

func (tx *Tx) DeleteCaptureTranscript(ctx context.Context, captureID string) error {
	if _, err := tx.tx.ExecContext(ctx,
		"UPDATE captures SET transcript_id = NULL WHERE id = ?", captureID); err != nil {
		return err
	}
	_, err := tx.tx.ExecContext(ctx, "DELETE FROM transcripts WHERE capture_id = ?", captureID)
	return err
}

func scanOptionalTranscript(source scanner) (TranscriptRecord, bool, error) {
	record, err := scanTranscript(source)
	if errors.Is(err, sql.ErrNoRows) {
		return TranscriptRecord{}, false, nil
	}
	return record, err == nil, err
}

func scanTranscript(source scanner) (TranscriptRecord, error) {
	var record TranscriptRecord
	var compactText, publishedAt sql.NullString
	var createdAt string
	if err := source.Scan(
		&record.ID, &record.CaptureID, &record.RepositoryID, &record.ConversationID,
		&record.Harness, &record.FormatVersion, &record.SourceStartCursor,
		&record.SourceEndCursor, &compactText, &createdAt, &publishedAt,
	); err != nil {
		return TranscriptRecord{}, err
	}
	var err error
	record.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return TranscriptRecord{}, fmt.Errorf("parse Transcript creation time: %w", err)
	}
	if compactText.Valid {
		record.CompactText = &compactText.String
	}
	if publishedAt.Valid {
		value, err := parseTimestamp(publishedAt.String)
		if err != nil {
			return TranscriptRecord{}, fmt.Errorf("parse Transcript publication time: %w", err)
		}
		record.PublishedAt = &value
	}
	return record, nil
}
