package madeleine

import (
	"context"
	"fmt"

	"github.com/aduverger/madeleine/internal/repopath"
)

const maxEpisodesPerPath = 5

func (s *Service) ContextForPaths(ctx context.Context, request ContextRequest) ([]FileContext, error) {
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

	paths := make([]string, len(contexts))
	for index, fileContext := range contexts {
		paths[index] = fileContext.Path
	}
	summaries, err := s.database.EpisodeSummariesForPaths(
		ctx, string(repository.ID), paths, maxEpisodesPerPath,
	)
	if err != nil {
		return nil, wrapError("get context for paths", request.RepositoryRoot, err)
	}
	for _, summary := range summaries {
		contextIndex, exists := contextIndexByPath[summary.Path]
		if !exists {
			return nil, wrapError("get context for paths", summary.Path, ErrInvalidState)
		}
		contexts[contextIndex].Episodes = append(contexts[contextIndex].Episodes, EpisodeSummary{
			EpisodeID: EpisodeID(summary.EpisodeID),
			EndedAt:   summary.EndedAt,
			Harness:   Harness(summary.Harness),
			L1:        summary.L1,
		})
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

func (s *Service) GetEpisode(ctx context.Context, request EpisodeRequest) (EpisodeDetail, error) {
	if request.RepositoryRoot == "" || request.EpisodeID == "" {
		return EpisodeDetail{}, wrapError("get Episode", string(request.EpisodeID), ErrInvalidState)
	}
	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return EpisodeDetail{}, err
	}
	record, found, err := s.database.GetEpisode(ctx, string(repository.ID), string(request.EpisodeID))
	if err != nil {
		return EpisodeDetail{}, wrapError("get Episode", string(request.EpisodeID), err)
	}
	if !found {
		return EpisodeDetail{}, wrapError("get Episode", string(request.EpisodeID), ErrNotFound)
	}
	if record.ID == "" {
		return EpisodeDetail{}, wrapError(
			"get Episode", string(request.EpisodeID), fmt.Errorf("%w: empty Episode record", ErrInvalidState),
		)
	}
	return EpisodeDetail{
		EpisodeID:       EpisodeID(record.ID),
		ConversationID:  ConversationID(record.ConversationID),
		ConversationKey: ConversationKey{Harness: Harness(record.Harness), ExternalID: record.ExternalID},
		Harness:         Harness(record.Harness),
		Paths:           record.Paths,
		L1:              record.L1,
		L2:              record.L2,
		TranscriptRef:   record.TranscriptRef,
		StartCursor:     record.StartCursor,
		EndCursor:       record.EndCursor,
		StartedAt:       record.StartedAt,
		EndedAt:         record.EndedAt,
	}, nil
}
