ALTER TABLE captures ADD COLUMN git_start_head TEXT;
ALTER TABLE captures ADD COLUMN git_start_head_exists INTEGER NOT NULL DEFAULT 0
    CHECK(git_start_head_exists IN (0, 1));

CREATE TABLE capture_git_baseline_paths (
    capture_id TEXT NOT NULL REFERENCES captures(id),
    path TEXT NOT NULL,
    porcelain_status TEXT NOT NULL,
    worktree_fingerprint TEXT,
    index_identity TEXT,
    PRIMARY KEY(capture_id, path)
);
