package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (tx *Tx) InsertGitBaselinePaths(ctx context.Context, captureID string, paths []GitPathRecord) error {
	for _, path := range paths {
		indexIdentity := sql.NullString{String: path.IndexIdentity, Valid: path.IndexIdentity != ""}
		if _, err := tx.tx.ExecContext(ctx, `
			INSERT INTO capture_git_baseline_paths(
				capture_id, path, porcelain_status, worktree_fingerprint, index_identity
			) VALUES (?, ?, ?, ?, ?)`,
			captureID, path.Path, path.PorcelainStatus,
			path.WorktreeFingerprint, indexIdentity); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) GetGitBaseline(ctx context.Context, captureID string) (GitBaselineRecord, bool, error) {
	var baseline GitBaselineRecord
	var head sql.NullString
	err := db.db.QueryRowContext(ctx, `
		SELECT worktree_root, status, git_start_head, git_start_head_exists
		FROM captures WHERE id = ?`, captureID).Scan(
		&baseline.WorktreeRoot, &baseline.Status, &head, &baseline.HeadExists,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return GitBaselineRecord{}, false, nil
	}
	if err != nil {
		return GitBaselineRecord{}, false, err
	}
	baseline.Head = head.String
	if baseline.HeadExists != head.Valid {
		return GitBaselineRecord{}, false, fmt.Errorf("Capture Git HEAD baseline is inconsistent")
	}

	rows, err := db.db.QueryContext(ctx, `
		SELECT path, porcelain_status, worktree_fingerprint, index_identity
		FROM capture_git_baseline_paths WHERE capture_id = ?`, captureID)
	if err != nil {
		return GitBaselineRecord{}, false, err
	}
	defer rows.Close()

	baseline.Paths = make([]GitPathRecord, 0)
	for rows.Next() {
		var path GitPathRecord
		var worktreeFingerprint, indexIdentity sql.NullString
		if err := rows.Scan(
			&path.Path, &path.PorcelainStatus, &worktreeFingerprint, &indexIdentity,
		); err != nil {
			return GitBaselineRecord{}, false, err
		}
		if !worktreeFingerprint.Valid {
			return GitBaselineRecord{}, false, fmt.Errorf("Capture Git path %q has no fingerprint", path.Path)
		}
		path.WorktreeFingerprint = worktreeFingerprint.String
		path.IndexIdentity = indexIdentity.String
		baseline.Paths = append(baseline.Paths, path)
	}
	if err := rows.Err(); err != nil {
		return GitBaselineRecord{}, false, err
	}
	return baseline, true, nil
}
