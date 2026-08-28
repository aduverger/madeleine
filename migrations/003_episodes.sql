CREATE TABLE episodes (
    id TEXT PRIMARY KEY,
    source_capture_id TEXT NOT NULL UNIQUE REFERENCES captures(id),
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    harness TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    l1 TEXT NOT NULL,
    l2 TEXT NOT NULL,
    transcript_ref TEXT,
    start_cursor TEXT,
    end_cursor TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX episodes_repository_ended_id_idx
    ON episodes(repository_id, ended_at DESC, id DESC);

CREATE TABLE episode_files (
    episode_id TEXT NOT NULL REFERENCES episodes(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    path TEXT NOT NULL,
    PRIMARY KEY(episode_id, path)
);

CREATE INDEX episode_files_repository_path_episode_idx
    ON episode_files(repository_id, path, episode_id);
