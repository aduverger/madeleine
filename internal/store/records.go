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
	TranscriptRef  string
	StartCursor    string
	EndCursor      string
	StartedAt      time.Time
	EndedAt        *time.Time
	LastSeenAt     time.Time
	EpisodeID      string
}

type EpisodeRecord struct {
	ID             string
	CaptureID      string
	RepositoryID   string
	ConversationID string
	Harness        string
	ExternalID     string
	Paths          []string
	L1             string
	L2             string
	TranscriptRef  string
	StartCursor    string
	EndCursor      string
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

type GitPathRecord struct {
	Path                string
	PorcelainStatus     string
	WorktreeFingerprint string
	IndexIdentity       string
}

type GitBaselineRecord struct {
	WorktreeRoot string
	Status       string
	Head         string
	HeadExists   bool
	Paths        []GitPathRecord
}

func timestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimestamp(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
