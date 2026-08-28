package madeleine

import (
	"context"

	"github.com/aduverger/madeleine/internal/store"
)

type Store struct {
	inner *store.Store
}

var (
	ErrNotFound          = store.ErrNotFound
	ErrConflict          = store.ErrConflict
	ErrInvalidState      = store.ErrInvalidState
	ErrNotGitRepository  = store.ErrNotGitRepository
	ErrOutsideRepository = store.ErrOutsideRepository
)

func Open(ctx context.Context, options Options) (*Store, error) {
	inner, err := store.Open(ctx, store.Options{Home: options.Home})
	if err != nil {
		return nil, err
	}
	return &Store{inner: inner}, nil
}

func ResolveRepository(ctx context.Context, path string) (Repository, error) {
	repository, err := store.ResolveRepository(ctx, path)
	return fromStoreRepository(repository), err
}

func (s *Store) Close() error {
	return s.inner.Close()
}

func (s *Store) ResolveRepository(ctx context.Context, path string) (Repository, error) {
	repository, err := s.inner.ResolveRepository(ctx, path)
	return fromStoreRepository(repository), err
}

func (s *Store) StartCapture(ctx context.Context, request StartCaptureRequest) (Capture, error) {
	capture, err := s.inner.StartCapture(ctx, store.StartCaptureRequest{
		RepositoryRoot:  request.RepositoryRoot,
		ConversationKey: toStoreConversationKey(request.ConversationKey),
		TranscriptRef:   request.TranscriptRef,
		StartCursor:     request.StartCursor,
	})
	return fromStoreCapture(capture), err
}

func (s *Store) GetCapture(ctx context.Context, captureID CaptureID) (Capture, error) {
	capture, err := s.inner.GetCapture(ctx, store.CaptureID(captureID))
	return fromStoreCapture(capture), err
}

func (s *Store) RecordWrite(ctx context.Context, request RecordWriteRequest) error {
	return s.inner.RecordWrite(ctx, store.RecordWriteRequest{
		CaptureID: store.CaptureID(request.CaptureID),
		Path:      request.Path,
	})
}

func (s *Store) ListPendingCaptures(ctx context.Context, query PendingCaptureQuery) ([]Capture, error) {
	storeQuery := store.PendingCaptureQuery{RepositoryRoot: query.RepositoryRoot}
	if query.ConversationKey != nil {
		conversationKey := toStoreConversationKey(*query.ConversationKey)
		storeQuery.ConversationKey = &conversationKey
	}
	captures, err := s.inner.ListPendingCaptures(ctx, storeQuery)
	if err != nil {
		return nil, err
	}
	result := make([]Capture, len(captures))
	for index, capture := range captures {
		result[index] = fromStoreCapture(capture)
	}
	return result, nil
}

func (s *Store) SealCapture(ctx context.Context, request SealCaptureRequest) (FinalizationDraft, error) {
	draft, err := s.inner.SealCapture(ctx, store.SealCaptureRequest{
		CaptureID: store.CaptureID(request.CaptureID),
		EndCursor: request.EndCursor,
	})
	return FinalizationDraft{
		CaptureID: CaptureID(draft.CaptureID),
		Status:    CaptureStatus(draft.Status),
		Empty:     draft.Empty,
		Paths:     draft.Paths,
		EpisodeID: EpisodeID(draft.EpisodeID),
	}, err
}

func (s *Store) PublishEpisode(ctx context.Context, request PublishEpisodeRequest) (Episode, error) {
	episode, err := s.inner.PublishEpisode(ctx, store.PublishEpisodeRequest{
		CaptureID: store.CaptureID(request.CaptureID),
		L1:        request.L1,
		L2:        request.L2,
	})
	return fromStoreEpisode(episode), err
}

func (s *Store) AbandonCapture(ctx context.Context, captureID CaptureID) error {
	return s.inner.AbandonCapture(ctx, store.CaptureID(captureID))
}

