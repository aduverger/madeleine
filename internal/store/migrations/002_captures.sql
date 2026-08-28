CREATE TABLE captures (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    worktree_root TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('open', 'pending_summary', 'finalized', 'abandoned')),
    transcript_ref TEXT,
    start_cursor TEXT,
    end_cursor TEXT,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    last_seen_at TEXT NOT NULL,
    episode_id TEXT
);

CREATE UNIQUE INDEX captures_one_open_per_conversation_idx
    ON captures(conversation_id)
    WHERE status = 'open';

CREATE INDEX captures_repository_status_started_idx
    ON captures(repository_id, status, started_at, id);

CREATE INDEX captures_conversation_status_started_idx
    ON captures(conversation_id, status, started_at, id);

CREATE TABLE capture_paths (
    capture_id TEXT NOT NULL REFERENCES captures(id),
    path TEXT NOT NULL,
    source TEXT NOT NULL CHECK(source IN ('tool', 'git')),
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY(capture_id, path)
);
