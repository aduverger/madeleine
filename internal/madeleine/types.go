package madeleine

import (
	"time"

	"github.com/google/uuid"
)

type RepositoryID string
type ConversationID string
type CaptureID string
type EpisodeID string
type TranscriptID string

type Harness string

const HarnessPi Harness = "pi"

type Repository struct {
	ID           RepositoryID `json:"id,omitempty"`
	WorktreeRoot string       `json:"worktree_root"`
	GitCommonDir string       `json:"git_common_dir"`
	Origin       string       `json:"origin,omitempty"`
}

type ConversationKey struct {
	Harness    Harness `json:"harness"`
	ExternalID string  `json:"external_id"`
}

type CaptureStatus string

const (
	CaptureStatusOpen           CaptureStatus = "open"
	CaptureStatusPendingSummary CaptureStatus = "pending_summary"
	CaptureStatusFinalized      CaptureStatus = "finalized"
	CaptureStatusAbandoned      CaptureStatus = "abandoned"
)

type Options struct {
	Home string `json:"home,omitempty"`
}

type StartCaptureRequest struct {
	RepositoryRoot  string          `json:"repository_root"`
	ConversationKey ConversationKey `json:"conversation_key"`
	StartCursor     string          `json:"start_cursor"`
}

type RecordWriteRequest struct {
	CaptureID CaptureID `json:"capture_id"`
	Path      string    `json:"path"`
}

type PendingCaptureQuery struct {
	RepositoryRoot  string           `json:"repository_root"`
	ConversationKey *ConversationKey `json:"conversation_key,omitempty"`
}

type TranscriptEntryKind string

const (
	TranscriptEntryUser          TranscriptEntryKind = "user"
	TranscriptEntryAssistant     TranscriptEntryKind = "assistant"
	TranscriptEntryBranchSummary TranscriptEntryKind = "branch_summary"
	TranscriptEntryMutation      TranscriptEntryKind = "mutation"
)

type TranscriptEntry struct {
	Kind      TranscriptEntryKind `json:"kind"`
	Text      string              `json:"text,omitempty"`
	Operation string              `json:"operation,omitempty"`
	Path      string              `json:"path,omitempty"`
	Status    string              `json:"status,omitempty"`
	Error     string              `json:"error,omitempty"`
}

type TranscriptInput struct {
	FormatVersion int               `json:"format_version"`
	Entries       []TranscriptEntry `json:"entries"`
}

type SealCaptureRequest struct {
	CaptureID  CaptureID        `json:"capture_id"`
	EndCursor  string           `json:"end_cursor"`
	Transcript *TranscriptInput `json:"transcript,omitempty"`
}

type PublishEpisodeRequest struct {
	CaptureID       CaptureID `json:"capture_id"`
	L1              string    `json:"l1"`
	L2              string    `json:"l2"`
	CompactEvidence string    `json:"compact_evidence"`
}

type ContextRequest struct {
	RepositoryRoot string   `json:"repository_root"`
	Paths          []string `json:"paths"`
}

type EpisodeRequest struct {
	RepositoryRoot string    `json:"repository_root"`
	EpisodeID      EpisodeID `json:"episode_id"`
}

type TranscriptViewKind string

const (
	TranscriptViewCompact TranscriptViewKind = "compact"
	TranscriptViewRaw     TranscriptViewKind = "raw"
)

type TranscriptRequest struct {
	RepositoryRoot string             `json:"repository_root"`
	TranscriptID   TranscriptID       `json:"transcript_id"`
	View           TranscriptViewKind `json:"view"`
	Offset         int                `json:"offset,omitempty"`
}

type Capture struct {
	ID              CaptureID       `json:"id"`
	RepositoryID    RepositoryID    `json:"repository_id"`
	ConversationID  ConversationID  `json:"conversation_id"`
	ConversationKey ConversationKey `json:"conversation_key"`
	WorktreeRoot    string          `json:"worktree_root"`
	Status          CaptureStatus   `json:"status"`
	TranscriptID    TranscriptID    `json:"transcript_id,omitempty"`
	StartCursor     string          `json:"start_cursor"`
	EndCursor       string          `json:"end_cursor,omitempty"`
	StartedAt       time.Time       `json:"started_at"`
	EndedAt         *time.Time      `json:"ended_at,omitempty"`
	LastSeenAt      time.Time       `json:"last_seen_at"`
	EpisodeID       EpisodeID       `json:"episode_id,omitempty"`
}

type FinalizationDraft struct {
	CaptureID    CaptureID     `json:"capture_id"`
	TranscriptID TranscriptID  `json:"transcript_id,omitempty"`
	Status       CaptureStatus `json:"status"`
	Empty        bool          `json:"empty"`
	Paths        []string      `json:"paths"`
	EpisodeID    EpisodeID     `json:"episode_id,omitempty"`
}

type Episode struct {
	ID              EpisodeID       `json:"id"`
	CaptureID       CaptureID       `json:"capture_id"`
	RepositoryID    RepositoryID    `json:"repository_id"`
	ConversationID  ConversationID  `json:"conversation_id"`
	ConversationKey ConversationKey `json:"conversation_key"`
	Harness         Harness         `json:"harness"`
	Paths           []string        `json:"paths"`
	L1              string          `json:"l1"`
	L2              string          `json:"l2"`
	TranscriptID    TranscriptID    `json:"transcript_id"`
	StartedAt       time.Time       `json:"started_at"`
	EndedAt         time.Time       `json:"ended_at"`
	CreatedAt       time.Time       `json:"created_at"`
}

type EpisodeSummary struct {
	EpisodeID EpisodeID `json:"episode_id"`
	EndedAt   time.Time `json:"ended_at"`
	Harness   Harness   `json:"harness"`
	L1        string    `json:"l1"`
}

type FileContext struct {
	Path     string           `json:"path"`
	Episodes []EpisodeSummary `json:"episodes"`
}

type EpisodeDetail struct {
	EpisodeID       EpisodeID       `json:"episode_id"`
	ConversationID  ConversationID  `json:"conversation_id"`
	ConversationKey ConversationKey `json:"conversation_key"`
	Harness         Harness         `json:"harness"`
	Paths           []string        `json:"paths"`
	L1              string          `json:"l1"`
	L2              string          `json:"l2"`
	TranscriptID    TranscriptID    `json:"transcript_id"`
	StartedAt       time.Time       `json:"started_at"`
	EndedAt         time.Time       `json:"ended_at"`
}

type TranscriptView struct {
	TranscriptID TranscriptID       `json:"transcript_id"`
	View         TranscriptViewKind `json:"view"`
	Compact      string             `json:"compact,omitempty"`
	Entries      []TranscriptEntry  `json:"entries,omitempty"`
	NextOffset   *int               `json:"next_offset,omitempty"`
}

func newRepositoryID() (RepositoryID, error) {
	id, err := newUUIDv7()
	return RepositoryID(id), err
}

func newConversationID() (ConversationID, error) {
	id, err := newUUIDv7()
	return ConversationID(id), err
}

func newCaptureID() (CaptureID, error) {
	id, err := newUUIDv7()
	return CaptureID(id), err
}

func newEpisodeID() (EpisodeID, error) {
	id, err := newUUIDv7()
	return EpisodeID(id), err
}

func newTranscriptID() (TranscriptID, error) {
	id, err := newUUIDv7()
	return TranscriptID(id), err
}

func newUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
