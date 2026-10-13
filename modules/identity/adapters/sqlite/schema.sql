CREATE TABLE IF NOT EXISTS identity_schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS auth_users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1))
) STRICT;
CREATE TABLE IF NOT EXISTS auth_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES auth_users(id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER
) STRICT;
CREATE INDEX IF NOT EXISTS auth_sessions_user ON auth_sessions(user_id);
CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
    token_hash TEXT PRIMARY KEY CHECK (length(token_hash) = 64),
    session_id TEXT NOT NULL REFERENCES auth_sessions(id),
    created_at INTEGER NOT NULL,
    used_at INTEGER
) STRICT;
CREATE INDEX IF NOT EXISTS auth_refresh_session ON auth_refresh_tokens(session_id);
INSERT INTO identity_schema_migrations(version, applied_at) VALUES (1, unixepoch()) ON CONFLICT(version) DO NOTHING;
