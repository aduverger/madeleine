package madeleine

import (
	"context"
	"fmt"

	"github.com/aduverger/madeleine/internal/gitstate"
)

func (s *Service) reconcileCaptureGitPaths(ctx context.Context, captureID CaptureID) ([]string, error) {
	baseline, found, err := s.database.GetGitBaseline(ctx, string(captureID))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNotFound
	}
	if CaptureStatus(baseline.Status) != CaptureStatusOpen {
		return nil, nil
	}

	startPaths := make(map[string]gitstate.PathSnapshot, len(baseline.Paths))
	baselinePaths := make([]string, 0, len(baseline.Paths))
	for _, path := range baseline.Paths {
		baselinePaths = append(baselinePaths, path.Path)
		startPaths[path.Path] = gitstate.PathSnapshot{
			PorcelainStatus:     path.PorcelainStatus,
			WorktreeFingerprint: path.WorktreeFingerprint,
			IndexIdentity:       path.IndexIdentity,
		}
	}
	start := gitstate.Snapshot{
		Head:       baseline.Head,
		HeadExists: baseline.HeadExists,
		Paths:      startPaths,
	}
	end, err := gitstate.Capture(ctx, baseline.WorktreeRoot, baselinePaths)
	if err != nil {
		return nil, err
	}
	paths, err := gitstate.Reconcile(ctx, baseline.WorktreeRoot, start, end)
	if err != nil {
		return nil, fmt.Errorf("reconcile Capture Git state: %w", err)
	}
	return paths, nil
}
