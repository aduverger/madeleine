package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aduverger/madeleine/internal/gitstate"
	"github.com/aduverger/madeleine/internal/repopath"
)

const captureSelect = `
	SELECT c.id, c.repository_id, c.conversation_id,
		conversation.harness, conversation.external_id,
		c.worktree_root, c.status, c.transcript_ref,
		c.start_cursor, c.end_cursor, c.started_at, c.ended_at,
		c.last_seen_at, c.episode_id
	FROM captures c
	JOIN conversations conversation ON conversation.id = c.conversation_id`

type captureScanner interface {
	Scan(...any) error
}

func (s *Store) StartCapture(ctx context.Context, request StartCaptureRequest) (Capture, error) {
	if request.RepositoryRoot == "" || request.StartCursor == "" {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, ErrInvalidState)
	}

	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return Capture{}, err
	}
	gitBaseline, err := gitstate.Capture(ctx, repository.WorktreeRoot, nil)
	if err != nil {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, err)
	}
	conversationID, err := s.getOrCreateConversation(
		ctx, repository.ID, request.ConversationKey, request.TranscriptRef,
	)
	if err != nil {
		return Capture{}, err
	}
	captureID, err := newCaptureID()
	if err != nil {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, err)
	}

	now := utcTimestamp()
	err = withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		var existingCaptureID CaptureID
		err := transaction.QueryRowContext(ctx, `
			SELECT id FROM captures
			WHERE conversation_id = ? AND status = ?`,
			conversationID, CaptureStatusOpen).Scan(&existingCaptureID)
		switch {
		case err == nil:
			return fmt.Errorf("%w: Conversation already has open Capture %s", ErrConflict, existingCaptureID)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		transcriptRef := sql.NullString{String: request.TranscriptRef, Valid: request.TranscriptRef != ""}
		gitStartHead := sql.NullString{String: gitBaseline.Head, Valid: gitBaseline.HeadExists}
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO captures(
				id, conversation_id, repository_id, worktree_root, status,
				transcript_ref, start_cursor, started_at, last_seen_at,
				git_start_head, git_start_head_exists
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			captureID, conversationID, repository.ID, repository.WorktreeRoot,
			CaptureStatusOpen, transcriptRef, request.StartCursor, now, now,
			gitStartHead, gitBaseline.HeadExists)
		if err != nil {
			return err
		}
		return insertCaptureGitBaseline(ctx, transaction, captureID, gitBaseline.Paths)
	})
	if err != nil {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, err)
	}

	startedAt, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, err)
	}
	return Capture{
		ID:              captureID,
		RepositoryID:    repository.ID,
		ConversationID:  conversationID,
		ConversationKey: request.ConversationKey,
		WorktreeRoot:    repository.WorktreeRoot,
		Status:          CaptureStatusOpen,
		TranscriptRef:   request.TranscriptRef,
		StartCursor:     request.StartCursor,
		StartedAt:       startedAt,
		LastSeenAt:      startedAt,
	}, nil
}

func (s *Store) GetCapture(ctx context.Context, captureID CaptureID) (Capture, error) {
	capture, err := scanCapture(s.db.QueryRowContext(ctx, captureSelect+" WHERE c.id = ?", captureID))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return Capture{}, wrapError("get capture", string(captureID), err)
	}
	return capture, nil
}

