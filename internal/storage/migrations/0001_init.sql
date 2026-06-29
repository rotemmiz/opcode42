-- Opcode42 canonical schema (plan 01 §5). Idempotent DDL: re-run safely on every
-- startup. The JSON blob columns (message.data / part.data) carry opencode's
-- MessageV2 wire shapes verbatim so responses marshal without transformation.

CREATE TABLE IF NOT EXISTS project (
    id           TEXT PRIMARY KEY,
    worktree     TEXT NOT NULL,
    name         TEXT,
    time_created INTEGER NOT NULL,
    time_updated INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS session (
    id                 TEXT PRIMARY KEY,
    project_id         TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    slug               TEXT NOT NULL,
    directory          TEXT NOT NULL,
    path               TEXT,
    parent_id          TEXT,
    title              TEXT NOT NULL DEFAULT '',
    agent              TEXT,
    model_id           TEXT,
    provider_id        TEXT,
    version            TEXT NOT NULL DEFAULT '',
    cost               REAL NOT NULL DEFAULT 0,
    tokens_input       INTEGER NOT NULL DEFAULT 0,
    tokens_output      INTEGER NOT NULL DEFAULT 0,
    tokens_reasoning   INTEGER NOT NULL DEFAULT 0,
    tokens_cache_read  INTEGER NOT NULL DEFAULT 0,
    tokens_cache_write INTEGER NOT NULL DEFAULT 0,
    time_created       INTEGER NOT NULL,
    time_updated       INTEGER NOT NULL,
    time_archived      INTEGER
);

CREATE INDEX IF NOT EXISTS session_project_idx ON session(project_id);
-- Covers the default list order (ORDER BY time_updated DESC, id DESC).
CREATE INDEX IF NOT EXISTS session_time_idx ON session(time_updated, id);

CREATE TABLE IF NOT EXISTS message (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES session(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    data         TEXT NOT NULL,
    time_created INTEGER NOT NULL,
    time_updated INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS message_session_time_idx ON message(session_id, time_created, id);

CREATE TABLE IF NOT EXISTS part (
    id           TEXT PRIMARY KEY,
    message_id   TEXT NOT NULL REFERENCES message(id) ON DELETE CASCADE,
    session_id   TEXT NOT NULL,
    type         TEXT NOT NULL,
    data         TEXT NOT NULL,
    time_created INTEGER NOT NULL,
    time_updated INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS part_message_idx ON part(message_id, id);
CREATE INDEX IF NOT EXISTS part_session_idx ON part(session_id);
