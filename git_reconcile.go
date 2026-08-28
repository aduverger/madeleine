package madeleine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

type captureGitBaseline struct {
	WorktreeRoot string
	Status       CaptureStatus
	Head         string
	HeadExists   bool
	Paths        map[string]gitPathSnapshot
}

func (s *Store) reconcileCaptureGitPaths(ctx context.Context, captureID CaptureID) ([]string, error) {
	baseline, err := s.loadCaptureGitBaseline(ctx, captureID)
	if err != nil {
		return nil, err
	}
	if baseline.Status != CaptureStatusOpen {
		return nil, nil
	}

	baselinePaths := make([]string, 0, len(baseline.Paths))
	for path := range baseline.Paths {
		baselinePaths = append(baselinePaths, path)
	}
	end, err := captureGitSnapshot(ctx, baseline.WorktreeRoot, baselinePaths)
	if err != nil {
		return nil, err
	}

	finalPaths := make(map[string]struct{})
	if baseline.HeadExists && end.HeadExists && baseline.Head != end.Head {
		paths, err := changedPathsBetweenHeads(ctx, baseline.WorktreeRoot, baseline.Head, end.Head)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			finalPaths[path] = struct{}{}
		}
	}
	for path := range end.Paths {
		if _, dirtyAtStart := baseline.Paths[path]; !dirtyAtStart {
			finalPaths[path] = struct{}{}
		}
	}
	for path, startState := range baseline.Paths {
		if end.Paths[path] != startState {
			finalPaths[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(finalPaths))
	for path := range finalPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *Store) loadCaptureGitBaseline(ctx context.Context, captureID CaptureID) (captureGitBaseline, error) {
	var baseline captureGitBaseline
	var head sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT worktree_root, status, git_start_head, git_start_head_exists
		FROM captures WHERE id = ?`, captureID,
	).Scan(&baseline.WorktreeRoot, &baseline.Status, &head, &baseline.HeadExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return captureGitBaseline{}, ErrNotFound
		}
		return captureGitBaseline{}, err
	}
	baseline.Head = head.String
	if baseline.HeadExists != head.Valid {
		return captureGitBaseline{}, fmt.Errorf("%w: Capture Git HEAD baseline is inconsistent", ErrInvalidState)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT path, porcelain_status, worktree_fingerprint, index_identity
		FROM capture_git_baseline_paths WHERE capture_id = ?`, captureID)
	if err != nil {
		return captureGitBaseline{}, err
	}
	defer rows.Close()

	baseline.Paths = make(map[string]gitPathSnapshot)
	for rows.Next() {
		var path string
		var worktreeFingerprint, indexIdentity sql.NullString
		var snapshot gitPathSnapshot
		if err := rows.Scan(
			&path, &snapshot.PorcelainStatus, &worktreeFingerprint, &indexIdentity,
		); err != nil {
			return captureGitBaseline{}, err
		}
		if !worktreeFingerprint.Valid {
			return captureGitBaseline{}, fmt.Errorf("%w: Capture Git path %q has no fingerprint", ErrInvalidState, path)
		}
		snapshot.WorktreeFingerprint = worktreeFingerprint.String
		snapshot.IndexIdentity = indexIdentity.String
		baseline.Paths[path] = snapshot
	}
	if err := rows.Err(); err != nil {
		return captureGitBaseline{}, err
	}
	return baseline, nil
}

func changedPathsBetweenHeads(ctx context.Context, worktreeRoot, startHead, endHead string) ([]string, error) {
	output, err := runGitObservation(ctx, worktreeRoot,
		"diff", "--name-only", "-z", "--no-renames", startHead+".."+endHead, "--")
	if err != nil {
		return nil, fmt.Errorf("observe Git commit range: %w", err)
	}

	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for _, record := range splitNullRecords(output) {
		path, err := normalizeRepositoryPath(worktreeRoot, string(record))
		if err != nil {
			return nil, fmt.Errorf("normalize Git diff path %q: %w", record, err)
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}
