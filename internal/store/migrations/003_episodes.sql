CREATE TABLE transcripts (
    id TEXT PRIMARY KEY,
    capture_id TEXT NOT NULL UNIQUE REFERENCES captures(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    harness TEXT NOT NULL,
    format_version INTEGER NOT NULL CHECK(format_version > 0),
    source_start_cursor TEXT NOT NULL,
    source_end_cursor TEXT NOT NULL,
    compact_text TEXT,
    created_at TEXT NOT NULL,
    published_at TEXT
);

CREATE INDEX transcripts_repository_id_idx
    ON transcripts(repository_id, id);

CREATE TABLE transcript_entries (
    transcript_id TEXT NOT NULL REFERENCES transcripts(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK(position >= 0),
    kind TEXT NOT NULL,
    content_json TEXT NOT NULL CHECK(json_valid(content_json)),
    PRIMARY KEY(transcript_id, position)
);

CREATE TABLE episodes (
    id TEXT PRIMARY KEY,
    source_capture_id TEXT NOT NULL UNIQUE REFERENCES captures(id),
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    transcript_id TEXT NOT NULL UNIQUE REFERENCES transcripts(id),
    harness TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    l1 TEXT NOT NULL,
    l2 TEXT NOT NULL,
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
