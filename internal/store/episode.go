package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const episodePathInsertBatchSize = 300

const episodeSelect = `
	SELECT e.id, e.source_capture_id, e.repository_id, e.conversation_id,
		conversation.external_id, e.harness, e.l1, e.l2, e.transcript_ref,
		e.start_cursor, e.end_cursor, e.started_at, e.ended_at, e.created_at
	FROM episodes e
	JOIN conversations conversation ON conversation.id = e.conversation_id`

func (tx *Tx) InsertEpisode(ctx context.Context, episode EpisodeRecord) error {
	transcriptRef := sql.NullString{String: episode.TranscriptRef, Valid: episode.TranscriptRef != ""}
	_, err := tx.tx.ExecContext(ctx, `
		INSERT INTO episodes(
			id, source_capture_id, conversation_id, repository_id, harness,
			started_at, ended_at, l1, l2, transcript_ref,
			start_cursor, end_cursor, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		episode.ID, episode.CaptureID, episode.ConversationID, episode.RepositoryID,
		episode.Harness, timestamp(episode.StartedAt), timestamp(episode.EndedAt),
		episode.L1, episode.L2, transcriptRef, episode.StartCursor, episode.EndCursor,
		timestamp(episode.CreatedAt))
	return err
}

func (tx *Tx) InsertEpisodePaths(ctx context.Context, episodeID, repositoryID string, paths []string) error {
	for start := 0; start < len(paths); start += episodePathInsertBatchSize {
		batch := paths[start:min(start+episodePathInsertBatchSize, len(paths))]
		placeholders := make([]string, len(batch))
		arguments := make([]any, 0, len(batch)*3)
		for index, path := range batch {
			placeholders[index] = "(?, ?, ?)"
			arguments = append(arguments, episodeID, repositoryID, path)
		}
		if _, err := tx.tx.ExecContext(ctx, `
			INSERT INTO episode_files(episode_id, repository_id, path) VALUES `+
			strings.Join(placeholders, ", "), arguments...); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) GetEpisode(ctx context.Context, repositoryID, episodeID string) (EpisodeRecord, bool, error) {
	return loadEpisode(ctx, tx.tx, repositoryID, episodeID)
}

func (db *DB) GetEpisode(ctx context.Context, repositoryID, episodeID string) (EpisodeRecord, bool, error) {
	return loadEpisode(ctx, db.db, repositoryID, episodeID)
}

type episodeQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadEpisode(
	ctx context.Context,
	querier episodeQuerier,
	repositoryID, episodeID string,
) (EpisodeRecord, bool, error) {
	episode, err := scanEpisode(querier.QueryRowContext(ctx,
		episodeSelect+" WHERE e.repository_id = ? AND e.id = ?", repositoryID, episodeID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return EpisodeRecord{}, false, nil
	}
	if err != nil {
		return EpisodeRecord{}, false, err
	}
	episode.Paths, err = loadEpisodePaths(ctx, querier, repositoryID, episodeID)
	if err != nil {
		return EpisodeRecord{}, false, err
	}
	return episode, true, nil
}

func loadEpisodePaths(
	ctx context.Context,
	querier episodeQuerier,
	repositoryID, episodeID string,
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

func scanEpisode(source scanner) (EpisodeRecord, error) {
	var episode EpisodeRecord
	var transcriptRef sql.NullString
	var startedAt, endedAt, createdAt string
	if err := source.Scan(
		&episode.ID, &episode.CaptureID, &episode.RepositoryID, &episode.ConversationID,
		&episode.ExternalID, &episode.Harness, &episode.L1, &episode.L2,
		&transcriptRef, &episode.StartCursor, &episode.EndCursor,
		&startedAt, &endedAt, &createdAt,
	); err != nil {
		return EpisodeRecord{}, err
	}

	episode.TranscriptRef = transcriptRef.String
	var err error
	episode.StartedAt, err = parseTimestamp(startedAt)
	if err != nil {
		return EpisodeRecord{}, fmt.Errorf("parse Episode start time: %w", err)
	}
	episode.EndedAt, err = parseTimestamp(endedAt)
	if err != nil {
		return EpisodeRecord{}, fmt.Errorf("parse Episode end time: %w", err)
	}
	episode.CreatedAt, err = parseTimestamp(createdAt)
	if err != nil {
		return EpisodeRecord{}, fmt.Errorf("parse Episode creation time: %w", err)
	}
	return episode, nil
}
