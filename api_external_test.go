package madeleine_test

import (
	"context"
	"testing"

	"github.com/aduverger/madeleine"
)

func TestPublicContractsAreImportable(t *testing.T) {
	t.Parallel()

	_ = []any{
		madeleine.Options{},
		(*madeleine.Store)(nil),
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
	var _ func(context.Context, madeleine.Options) (*madeleine.Store, error) = madeleine.Open
	var _ func(*madeleine.Store, context.Context, string) (madeleine.Repository, error) = (*madeleine.Store).ResolveRepository
}
