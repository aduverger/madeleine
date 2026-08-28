package madeleine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const episodePathInsertBatchSize = 300

const episodeSelect = `
	SELECT e.id, e.source_capture_id, e.repository_id, e.conversation_id,
		conversation.external_id, e.harness, e.l1, e.l2, e.transcript_ref,
		e.start_cursor, e.end_cursor, e.started_at, e.ended_at, e.created_at
	FROM episodes e
	JOIN conversations conversation ON conversation.id = e.conversation_id`

type episodeQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) PublishEpisode(ctx context.Context, request PublishEpisodeRequest) (Episode, error) {
	l1, l2, err := validateEpisodeSummaries(request.L1, request.L2)
	if err != nil {
		return Episode{}, wrapError("publish Episode", string(request.CaptureID), err)
	}

	var episode Episode
	err = withImmediateTransaction(ctx, s.db, func(transaction *sql.Tx) error {
		capture, err := scanCapture(transaction.QueryRowContext(
			ctx, captureSelect+" WHERE c.id = ?", request.CaptureID,
		))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		if capture.Status == CaptureStatusFinalized {
			if capture.EpisodeID == "" {
				return fmt.Errorf("%w: finalized Capture has no Episode", ErrInvalidState)
			}
			episode, err = loadEpisode(ctx, transaction, capture.RepositoryID, capture.EpisodeID)
			if err != nil {
				return err
			}
			if episode.L1 != l1 || episode.L2 != l2 {
				return fmt.Errorf("%w: Capture was published with different summaries", ErrConflict)
			}
			return nil
		}
		if capture.Status != CaptureStatusPendingSummary {
			return fmt.Errorf("%w: Capture status is %q", ErrInvalidState, capture.Status)
		}

		paths, err := capturePaths(ctx, transaction, capture.ID)
		if err != nil {
			return err
		}
		if len(paths) == 0 || capture.EndedAt == nil || capture.StartCursor == "" || capture.EndCursor == "" {
			return fmt.Errorf("%w: pending Capture has incomplete finalization data", ErrInvalidState)
		}

		episodeID, err := newEpisodeID()
		if err != nil {
			return err
		}
		createdAt := utcTimestamp()
		transcriptRef := sql.NullString{String: capture.TranscriptRef, Valid: capture.TranscriptRef != ""}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO episodes(
				id, source_capture_id, conversation_id, repository_id, harness,
				started_at, ended_at, l1, l2, transcript_ref,
				start_cursor, end_cursor, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			episodeID, capture.ID, capture.ConversationID, capture.RepositoryID,
			capture.ConversationKey.Harness, capture.StartedAt.Format(time.RFC3339Nano),
			capture.EndedAt.Format(time.RFC3339Nano), l1, l2, transcriptRef,
			capture.StartCursor, capture.EndCursor, createdAt); err != nil {
			return err
		}
		if err := insertEpisodePaths(ctx, transaction, episodeID, capture.RepositoryID, paths); err != nil {
			return err
		}

		result, err := transaction.ExecContext(ctx, `
			UPDATE captures SET status = ?, episode_id = ?
			WHERE id = ? AND status = ?`,
			CaptureStatusFinalized, episodeID, capture.ID, CaptureStatusPendingSummary)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return fmt.Errorf("%w: Capture changed during publication", ErrConflict)
		}
		if _, err := transaction.ExecContext(
			ctx, "DELETE FROM capture_paths WHERE capture_id = ?", capture.ID,
		); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(
			ctx, "DELETE FROM capture_git_baseline_paths WHERE capture_id = ?", capture.ID,
		); err != nil {
			return err
		}

		episode, err = loadEpisode(ctx, transaction, capture.RepositoryID, episodeID)
		return err
	})
	if err != nil {
		return Episode{}, wrapError("publish Episode", string(request.CaptureID), err)
	}
	return episode, nil
}

func insertEpisodePaths(
	ctx context.Context,
	transaction *sql.Tx,
	episodeID EpisodeID,
	repositoryID RepositoryID,
	paths []string,
) error {
	for start := 0; start < len(paths); start += episodePathInsertBatchSize {
		batch := paths[start:min(start+episodePathInsertBatchSize, len(paths))]
		placeholders := make([]string, len(batch))
		arguments := make([]any, 0, len(batch)*3)
		for index, path := range batch {
			placeholders[index] = "(?, ?, ?)"
			arguments = append(arguments, episodeID, repositoryID, path)
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO episode_files(episode_id, repository_id, path) VALUES `+
			strings.Join(placeholders, ", "), arguments...); err != nil {
			return err
		}
	}
	return nil
}

func loadEpisode(
	ctx context.Context,
	querier episodeQuerier,
	repositoryID RepositoryID,
	episodeID EpisodeID,
) (Episode, error) {
	episode, err := scanEpisode(querier.QueryRowContext(ctx,
		episodeSelect+" WHERE e.repository_id = ? AND e.id = ?", repositoryID, episodeID,
	))
	if err != nil {
		return Episode{}, err
	}
	episode.Paths, err = loadEpisodePaths(ctx, querier, repositoryID, episodeID)
	if err != nil {
		return Episode{}, err
	}
	return episode, nil
}

func loadEpisodePaths(
	ctx context.Context,
	querier episodeQuerier,
	repositoryID RepositoryID,
	episodeID EpisodeID,
) ([]string, error) {
	rows, err := querier.QueryContext(ctx, `
		SELECT path FROM episode_files
		WHERE repository_id = ? AND episode_id = ? ORDER BY path`,
		repositoryID, episodeID)
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

func scanEpisode(scanner interface{ Scan(...any) error }) (Episode, error) {
	var episode Episode
	var transcriptRef sql.NullString
	var startedAt, endedAt, createdAt string
	if err := scanner.Scan(
		&episode.ID, &episode.CaptureID, &episode.RepositoryID, &episode.ConversationID,
		&episode.ConversationKey.ExternalID, &episode.Harness, &episode.L1, &episode.L2,
		&transcriptRef, &episode.StartCursor, &episode.EndCursor,
		&startedAt, &endedAt, &createdAt,
	); err != nil {
		return Episode{}, err
	}

	episode.ConversationKey.Harness = episode.Harness
	episode.TranscriptRef = transcriptRef.String
	var err error
	episode.StartedAt, err = parseStoredTimestamp(startedAt)
	if err != nil {
		return Episode{}, fmt.Errorf("parse Episode start time: %w", err)
	}
	episode.EndedAt, err = parseStoredTimestamp(endedAt)
	if err != nil {
		return Episode{}, fmt.Errorf("parse Episode end time: %w", err)
	}
	episode.CreatedAt, err = parseStoredTimestamp(createdAt)
	if err != nil {
		return Episode{}, fmt.Errorf("parse Episode creation time: %w", err)
	}
	return episode, nil
}
