CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    hostname TEXT,
    agent_version TEXT,
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS hostname TEXT;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS agent_version TEXT;

CREATE INDEX IF NOT EXISTS servers_user_idx ON servers (user_id);

CREATE TABLE IF NOT EXISTS server_api_keys (
    id BIGSERIAL PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    api_key_hash CHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS metrics (
    id BIGSERIAL PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    cpu_usage DOUBLE PRECISION NOT NULL,
    memory_usage DOUBLE PRECISION NOT NULL,
    disk_usage DOUBLE PRECISION NOT NULL,
    load_average DOUBLE PRECISION NOT NULL,
    uptime_seconds BIGINT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS metrics_server_collected_idx
    ON metrics (server_id, collected_at DESC);
