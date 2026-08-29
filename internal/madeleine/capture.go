package madeleine

import (
	"context"
	"fmt"

	"github.com/aduverger/madeleine/internal/repopath"
	"github.com/aduverger/madeleine/internal/store"
)

type captureAction uint8

const (
	captureActionRecordWrite captureAction = iota
	captureActionSeal
	captureActionAbandon
)

func transitionCapture(status CaptureStatus, action captureAction, hasPaths bool) (CaptureStatus, error) {
	switch action {
	case captureActionRecordWrite:
		if status == CaptureStatusOpen {
			return status, nil
		}
	case captureActionSeal:
		switch status {
		case CaptureStatusOpen:
			if hasPaths {
				return CaptureStatusPendingSummary, nil
			}
			return CaptureStatusAbandoned, nil
		case CaptureStatusPendingSummary, CaptureStatusFinalized, CaptureStatusAbandoned:
			return status, nil
		}
	case captureActionAbandon:
		switch status {
		case CaptureStatusOpen, CaptureStatusPendingSummary:
			return CaptureStatusAbandoned, nil
		case CaptureStatusAbandoned:
			return status, nil
		}
	}
	return "", fmt.Errorf("%w: cannot apply action %d to Capture status %q", ErrInvalidState, action, status)
}

func (s *Service) StartCapture(ctx context.Context, request StartCaptureRequest) (Capture, error) {
	if request.RepositoryRoot == "" || request.StartCursor == "" {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, ErrInvalidState)
	}

	repository, err := s.ResolveRepository(ctx, request.RepositoryRoot)
	if err != nil {
		return Capture{}, err
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

	now := nowUTC()
	record := store.CaptureRecord{
		ID:             string(captureID),
		RepositoryID:   string(repository.ID),
		ConversationID: string(conversationID),
		Harness:        string(request.ConversationKey.Harness),
		ExternalID:     request.ConversationKey.ExternalID,
		WorktreeRoot:   repository.WorktreeRoot,
		Status:         string(CaptureStatusOpen),
		TranscriptRef:  request.TranscriptRef,
		StartCursor:    request.StartCursor,
		StartedAt:      now,
		LastSeenAt:     now,
	}
	err = s.database.WithTransaction(ctx, func(transaction *store.Tx) error {
		existingCaptureID, found, err := transaction.FindOpenCaptureID(
			ctx, string(conversationID), string(CaptureStatusOpen),
		)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("%w: Conversation already has open Capture %s", ErrConflict, existingCaptureID)
		}
		return transaction.InsertCapture(ctx, record)
	})
	if err != nil {
		return Capture{}, wrapError("start capture", request.RepositoryRoot, err)
	}
	return captureFromRecord(record), nil
}

func (s *Service) getOrCreateConversation(
	ctx context.Context,
	repositoryID RepositoryID,
	key ConversationKey,
	transcriptRef string,
) (ConversationID, error) {
	if key.Harness == "" || key.ExternalID == "" {
		return "", wrapError("get or create conversation", key.ExternalID, ErrInvalidState)
	}

	var conversationID ConversationID
	err := s.database.WithTransaction(ctx, func(transaction *store.Tx) error {
		id, found, err := transaction.FindConversationID(
			ctx, string(repositoryID), string(key.Harness), key.ExternalID,
		)
		if err != nil {
			return err
		}
		if found {
			conversationID = ConversationID(id)
			if transcriptRef == "" {
				return nil
			}
			return transaction.UpdateConversationTranscript(ctx, id, transcriptRef, nowUTC())
		}

		conversationID, err = newConversationID()
		if err != nil {
			return err
		}
		return transaction.InsertConversation(
			ctx, string(conversationID), string(repositoryID), string(key.Harness),
			key.ExternalID, transcriptRef, nowUTC(),
		)
	})
	if err != nil {
		return "", wrapError("get or create conversation", key.ExternalID, err)
	}
	return conversationID, nil
}

func (s *Service) GetCapture(ctx context.Context, captureID CaptureID) (Capture, error) {
	record, found, err := s.database.GetCapture(ctx, string(captureID))
	if err != nil {
		return Capture{}, wrapError("get capture", string(captureID), err)
	}
	if !found {
		return Capture{}, wrapError("get capture", string(captureID), ErrNotFound)
	}
	return captureFromRecord(record), nil
}

