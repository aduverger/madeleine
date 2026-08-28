package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aduverger/madeleine/internal/gitstate"
)

type captureGitBaseline struct {
	WorktreeRoot string
	Status       CaptureStatus
	Snapshot     gitstate.Snapshot
}

func (s *Store) reconcileCaptureGitPaths(ctx context.Context, captureID CaptureID) ([]string, error) {
	baseline, err := s.loadCaptureGitBaseline(ctx, captureID)
	if err != nil {
		return nil, err
	}
	if baseline.Status != CaptureStatusOpen {
		return nil, nil
	}

	baselinePaths := make([]string, 0, len(baseline.Snapshot.Paths))
	for path := range baseline.Snapshot.Paths {
		baselinePaths = append(baselinePaths, path)
	}
	end, err := gitstate.Capture(ctx, baseline.WorktreeRoot, baselinePaths)
	if err != nil {
		return nil, err
	}
	return gitstate.Reconcile(ctx, baseline.WorktreeRoot, baseline.Snapshot, end)
}

func (s *Store) loadCaptureGitBaseline(ctx context.Context, captureID CaptureID) (captureGitBaseline, error) {
	baseline, err := scanCaptureGitBaseline(s.db.QueryRowContext(ctx, `
		SELECT worktree_root, status, git_start_head, git_start_head_exists
		FROM captures WHERE id = ?`, captureID))
	if errors.Is(err, sql.ErrNoRows) {
		return captureGitBaseline{}, ErrNotFound
	}
	if err != nil {
		return captureGitBaseline{}, err
	}
	baseline.Snapshot.Paths, err = s.loadCaptureGitBaselinePaths(ctx, captureID)
	if err != nil {
		return captureGitBaseline{}, err
	}
	return baseline, nil
}

func scanCaptureGitBaseline(scanner captureScanner) (captureGitBaseline, error) {
	var baseline captureGitBaseline
	var head sql.NullString
	if err := scanner.Scan(
		&baseline.WorktreeRoot, &baseline.Status, &head, &baseline.Snapshot.HeadExists,
	); err != nil {
		return captureGitBaseline{}, err
	}
	baseline.Snapshot.Head = head.String
	if baseline.Snapshot.HeadExists != head.Valid {
		return captureGitBaseline{}, fmt.Errorf("%w: Capture Git HEAD baseline is inconsistent", ErrInvalidState)
	}
	return baseline, nil
}

func (s *Store) loadCaptureGitBaselinePaths(
	ctx context.Context,
	captureID CaptureID,
) (map[string]gitstate.PathSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, porcelain_status, worktree_fingerprint, index_identity
		FROM capture_git_baseline_paths WHERE capture_id = ?`, captureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := make(map[string]gitstate.PathSnapshot)
	for rows.Next() {
		var path string
		var worktreeFingerprint, indexIdentity sql.NullString
		var snapshot gitstate.PathSnapshot
		if err := rows.Scan(
			&path, &snapshot.PorcelainStatus, &worktreeFingerprint, &indexIdentity,
		); err != nil {
			return nil, err
		}
		if !worktreeFingerprint.Valid {
			return nil, fmt.Errorf("%w: Capture Git path %q has no fingerprint", ErrInvalidState, path)
		}
		snapshot.WorktreeFingerprint = worktreeFingerprint.String
		snapshot.IndexIdentity = indexIdentity.String
		paths[path] = snapshot
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return paths, nil
}
