package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const captureSelect = `
	SELECT c.id, c.repository_id, c.conversation_id,
		conversation.harness, conversation.external_id,
		c.worktree_root, c.status, c.transcript_ref,
		c.start_cursor, c.end_cursor, c.started_at, c.ended_at,
		c.last_seen_at, c.episode_id
	FROM captures c
	JOIN conversations conversation ON conversation.id = c.conversation_id`

type scanner interface {
	Scan(...any) error
}

func (tx *Tx) FindOpenCaptureID(ctx context.Context, conversationID, openStatus string) (string, bool, error) {
	var captureID string
	err := tx.tx.QueryRowContext(ctx, `
		SELECT id FROM captures WHERE conversation_id = ? AND status = ?`,
		conversationID, openStatus).Scan(&captureID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return captureID, true, nil
}

func (tx *Tx) InsertCapture(ctx context.Context, record CaptureRecord, head string, headExists bool) error {
	transcriptRef := sql.NullString{String: record.TranscriptRef, Valid: record.TranscriptRef != ""}
	gitStartHead := sql.NullString{String: head, Valid: headExists}
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO captures(
			id, conversation_id, repository_id, worktree_root, status,
			transcript_ref, start_cursor, started_at, last_seen_at,
			git_start_head, git_start_head_exists
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ConversationID, record.RepositoryID, record.WorktreeRoot,
		record.Status, transcriptRef, record.StartCursor, timestamp(record.StartedAt),
		timestamp(record.LastSeenAt), gitStartHead, headExists)
	return err
}

func (db *DB) GetCapture(ctx context.Context, captureID string) (CaptureRecord, bool, error) {
	capture, err := scanCapture(db.db.QueryRowContext(ctx, captureSelect+" WHERE c.id = ?", captureID))
	if errors.Is(err, sql.ErrNoRows) {
		return CaptureRecord{}, false, nil
	}
	if err != nil {
		return CaptureRecord{}, false, err
	}
	return capture, true, nil
}

func (tx *Tx) GetCapture(ctx context.Context, captureID string) (CaptureRecord, bool, error) {
	capture, err := scanCapture(tx.tx.QueryRowContext(ctx, captureSelect+" WHERE c.id = ?", captureID))
	if errors.Is(err, sql.ErrNoRows) {
		return CaptureRecord{}, false, nil
	}
	if err != nil {
		return CaptureRecord{}, false, err
	}
	return capture, true, nil
}

func (db *DB) ListPendingCaptures(
	ctx context.Context,
	repositoryID, openStatus, pendingStatus string,
	harness, externalID *string,
) ([]CaptureRecord, error) {
	statement := captureSelect + `
		WHERE c.repository_id = ? AND c.status IN (?, ?)`
	arguments := []any{repositoryID, openStatus, pendingStatus}
	if harness != nil && externalID != nil {
		statement += `
			AND conversation.harness = ? AND conversation.external_id = ?`
		arguments = append(arguments, *harness, *externalID)
	}
	statement += " ORDER BY c.started_at, c.id"

	rows, err := db.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	captures := make([]CaptureRecord, 0)
	for rows.Next() {
		capture, err := scanCapture(rows)
		if err != nil {
			return nil, err
		}
		captures = append(captures, capture)
	}
	return captures, rows.Err()
}

func (tx *Tx) UpsertCapturePath(
	ctx context.Context,
	captureID, path, source string,
	seenAt time.Time,
	updateExisting bool,
) error {
	conflict := "DO NOTHING"
	if updateExisting {
		conflict = "DO UPDATE SET last_seen_at = excluded.last_seen_at"
	}
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO capture_paths(capture_id, path, source, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(capture_id, path) `+conflict,
		captureID, path, source, timestamp(seenAt), timestamp(seenAt))
	return err
}

func (tx *Tx) UpdateCaptureLastSeen(ctx context.Context, captureID string, seenAt time.Time) error {
	_, err := tx.tx.ExecContext(ctx,
		"UPDATE captures SET last_seen_at = ? WHERE id = ?", timestamp(seenAt), captureID)
	return err
}

func (tx *Tx) CapturePaths(ctx context.Context, captureID string) ([]string, error) {
	rows, err := tx.tx.QueryContext(ctx,
		"SELECT path FROM capture_paths WHERE capture_id = ? ORDER BY path", captureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func (tx *Tx) SealCapture(
	ctx context.Context,
	captureID, expectedStatus, nextStatus, endCursor string,
	endedAt time.Time,
) (bool, error) {
	result, err := tx.tx.ExecContext(ctx, `
		UPDATE captures SET status = ?, end_cursor = ?, ended_at = ?
		WHERE id = ? AND status = ?`,
		nextStatus, endCursor, timestamp(endedAt), captureID, expectedStatus)
	if err != nil {
		return false, err
	}
	return oneRowAffected(result)
}

func (tx *Tx) AbandonCapture(
	ctx context.Context,
	captureID, expectedStatus, abandonedStatus string,
	endedAt time.Time,
) (bool, error) {
	result, err := tx.tx.ExecContext(ctx, `
		UPDATE captures SET status = ?, ended_at = COALESCE(ended_at, ?)
		WHERE id = ? AND status = ?`,
		abandonedStatus, timestamp(endedAt), captureID, expectedStatus)
	if err != nil {
		return false, err
	}
	return oneRowAffected(result)
}

func (tx *Tx) FinalizeCapture(
	ctx context.Context,
	captureID, expectedStatus, finalizedStatus, episodeID string,
) (bool, error) {
	result, err := tx.tx.ExecContext(ctx, `
		UPDATE captures SET status = ?, episode_id = ?
		WHERE id = ? AND status = ?`,
		finalizedStatus, episodeID, captureID, expectedStatus)
	if err != nil {
		return false, err
	}
	return oneRowAffected(result)
}

func (tx *Tx) DeleteCaptureRawState(ctx context.Context, captureID string) error {
	if _, err := tx.tx.ExecContext(ctx,
		"DELETE FROM capture_paths WHERE capture_id = ?", captureID); err != nil {
		return err
	}
	_, err := tx.tx.ExecContext(ctx,
		"DELETE FROM capture_git_baseline_paths WHERE capture_id = ?", captureID)
	return err
}

func oneRowAffected(result sql.Result) (bool, error) {
	count, err := result.RowsAffected()
	return count == 1, err
}

func scanCapture(source scanner) (CaptureRecord, error) {
	var capture CaptureRecord
	var transcriptRef, startCursor, endCursor, endedAt, episodeID sql.NullString
	var startedAt, lastSeenAt string
	if err := source.Scan(
		&capture.ID, &capture.RepositoryID, &capture.ConversationID,
		&capture.Harness, &capture.ExternalID,
		&capture.WorktreeRoot, &capture.Status, &transcriptRef,
		&startCursor, &endCursor, &startedAt, &endedAt, &lastSeenAt, &episodeID,
	); err != nil {
		return CaptureRecord{}, err
	}

	var err error
	capture.StartedAt, err = parseTimestamp(startedAt)
	if err != nil {
		return CaptureRecord{}, fmt.Errorf("parse Capture start time: %w", err)
	}
	capture.LastSeenAt, err = parseTimestamp(lastSeenAt)
	if err != nil {
		return CaptureRecord{}, fmt.Errorf("parse Capture last-seen time: %w", err)
	}
	if endedAt.Valid {
		value, err := parseTimestamp(endedAt.String)
		if err != nil {
			return CaptureRecord{}, fmt.Errorf("parse Capture end time: %w", err)
		}
		capture.EndedAt = &value
	}
	capture.TranscriptRef = transcriptRef.String
	capture.StartCursor = startCursor.String
	capture.EndCursor = endCursor.String
	capture.EpisodeID = episodeID.String
	return capture, nil
}