func (s *Store) RecordWrite(ctx context.Context, request RecordWriteRequest) error {
	err := withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		var worktreeRoot string
		var status CaptureStatus
		err := transaction.QueryRowContext(ctx,
			"SELECT worktree_root, status FROM captures WHERE id = ?", request.CaptureID,
		).Scan(&worktreeRoot, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := transitionCapture(status, captureActionRecordWrite, false); err != nil {
			return err
		}

		path, err := repopath.Normalize(worktreeRoot, request.Path)
		if err != nil {
			return err
		}
		now := utcTimestamp()
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO capture_paths(capture_id, path, source, first_seen_at, last_seen_at)
			VALUES (?, ?, 'tool', ?, ?)
			ON CONFLICT(capture_id, path) DO UPDATE SET
				last_seen_at = excluded.last_seen_at`,
			request.CaptureID, path, now, now); err != nil {
			return err
		}
		_, err = transaction.ExecContext(ctx,
			"UPDATE captures SET last_seen_at = ? WHERE id = ?", now, request.CaptureID)
		return err
	})
	return wrapError("record write", string(request.CaptureID), err)
}

func (s *Store) ListPendingCaptures(ctx context.Context, query PendingCaptureQuery) ([]Capture, error) {
	if query.RepositoryRoot == "" {
		return nil, wrapError("list pending captures", query.RepositoryRoot, ErrInvalidState)
	}
	repository, err := s.ResolveRepository(ctx, query.RepositoryRoot)
	if err != nil {
		return nil, err
	}

	statement := captureSelect + `
		WHERE c.repository_id = ? AND c.status IN (?, ?)`
	arguments := []any{repository.ID, CaptureStatusOpen, CaptureStatusPendingSummary}
	if query.ConversationKey != nil {
		if query.ConversationKey.Harness == "" || query.ConversationKey.ExternalID == "" {
			return nil, wrapError("list pending captures", query.ConversationKey.ExternalID, ErrInvalidState)
		}
		statement += `
			AND conversation.harness = ? AND conversation.external_id = ?`
		arguments = append(arguments, query.ConversationKey.Harness, query.ConversationKey.ExternalID)
	}
	statement += " ORDER BY c.started_at, c.id"

	rows, err := s.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, wrapError("list pending captures", query.RepositoryRoot, err)
	}
	defer rows.Close()

	captures := make([]Capture, 0)
	for rows.Next() {
		capture, err := scanCapture(rows)
		if err != nil {
			return nil, wrapError("list pending captures", query.RepositoryRoot, err)
		}
		captures = append(captures, capture)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("list pending captures", query.RepositoryRoot, err)
	}
	return captures, nil
}

func (s *Store) SealCapture(ctx context.Context, request SealCaptureRequest) (FinalizationDraft, error) {
	if request.EndCursor == "" {
		return FinalizationDraft{}, wrapError("seal capture", string(request.CaptureID), ErrInvalidState)
	}

	gitPaths, err := s.reconcileCaptureGitPaths(ctx, request.CaptureID)
	if err != nil {
		return FinalizationDraft{}, wrapError("seal capture", string(request.CaptureID), err)
	}

	var draft FinalizationDraft
	err = withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		var status CaptureStatus
		var episodeID sql.NullString
		err := transaction.QueryRowContext(ctx,
			"SELECT status, episode_id FROM captures WHERE id = ?", request.CaptureID,
		).Scan(&status, &episodeID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		if status == CaptureStatusOpen {
			if err := insertGitCapturePaths(ctx, transaction, request.CaptureID, gitPaths); err != nil {
				return err
			}
		}
		paths, err := capturePaths(ctx, transaction, request.CaptureID)
		if err != nil {
			return err
		}
		nextStatus, err := transitionCapture(status, captureActionSeal, len(paths) > 0)
		if err != nil {
			return err
		}
		if status == CaptureStatusOpen {
			endedAt := utcTimestamp()
			if _, err := transaction.ExecContext(ctx, `
				UPDATE captures
				SET status = ?, end_cursor = ?, ended_at = ?
				WHERE id = ? AND status = ?`,
				nextStatus, request.EndCursor, endedAt, request.CaptureID, CaptureStatusOpen); err != nil {
				return err
			}
			if nextStatus == CaptureStatusAbandoned {
				if err := deleteCaptureRawState(ctx, transaction, request.CaptureID); err != nil {
					return err
				}
			}
		}

		draft = FinalizationDraft{
			CaptureID: request.CaptureID,
			Status:    nextStatus,
			Empty:     nextStatus == CaptureStatusAbandoned,
			Paths:     []string{},
		}
		if nextStatus == CaptureStatusPendingSummary {
			draft.Paths = paths
		}
		if nextStatus == CaptureStatusFinalized && episodeID.Valid {
			draft.EpisodeID = EpisodeID(episodeID.String)
		}
		return nil
	})
	if err != nil {
		return FinalizationDraft{}, wrapError("seal capture", string(request.CaptureID), err)
	}
	return draft, nil
}

func (s *Store) AbandonCapture(ctx context.Context, captureID CaptureID) error {
	err := withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		var status CaptureStatus
		err := transaction.QueryRowContext(ctx,
			"SELECT status FROM captures WHERE id = ?", captureID,
		).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		nextStatus, err := transitionCapture(status, captureActionAbandon, false)
		if err != nil {
			return err
		}
		if err := deleteCaptureRawState(ctx, transaction, captureID); err != nil {
			return err
		}
		if nextStatus == status {
			return nil
		}
		_, err = transaction.ExecContext(ctx, `
			UPDATE captures SET status = ?, ended_at = COALESCE(ended_at, ?) WHERE id = ?`,
			nextStatus, utcTimestamp(), captureID)
		return err
	})
	return wrapError("abandon capture", string(captureID), err)
}

func deleteCaptureRawState(ctx context.Context, transaction *sql.Tx, captureID CaptureID) error {
	if _, err := transaction.ExecContext(ctx,
		"DELETE FROM capture_paths WHERE capture_id = ?", captureID); err != nil {
		return err
	}
	_, err := transaction.ExecContext(ctx,
		"DELETE FROM capture_git_baseline_paths WHERE capture_id = ?", captureID)
	return err
}

func insertCaptureGitBaseline(
	ctx context.Context,
	transaction *sql.Tx,
	captureID CaptureID,
	paths map[string]gitstate.PathSnapshot,
) error {
	for path, snapshot := range paths {
		indexIdentity := sql.NullString{String: snapshot.IndexIdentity, Valid: snapshot.IndexIdentity != ""}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO capture_git_baseline_paths(
				capture_id, path, porcelain_status, worktree_fingerprint, index_identity
			) VALUES (?, ?, ?, ?, ?)`,
			captureID, path, snapshot.PorcelainStatus,
			snapshot.WorktreeFingerprint, indexIdentity); err != nil {
			return err
		}
	}
	return nil
}

