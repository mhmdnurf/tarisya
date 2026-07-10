CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    hostname TEXT,
    agent_version TEXT,
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS servers_user_idx ON servers (user_id);

CREATE TABLE IF NOT EXISTS server_api_keys (
    id INTEGER PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    api_key_hash CHAR(64) NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    revoked_at INTEGER
);

CREATE TABLE IF NOT EXISTS metrics (
    id INTEGER PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    cpu_usage REAL NOT NULL,
    memory_usage REAL NOT NULL,
    disk_usage REAL NOT NULL,
    load_average REAL NOT NULL,
    uptime_seconds INTEGER NOT NULL,
    collected_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS metrics_server_collected_idx
    ON metrics (server_id, collected_at DESC);

CREATE TABLE IF NOT EXISTS metrics_5m (
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    bucket_start INTEGER NOT NULL,
    cpu_avg REAL NOT NULL, cpu_min REAL NOT NULL, cpu_max REAL NOT NULL,
    memory_avg REAL NOT NULL, memory_min REAL NOT NULL, memory_max REAL NOT NULL,
    disk_avg REAL NOT NULL, disk_min REAL NOT NULL, disk_max REAL NOT NULL,
    load_avg REAL NOT NULL, load_min REAL NOT NULL, load_max REAL NOT NULL,
    uptime_max INTEGER NOT NULL,
    sample_count INTEGER NOT NULL,
    PRIMARY KEY (server_id, bucket_start)
);

CREATE TABLE IF NOT EXISTS metrics_1h (
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    bucket_start INTEGER NOT NULL,
    cpu_avg REAL NOT NULL, cpu_min REAL NOT NULL, cpu_max REAL NOT NULL,
    memory_avg REAL NOT NULL, memory_min REAL NOT NULL, memory_max REAL NOT NULL,
    disk_avg REAL NOT NULL, disk_min REAL NOT NULL, disk_max REAL NOT NULL,
    load_avg REAL NOT NULL, load_min REAL NOT NULL, load_max REAL NOT NULL,
    uptime_max INTEGER NOT NULL,
    sample_count INTEGER NOT NULL,
    PRIMARY KEY (server_id, bucket_start)
);
