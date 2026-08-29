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
			if capture.EpisodeID == "" {
				return fmt.Errorf("%w: finalized Capture has no Episode", ErrInvalidState)
			}
			record, found, err := transaction.GetEpisode(ctx, capture.RepositoryID, capture.EpisodeID)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("%w: finalized Capture Episode does not exist", ErrInvalidState)
			}
			episode = episodeFromRecord(record)
			if episode.L1 != l1 || episode.L2 != l2 {
				return fmt.Errorf("%w: Capture was published with different summaries", ErrConflict)
			}
			return nil
		}
		if CaptureStatus(capture.Status) != CaptureStatusPendingSummary {
			return fmt.Errorf("%w: Capture status is %q", ErrInvalidState, capture.Status)
		}

		paths, err := transaction.CapturePaths(ctx, capture.ID)
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
		record := store.EpisodeRecord{
			ID:             string(episodeID),
			CaptureID:      capture.ID,
			RepositoryID:   capture.RepositoryID,
			ConversationID: capture.ConversationID,
			Harness:        capture.Harness,
			ExternalID:     capture.ExternalID,
			Paths:          paths,
			L1:             l1,
			L2:             l2,
			TranscriptRef:  capture.TranscriptRef,
			StartCursor:    capture.StartCursor,
			EndCursor:      capture.EndCursor,
			StartedAt:      capture.StartedAt,
			EndedAt:        *capture.EndedAt,
			CreatedAt:      nowUTC(),
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
		if err := transaction.DeleteCaptureRawState(ctx, capture.ID); err != nil {
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
