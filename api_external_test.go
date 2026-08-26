package madeleine_test

import (
	"testing"

	"github.com/aduverger/madeleine"
)

func TestPublicContractsAreImportable(t *testing.T) {
	t.Parallel()

	_ = []any{
		madeleine.StartCaptureRequest{},
		madeleine.RecordWriteRequest{},
		madeleine.PendingCaptureQuery{},
		madeleine.SealCaptureRequest{},
		madeleine.PublishEpisodeRequest{},
		madeleine.ContextRequest{},
		madeleine.EpisodeRequest{},
		madeleine.Repository{},
		madeleine.Capture{},
		madeleine.FinalizationDraft{},
		madeleine.Episode{},
		madeleine.FileContext{},
		madeleine.EpisodeDetail{},
	}
}