func (s *Service) RecordWrite(ctx context.Context, request RecordWriteRequest) error {
	err := s.database.WithTransaction(ctx, func(transaction *store.Tx) error {
		capture, found, err := transaction.GetCapture(ctx, string(request.CaptureID))
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if _, err := transitionCapture(CaptureStatus(capture.Status), captureActionRecordWrite, false); err != nil {
			return err
		}

		path, err := repopath.Normalize(capture.WorktreeRoot, request.Path)
		if err != nil {
			return err
		}
		now := nowUTC()
		if err := transaction.UpsertCapturePath(ctx, capture.ID, path, now); err != nil {
			return err
		}
		return transaction.UpdateCaptureLastSeen(ctx, capture.ID, now)
	})
	return wrapError("record write", string(request.CaptureID), err)
}

func (s *Service) ListPendingCaptures(ctx context.Context, query PendingCaptureQuery) ([]Capture, error) {
	if query.RepositoryRoot == "" {
		return nil, wrapError("list pending captures", query.RepositoryRoot, ErrInvalidState)
	}
	repository, err := s.ResolveRepository(ctx, query.RepositoryRoot)
	if err != nil {
		return nil, err
	}

	var harness, externalID *string
	if query.ConversationKey != nil {
		if query.ConversationKey.Harness == "" || query.ConversationKey.ExternalID == "" {
			return nil, wrapError("list pending captures", query.ConversationKey.ExternalID, ErrInvalidState)
		}
		harnessValue := string(query.ConversationKey.Harness)
		harness = &harnessValue
		externalID = &query.ConversationKey.ExternalID
	}
	records, err := s.database.ListPendingCaptures(
		ctx, string(repository.ID), string(CaptureStatusOpen),
		string(CaptureStatusPendingSummary), harness, externalID,
	)
	if err != nil {
		return nil, wrapError("list pending captures", query.RepositoryRoot, err)
	}
	captures := make([]Capture, len(records))
	for index, record := range records {
		captures[index] = captureFromRecord(record)
	}
	return captures, nil
}

func (s *Service) SealCapture(ctx context.Context, request SealCaptureRequest) (FinalizationDraft, error) {
	if request.EndCursor == "" {
		return FinalizationDraft{}, wrapError("seal capture", string(request.CaptureID), ErrInvalidState)
	}

	var draft FinalizationDraft
	err := s.database.WithTransaction(ctx, func(transaction *store.Tx) error {
		capture, found, err := transaction.GetCapture(ctx, string(request.CaptureID))
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		status := CaptureStatus(capture.Status)
		paths, err := transaction.CapturePaths(ctx, capture.ID)
		if err != nil {
			return err
		}
		nextStatus, err := transitionCapture(status, captureActionSeal, len(paths) > 0)
		if err != nil {
			return err
		}
		if status == CaptureStatusOpen {
			updated, err := transaction.SealCapture(
				ctx, capture.ID, string(CaptureStatusOpen), string(nextStatus), request.EndCursor, nowUTC(),
			)
			if err != nil {
				return err
			}
			if !updated {
				return fmt.Errorf("%w: Capture changed during sealing", ErrConflict)
			}
			if nextStatus == CaptureStatusAbandoned {
				if err := transaction.DeleteCaptureRawState(ctx, capture.ID); err != nil {
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
		if nextStatus == CaptureStatusFinalized {
			draft.EpisodeID = EpisodeID(capture.EpisodeID)
		}
		return nil
	})
	if err != nil {
		return FinalizationDraft{}, wrapError("seal capture", string(request.CaptureID), err)
	}
	return draft, nil
}

func (s *Service) AbandonCapture(ctx context.Context, captureID CaptureID) error {
	err := s.database.WithTransaction(ctx, func(transaction *store.Tx) error {
		capture, found, err := transaction.GetCapture(ctx, string(captureID))
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		status := CaptureStatus(capture.Status)
		nextStatus, err := transitionCapture(status, captureActionAbandon, false)
		if err != nil {
			return err
		}
		if err := transaction.DeleteCaptureRawState(ctx, capture.ID); err != nil {
			return err
		}
		if nextStatus == status {
			return nil
		}
		updated, err := transaction.AbandonCapture(
			ctx, capture.ID, string(status), string(nextStatus), nowUTC(),
		)
		if err != nil {
			return err
		}
		if !updated {
			return fmt.Errorf("%w: Capture changed during abandonment", ErrConflict)
		}
		return nil
	})
	return wrapError("abandon capture", string(captureID), err)
}
