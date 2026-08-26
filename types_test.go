package madeleine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGeneratedIDsAreUUIDv7(t *testing.T) {
	t.Parallel()

	generators := []struct {
		name     string
		generate func() (string, error)
	}{
		{"repository", func() (string, error) { value, err := newRepositoryID(); return string(value), err }},
		{"conversation", func() (string, error) { value, err := newConversationID(); return string(value), err }},
		{"capture", func() (string, error) { value, err := newCaptureID(); return string(value), err }},
		{"episode", func() (string, error) { value, err := newEpisodeID(); return string(value), err }},
	}

	seen := make(map[string]bool, len(generators))
	for _, test := range generators {
		t.Run(test.name, func(t *testing.T) {
			value, err := test.generate()
			if err != nil {
				t.Fatalf("generate ID: %v", err)
			}
			parsed, err := uuid.Parse(value)
			if err != nil {
				t.Fatalf("parse generated ID %q: %v", value, err)
			}
			if parsed.Version() != 7 {
				t.Fatalf("ID version = %d, want 7", parsed.Version())
			}
			if seen[value] {
				t.Fatalf("duplicate generated ID %q", value)
			}
			seen[value] = true
		})
	}
}

func TestPublicTimestampJSONUsesUTCRFC3339Nano(t *testing.T) {
	t.Parallel()

	endedAt := time.Date(2026, time.January, 2, 3, 4, 5, 123456789, time.UTC)
	encoded, err := json.Marshal(EpisodeSummary{EndedAt: endedAt})
	if err != nil {
		t.Fatalf("marshal timestamp: %v", err)
	}
	if !strings.Contains(string(encoded), `"ended_at":"2026-01-02T03:04:05.123456789Z"`) {
		t.Fatalf("JSON = %s, want UTC RFC3339Nano timestamp", encoded)
	}
}

func TestDomainConstants(t *testing.T) {
	t.Parallel()

	if HarnessPi != "pi" {
		t.Fatalf("HarnessPi = %q, want pi", HarnessPi)
	}
	got := []CaptureStatus{
		CaptureStatusOpen,
		CaptureStatusPendingSummary,
		CaptureStatusFinalized,
		CaptureStatusAbandoned,
	}
	want := []CaptureStatus{"open", "pending_summary", "finalized", "abandoned"}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("status %d = %q, want %q", index, got[index], want[index])
		}
	}
}
