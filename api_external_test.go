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
		madeleine.EpisodeSummary{},
		madeleine.FileContext{},
		madeleine.EpisodeDetail{},
	}
	var _ func(context.Context, madeleine.Options) (*madeleine.Store, error) = madeleine.Open
	var _ func(*madeleine.Store, context.Context, string) (madeleine.Repository, error) = (*madeleine.Store).ResolveRepository
	var _ func(*madeleine.Store, context.Context, madeleine.StartCaptureRequest) (madeleine.Capture, error) = (*madeleine.Store).StartCapture
	var _ func(*madeleine.Store, context.Context, madeleine.CaptureID) (madeleine.Capture, error) = (*madeleine.Store).GetCapture
	var _ func(*madeleine.Store, context.Context, madeleine.RecordWriteRequest) error = (*madeleine.Store).RecordWrite
	var _ func(*madeleine.Store, context.Context, madeleine.PendingCaptureQuery) ([]madeleine.Capture, error) = (*madeleine.Store).ListPendingCaptures
	var _ func(*madeleine.Store, context.Context, madeleine.SealCaptureRequest) (madeleine.FinalizationDraft, error) = (*madeleine.Store).SealCapture
	var _ func(*madeleine.Store, context.Context, madeleine.PublishEpisodeRequest) (madeleine.Episode, error) = (*madeleine.Store).PublishEpisode
	var _ func(*madeleine.Store, context.Context, madeleine.CaptureID) error = (*madeleine.Store).AbandonCapture
	var _ func(*madeleine.Store, context.Context, madeleine.ContextRequest) ([]madeleine.FileContext, error) = (*madeleine.Store).ContextForPaths
	var _ func(*madeleine.Store, context.Context, madeleine.EpisodeRequest) (madeleine.EpisodeDetail, error) = (*madeleine.Store).GetEpisode
}