func (s *Store) ContextForPaths(ctx context.Context, request ContextRequest) ([]FileContext, error) {
	contexts, err := s.inner.ContextForPaths(ctx, store.ContextRequest{
		RepositoryRoot: request.RepositoryRoot,
		Paths:          request.Paths,
	})
	if err != nil {
		return nil, err
	}
	result := make([]FileContext, len(contexts))
	for index, fileContext := range contexts {
		summaries := make([]EpisodeSummary, len(fileContext.Episodes))
		for summaryIndex, summary := range fileContext.Episodes {
			summaries[summaryIndex] = EpisodeSummary{
				EpisodeID: EpisodeID(summary.EpisodeID),
				EndedAt:   summary.EndedAt,
				Harness:   Harness(summary.Harness),
				L1:        summary.L1,
			}
		}
		result[index] = FileContext{Path: fileContext.Path, Episodes: summaries}
	}
	return result, nil
}

func (s *Store) GetEpisode(ctx context.Context, request EpisodeRequest) (EpisodeDetail, error) {
	detail, err := s.inner.GetEpisode(ctx, store.EpisodeRequest{
		RepositoryRoot: request.RepositoryRoot,
		EpisodeID:      store.EpisodeID(request.EpisodeID),
	})
	return EpisodeDetail{
		EpisodeID:       EpisodeID(detail.EpisodeID),
		ConversationID:  ConversationID(detail.ConversationID),
		ConversationKey: fromStoreConversationKey(detail.ConversationKey),
		Harness:         Harness(detail.Harness),
		Paths:           detail.Paths,
		L1:              detail.L1,
		L2:              detail.L2,
		TranscriptRef:   detail.TranscriptRef,
		StartCursor:     detail.StartCursor,
		EndCursor:       detail.EndCursor,
		StartedAt:       detail.StartedAt,
		EndedAt:         detail.EndedAt,
	}, err
}

func toStoreConversationKey(key ConversationKey) store.ConversationKey {
	return store.ConversationKey{Harness: store.Harness(key.Harness), ExternalID: key.ExternalID}
}

func fromStoreConversationKey(key store.ConversationKey) ConversationKey {
	return ConversationKey{Harness: Harness(key.Harness), ExternalID: key.ExternalID}
}

func fromStoreRepository(repository store.Repository) Repository {
	return Repository{
		ID:           RepositoryID(repository.ID),
		WorktreeRoot: repository.WorktreeRoot,
		GitCommonDir: repository.GitCommonDir,
		Origin:       repository.Origin,
	}
}

func fromStoreCapture(capture store.Capture) Capture {
	return Capture{
		ID:              CaptureID(capture.ID),
		RepositoryID:    RepositoryID(capture.RepositoryID),
		ConversationID:  ConversationID(capture.ConversationID),
		ConversationKey: fromStoreConversationKey(capture.ConversationKey),
		WorktreeRoot:    capture.WorktreeRoot,
		Status:          CaptureStatus(capture.Status),
		TranscriptRef:   capture.TranscriptRef,
		StartCursor:     capture.StartCursor,
		EndCursor:       capture.EndCursor,
		StartedAt:       capture.StartedAt,
		EndedAt:         capture.EndedAt,
		LastSeenAt:      capture.LastSeenAt,
		EpisodeID:       EpisodeID(capture.EpisodeID),
	}
}

func fromStoreEpisode(episode store.Episode) Episode {
	return Episode{
		ID:              EpisodeID(episode.ID),
		CaptureID:       CaptureID(episode.CaptureID),
		RepositoryID:    RepositoryID(episode.RepositoryID),
		ConversationID:  ConversationID(episode.ConversationID),
		ConversationKey: fromStoreConversationKey(episode.ConversationKey),
		Harness:         Harness(episode.Harness),
		Paths:           episode.Paths,
		L1:              episode.L1,
		L2:              episode.L2,
		TranscriptRef:   episode.TranscriptRef,
		StartCursor:     episode.StartCursor,
		EndCursor:       episode.EndCursor,
		StartedAt:       episode.StartedAt,
		EndedAt:         episode.EndedAt,
		CreatedAt:       episode.CreatedAt,
	}
}
