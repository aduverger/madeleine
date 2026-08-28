package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aduverger/madeleine/internal/repopath"
)

const maxEpisodesPerPath = 5

func (s *Store) ContextForPaths(ctx context.Context, request ContextRequest) ([]FileContext, error) {
	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return nil, err
	}

	contexts, contextIndexByPath, err := normalizeContextPaths(repository.WorktreeRoot, request.Paths)
	if err != nil {
		return nil, err
	}
	if len(contexts) == 0 {
		return contexts, nil
	}
	if err := s.populateEpisodeContext(
		ctx, request.RepositoryRoot, repository.ID, contexts, contextIndexByPath,
	); err != nil {
		return nil, err
	}
	return contexts, nil
}

func normalizeContextPaths(worktreeRoot string, requestedPaths []string) ([]FileContext, map[string]int, error) {
	contexts := make([]FileContext, 0, len(requestedPaths))
	contextIndexByPath := make(map[string]int, len(requestedPaths))
	for _, requestedPath := range requestedPaths {
		path, err := repopath.Normalize(worktreeRoot, requestedPath)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := contextIndexByPath[path]; exists {
			continue
		}
		contextIndexByPath[path] = len(contexts)
		contexts = append(contexts, FileContext{Path: path, Episodes: []EpisodeSummary{}})
	}
	return contexts, contextIndexByPath, nil
}

func episodeContextQuery(repositoryID RepositoryID, contexts []FileContext) (string, []any) {
	placeholders := make([]string, len(contexts))
	arguments := make([]any, 0, len(contexts)+3)
	arguments = append(arguments, repositoryID)
	for index, fileContext := range contexts {
		placeholders[index] = "?"
		arguments = append(arguments, fileContext.Path)
	}
	arguments = append(arguments, repositoryID, maxEpisodesPerPath)

	query := `
		WITH ranked_episodes AS (
			SELECT files.path, episode.id, episode.ended_at, episode.harness, episode.l1,
				ROW_NUMBER() OVER (
					PARTITION BY files.path
					ORDER BY episode.ended_at DESC, episode.id DESC
				) AS recency_rank
			FROM episode_files files
			JOIN episodes episode ON episode.id = files.episode_id
			WHERE files.repository_id = ?
				AND files.path IN (` + strings.Join(placeholders, ", ") + `)
				AND episode.repository_id = ?
		)
		SELECT path, id, ended_at, harness, l1
		FROM ranked_episodes
		WHERE recency_rank <= ?
		ORDER BY path, ended_at DESC, id DESC`
	return query, arguments
}

func (s *Store) populateEpisodeContext(
	ctx context.Context,
	reference string,
	repositoryID RepositoryID,
	contexts []FileContext,
	contextIndexByPath map[string]int,
) error {
	query, arguments := episodeContextQuery(repositoryID, contexts)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return wrapError("get context for paths", reference, err)
	}
	defer rows.Close()

	for rows.Next() {
		var path, endedAt string
		var summary EpisodeSummary
		if err := rows.Scan(&path, &summary.EpisodeID, &endedAt, &summary.Harness, &summary.L1); err != nil {
			return wrapError("get context for paths", reference, err)
		}
		summary.EndedAt, err = parseStoredTimestamp(endedAt)
		if err != nil {
			return wrapError("get context for paths", path, fmt.Errorf("parse Episode end time: %w", err))
		}
		contextIndex, exists := contextIndexByPath[path]
		if !exists {
			return wrapError("get context for paths", path, ErrInvalidState)
		}
		contexts[contextIndex].Episodes = append(contexts[contextIndex].Episodes, summary)
	}
	if err := rows.Err(); err != nil {
		return wrapError("get context for paths", reference, err)
	}
	return nil
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
