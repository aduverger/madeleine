package madeleine

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aduverger/madeleine/internal/store"
)

const maxL1Characters = 400

func validateEpisodeSummaries(l1, l2 string) (string, string, error) {
	l1 = strings.TrimSpace(l1)
	l2 = strings.TrimSpace(l2)
	if l1 == "" || l2 == "" {
		return "", "", fmt.Errorf("%w: Episode summaries must not be empty", ErrInvalidState)
	}
	if utf8.RuneCountInString(l1) > maxL1Characters {
		return "", "", fmt.Errorf("%w: Episode L1 exceeds %d Unicode characters", ErrInvalidState, maxL1Characters)
	}
	return l1, l2, nil
}

func (s *Service) PublishEpisode(ctx context.Context, request PublishEpisodeRequest) (Episode, error) {
	l1, l2, err := validateEpisodeSummaries(request.L1, request.L2)
	if err != nil {
		return Episode{}, wrapError("publish Episode", string(request.CaptureID), err)
	}
	if strings.TrimSpace(request.CompactEvidence) == "" {
		err := fmt.Errorf("%w: Episode compact evidence must not be empty", ErrInvalidState)
		return Episode{}, wrapError("publish Episode", string(request.CaptureID), err)
	}

	var episode Episode
	err = s.database.WithTransaction(ctx, func(transaction *store.Tx) error {
		capture, found, err := transaction.GetCapture(ctx, string(request.CaptureID))
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}

		if CaptureStatus(capture.Status) == CaptureStatusFinalized {
			episode, err = publishedEpisodeForRetry(
				ctx, transaction, capture, l1, l2, request.CompactEvidence,
			)
			return err
		}
		if CaptureStatus(capture.Status) != CaptureStatusPendingSummary {
			return fmt.Errorf("%w: Capture status is %q", ErrInvalidState, capture.Status)
		}

		paths, err := transaction.CapturePaths(ctx, capture.ID)
		if err != nil {
			return err
		}
		transcript, found, err := transaction.GetTranscriptByCapture(ctx, capture.ID)
		if err != nil {
			return err
		}
		if len(paths) == 0 || capture.EndedAt == nil || capture.TranscriptID == "" ||
			!found || transcript.ID != capture.TranscriptID || transcript.CompactText != nil {
			return fmt.Errorf("%w: pending Capture has incomplete finalization data", ErrInvalidState)
		}

		episodeID, err := newEpisodeID()
		if err != nil {
			return err
		}
		now := nowUTC()
		record := store.EpisodeRecord{
			ID:             string(episodeID),
			CaptureID:      capture.ID,
			RepositoryID:   capture.RepositoryID,
			ConversationID: capture.ConversationID,
			TranscriptID:   capture.TranscriptID,
			Harness:        capture.Harness,
			ExternalID:     capture.ExternalID,
			Paths:          paths,
			L1:             l1,
			L2:             l2,
			StartedAt:      capture.StartedAt,
			EndedAt:        *capture.EndedAt,
			CreatedAt:      now,
		}
		published, err := transaction.PublishTranscript(ctx, capture.TranscriptID, request.CompactEvidence, now)
		if err != nil {
			return err
		}
		if !published {
			return fmt.Errorf("%w: Transcript changed during publication", ErrConflict)
		}
		if err := transaction.InsertEpisode(ctx, record); err != nil {
			return err
		}
		if err := transaction.InsertEpisodePaths(ctx, record.ID, record.RepositoryID, paths); err != nil {
			return err
		}
		updated, err := transaction.FinalizeCapture(
			ctx, capture.ID, string(CaptureStatusPendingSummary),
			string(CaptureStatusFinalized), record.ID,
		)
		if err != nil {
			return err
		}
		if !updated {
			return fmt.Errorf("%w: Capture changed during publication", ErrConflict)
		}
		if err := transaction.DeleteCapturePaths(ctx, capture.ID); err != nil {
			return err
		}
		episode = episodeFromRecord(record)
		return nil
	})
	if err != nil {
		return Episode{}, wrapError("publish Episode", string(request.CaptureID), err)
	}
	return episode, nil
}

func publishedEpisodeForRetry(
	ctx context.Context,
	transaction *store.Tx,
	capture store.CaptureRecord,
	l1, l2, compactEvidence string,
) (Episode, error) {
	if capture.EpisodeID == "" {
		return Episode{}, fmt.Errorf("%w: finalized Capture has no Episode", ErrInvalidState)
	}
	record, found, err := transaction.GetEpisode(ctx, capture.RepositoryID, capture.EpisodeID)
	if err != nil {
		return Episode{}, err
	}
	if !found {
		return Episode{}, fmt.Errorf("%w: finalized Capture Episode does not exist", ErrInvalidState)
	}

	episode := episodeFromRecord(record)
	transcript, found, err := transaction.GetTranscriptByCapture(ctx, capture.ID)
	if err != nil {
		return Episode{}, err
	}
	if !found || transcript.CompactText == nil {
		return Episode{}, fmt.Errorf("%w: finalized Capture has no published Transcript", ErrInvalidState)
	}
	if episode.L1 != l1 || episode.L2 != l2 || *transcript.CompactText != compactEvidence {
		return Episode{}, fmt.Errorf("%w: Capture was published with different summaries or evidence", ErrConflict)
	}
	return episode, nil
}