func insertGitCapturePaths(
	ctx context.Context,
	transaction *sql.Tx,
	captureID CaptureID,
	paths []string,
) error {
	now := utcTimestamp()
	for _, path := range paths {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO capture_paths(capture_id, path, source, first_seen_at, last_seen_at)
			VALUES (?, ?, 'git', ?, ?)
			ON CONFLICT(capture_id, path) DO NOTHING`,
			captureID, path, now, now); err != nil {
			return err
		}
	}
	return nil
}

func capturePaths(ctx context.Context, transaction *sql.Tx, captureID CaptureID) ([]string, error) {
	rows, err := transaction.QueryContext(ctx,
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

func scanCapture(scanner captureScanner) (Capture, error) {
	var capture Capture
	var transcriptRef, startCursor, endCursor, endedAt, episodeID sql.NullString
	var startedAt, lastSeenAt string
	if err := scanner.Scan(
		&capture.ID, &capture.RepositoryID, &capture.ConversationID,
		&capture.ConversationKey.Harness, &capture.ConversationKey.ExternalID,
		&capture.WorktreeRoot, &capture.Status, &transcriptRef,
		&startCursor, &endCursor, &startedAt, &endedAt, &lastSeenAt, &episodeID,
	); err != nil {
		return Capture{}, err
	}

	var err error
	capture.StartedAt, err = parseStoredTimestamp(startedAt)
	if err != nil {
		return Capture{}, fmt.Errorf("parse Capture start time: %w", err)
	}
	capture.LastSeenAt, err = parseStoredTimestamp(lastSeenAt)
	if err != nil {
		return Capture{}, fmt.Errorf("parse Capture last-seen time: %w", err)
	}
	if endedAt.Valid {
		value, err := parseStoredTimestamp(endedAt.String)
		if err != nil {
			return Capture{}, fmt.Errorf("parse Capture end time: %w", err)
		}
		capture.EndedAt = &value
	}
	capture.TranscriptRef = transcriptRef.String
	capture.StartCursor = startCursor.String
	capture.EndCursor = endCursor.String
	capture.EpisodeID = EpisodeID(episodeID.String)
	return capture, nil
}

func parseStoredTimestamp(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
