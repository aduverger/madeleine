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
	return reconcileGitSnapshots(ctx, baseline, end)
}

func reconcileGitSnapshots(ctx context.Context, baseline captureGitBaseline, end gitSnapshot) ([]string, error) {
	finalPaths := make(map[string]struct{})
	committedPaths, err := changedCommittedPaths(ctx, baseline, end)
	if err != nil {
		return nil, err
	}
	for _, path := range committedPaths {
		finalPaths[path] = struct{}{}
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
	baseline, err := scanCaptureGitBaseline(s.db.QueryRowContext(ctx, `
		SELECT worktree_root, status, git_start_head, git_start_head_exists
		FROM captures WHERE id = ?`, captureID))
	if errors.Is(err, sql.ErrNoRows) {
		return captureGitBaseline{}, ErrNotFound
	}
	if err != nil {
		return captureGitBaseline{}, err
	}
	baseline.Paths, err = s.loadCaptureGitBaselinePaths(ctx, captureID)
	if err != nil {
		return captureGitBaseline{}, err
	}
	return baseline, nil
}

func scanCaptureGitBaseline(scanner captureScanner) (captureGitBaseline, error) {
	var baseline captureGitBaseline
	var head sql.NullString
	if err := scanner.Scan(
		&baseline.WorktreeRoot, &baseline.Status, &head, &baseline.HeadExists,
	); err != nil {
		return captureGitBaseline{}, err
	}
	baseline.Head = head.String
	if baseline.HeadExists != head.Valid {
		return captureGitBaseline{}, fmt.Errorf("%w: Capture Git HEAD baseline is inconsistent", ErrInvalidState)
	}
	return baseline, nil
}

func (s *Store) loadCaptureGitBaselinePaths(
	ctx context.Context,
	captureID CaptureID,
) (map[string]gitPathSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, porcelain_status, worktree_fingerprint, index_identity
		FROM capture_git_baseline_paths WHERE capture_id = ?`, captureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := make(map[string]gitPathSnapshot)
	for rows.Next() {
		var path string
		var worktreeFingerprint, indexIdentity sql.NullString
		var snapshot gitPathSnapshot
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

func changedCommittedPaths(ctx context.Context, baseline captureGitBaseline, end gitSnapshot) ([]string, error) {
	switch {
	case baseline.HeadExists && end.HeadExists:
		if baseline.Head == end.Head {
			return nil, nil
		}
		return changedPathsBetweenHeads(ctx, baseline.WorktreeRoot, baseline.Head, end.Head)
	case baseline.HeadExists:
		return pathsInGitTree(ctx, baseline.WorktreeRoot, baseline.Head)
	case end.HeadExists:
		return pathsInGitTree(ctx, baseline.WorktreeRoot, end.Head)
	default:
		return nil, nil
	}
}

func changedPathsBetweenHeads(ctx context.Context, worktreeRoot, startHead, endHead string) ([]string, error) {
	output, err := runGitObservation(ctx, worktreeRoot,
		"diff", "--name-only", "-z", "--no-renames", startHead+".."+endHead, "--")
	if err != nil {
		return nil, fmt.Errorf("observe Git commit range: %w", err)
	}
	return normalizeGitPathList(worktreeRoot, "diff", output)
}

func pathsInGitTree(ctx context.Context, worktreeRoot, head string) ([]string, error) {
	output, err := runGitObservation(ctx, worktreeRoot, "ls-tree", "-r", "-z", "--name-only", head)
	if err != nil {
		return nil, fmt.Errorf("observe Git tree: %w", err)
	}
	return normalizeGitPathList(worktreeRoot, "tree", output)
}

func normalizeGitPathList(worktreeRoot, source string, output []byte) ([]string, error) {
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for _, record := range splitNullRecords(output) {
		path, err := normalizeRepositoryPath(worktreeRoot, string(record))
		if err != nil {
			return nil, fmt.Errorf("normalize Git %s path %q: %w", source, record, err)
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
