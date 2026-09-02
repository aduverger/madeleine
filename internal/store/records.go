package store

import "time"

type RepositoryRecord struct {
	ID string
}

type CaptureRecord struct {
	ID             string
	RepositoryID   string
	ConversationID string
	Harness        string
	ExternalID     string
	WorktreeRoot   string
	Status         string
	TranscriptID   string
	StartCursor    string
	EndCursor      string
	StartedAt      time.Time
	EndedAt        *time.Time
	LastSeenAt     time.Time
	EpisodeID      string
}

type TranscriptRecord struct {
	ID                string
	CaptureID         string
	RepositoryID      string
	ConversationID    string
	Harness           string
	FormatVersion     int
	SourceStartCursor string
	SourceEndCursor   string
	CompactText       *string
	CreatedAt         time.Time
	PublishedAt       *time.Time
}

type TranscriptEntryRecord struct {
	Position    int
	Kind        string
	ContentJSON string
}

type EpisodeRecord struct {
	ID             string
	CaptureID      string
	RepositoryID   string
	ConversationID string
	TranscriptID   string
	Harness        string
	ExternalID     string
	Paths          []string
	L1             string
	L2             string
	StartedAt      time.Time
	EndedAt        time.Time
	CreatedAt      time.Time
}

type EpisodeSummaryRecord struct {
	Path      string
	EpisodeID string
	EndedAt   time.Time
	Harness   string
	L1        string
}

func timestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimestamp(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
