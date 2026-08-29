package madeleine

import (
	"context"
	"time"

	"github.com/aduverger/madeleine/internal/store"
)

type Service struct {
	database *store.DB
}

func CheckDataDirectory(options Options) error {
	return store.CheckHomeAccess(options.Home)
}

func Open(ctx context.Context, options Options) (*Service, error) {
	database, err := store.Open(ctx, options.Home)
	if err != nil {
		return nil, err
	}
	return &Service{database: database}, nil
}

func (s *Service) Close() error {
	return s.database.Close()
}

func (s *Service) SchemaVersion(ctx context.Context) (int, error) {
	version, err := s.database.SchemaVersion(ctx)
	if err != nil {
		return 0, wrapError("get schema version", "", err)
	}
	return version, nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func captureFromRecord(record store.CaptureRecord) Capture {
	return Capture{
		ID:              CaptureID(record.ID),
		RepositoryID:    RepositoryID(record.RepositoryID),
		ConversationID:  ConversationID(record.ConversationID),
		ConversationKey: ConversationKey{Harness: Harness(record.Harness), ExternalID: record.ExternalID},
		WorktreeRoot:    record.WorktreeRoot,
		Status:          CaptureStatus(record.Status),
		TranscriptID:    TranscriptID(record.TranscriptID),
		StartCursor:     record.StartCursor,
		EndCursor:       record.EndCursor,
		StartedAt:       record.StartedAt,
		EndedAt:         record.EndedAt,
		LastSeenAt:      record.LastSeenAt,
		EpisodeID:       EpisodeID(record.EpisodeID),
	}
}

func episodeFromRecord(record store.EpisodeRecord) Episode {
	return Episode{
		ID:              EpisodeID(record.ID),
		CaptureID:       CaptureID(record.CaptureID),
		RepositoryID:    RepositoryID(record.RepositoryID),
		ConversationID:  ConversationID(record.ConversationID),
		ConversationKey: ConversationKey{Harness: Harness(record.Harness), ExternalID: record.ExternalID},
		Harness:         Harness(record.Harness),
		Paths:           record.Paths,
		L1:              record.L1,
		L2:              record.L2,
		TranscriptID:    TranscriptID(record.TranscriptID),
		StartedAt:       record.StartedAt,
		EndedAt:         record.EndedAt,
		CreatedAt:       record.CreatedAt,
	}
}
