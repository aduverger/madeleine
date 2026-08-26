package madeleine_test

import (
	"testing"

	"github.com/aduverger/madeleine"
)

func TestPublicContractsAreImportable(t *testing.T) {
	t.Parallel()

	requests := []any{
		madeleine.StartCaptureRequest{},
		madeleine.RecordWriteRequest{},
		madeleine.PendingCaptureQuery{},
		madeleine.SealCaptureRequest{},
		madeleine.PublishEpisodeRequest{},
		madeleine.ContextRequest{},
		madeleine.EpisodeRequest{},
	}
	results := []any{
		madeleine.Repository{},
		madeleine.Capture{},
		madeleine.FinalizationDraft{},
		madeleine.Episode{},
		madeleine.FileContext{},
		madeleine.EpisodeDetail{},
	}
	if len(requests) != 7 || len(results) != 6 {
		t.Fatal("public contract declarations are incomplete")
	}
}
