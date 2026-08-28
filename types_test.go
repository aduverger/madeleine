package madeleine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
