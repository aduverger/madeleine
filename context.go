package madeleine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const maxEpisodesPerPath = 5

func (s *Store) ContextForPaths(ctx context.Context, request ContextRequest) ([]FileContext, error) {
	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return nil, err
	}

	contexts := make([]FileContext, 0, len(request.Paths))
	contextByPath := make(map[string]int, len(request.Paths))
	for _, input := range request.Paths {
		path, err := normalizeRepositoryPath(repository.WorktreeRoot, input)
		if err != nil {
			return nil, err
		}
		if _, exists := contextByPath[path]; exists {
			continue
		}
		contextByPath[path] = len(contexts)
		contexts = append(contexts, FileContext{Path: path, Episodes: []EpisodeSummary{}})
	}
	if len(contexts) == 0 {
		return contexts, nil
	}

	placeholders := make([]string, len(contexts))
	arguments := make([]any, 0, len(contexts)+3)
	arguments = append(arguments, repository.ID)
	for index, fileContext := range contexts {
		placeholders[index] = "?"
		arguments = append(arguments, fileContext.Path)
	}
	arguments = append(arguments, repository.ID, maxEpisodesPerPath)

	rows, err := s.db.QueryContext(ctx, `
		WITH ranked_episodes AS (
			SELECT files.path, episode.id, episode.ended_at, episode.harness, episode.l1,
				ROW_NUMBER() OVER (
					PARTITION BY files.path
					ORDER BY episode.ended_at DESC, episode.id DESC
				) AS position
			FROM episode_files files
			JOIN episodes episode ON episode.id = files.episode_id
			WHERE files.repository_id = ?
				AND files.path IN (`+strings.Join(placeholders, ", ")+`)
				AND episode.repository_id = ?
		)
		SELECT path, id, ended_at, harness, l1
		FROM ranked_episodes
		WHERE position <= ?
		ORDER BY path, ended_at DESC, id DESC`, arguments...)
	if err != nil {
		return nil, wrapError("get context for paths", request.RepositoryRoot, err)
	}
	defer rows.Close()

	for rows.Next() {
		var path, endedAt string
		var summary EpisodeSummary
		if err := rows.Scan(&path, &summary.EpisodeID, &endedAt, &summary.Harness, &summary.L1); err != nil {
			return nil, wrapError("get context for paths", request.RepositoryRoot, err)
		}
		summary.EndedAt, err = parseStoredTimestamp(endedAt)
		if err != nil {
			return nil, wrapError("get context for paths", path, fmt.Errorf("parse Episode end time: %w", err))
		}
		index, exists := contextByPath[path]
		if !exists {
			return nil, wrapError("get context for paths", path, ErrInvalidState)
		}
		contexts[index].Episodes = append(contexts[index].Episodes, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("get context for paths", request.RepositoryRoot, err)
	}
	return contexts, nil
}

func (s *Store) GetEpisode(ctx context.Context, request EpisodeRequest) (EpisodeDetail, error) {
	if request.RepositoryRoot == "" || request.EpisodeID == "" {
		return EpisodeDetail{}, wrapError("get Episode", string(request.EpisodeID), ErrInvalidState)
	}
	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return EpisodeDetail{}, err
	}
	episode, err := loadEpisode(ctx, s.db, repository.ID, request.EpisodeID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return EpisodeDetail{}, wrapError("get Episode", string(request.EpisodeID), err)
	}
	return EpisodeDetail{
		EpisodeID:       episode.ID,
		ConversationID:  episode.ConversationID,
		ConversationKey: episode.ConversationKey,
		Harness:         episode.Harness,
		Paths:           episode.Paths,
		L1:              episode.L1,
		L2:              episode.L2,
		TranscriptRef:   episode.TranscriptRef,
		StartCursor:     episode.StartCursor,
		EndCursor:       episode.EndCursor,
		StartedAt:       episode.StartedAt,
		EndedAt:         episode.EndedAt,
	}, nil
}
