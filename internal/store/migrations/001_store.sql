CREATE TABLE repositories (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

CREATE TABLE repository_aliases (
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    kind TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(kind, value)
);

CREATE INDEX repository_aliases_repository_id_idx
    ON repository_aliases(repository_id);

CREATE TABLE conversations (
    id TEXT PRIMARY KEY,
    repository_id TEXT NOT NULL REFERENCES repositories(id),
    harness TEXT NOT NULL,
    external_id TEXT NOT NULL,
    transcript_ref TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(repository_id, harness, external_id)
);

CREATE INDEX conversations_repository_id_idx
    ON conversations(repository_id);
