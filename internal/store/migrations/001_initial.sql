PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('Viewer','User','TeamOperator','Admin','Auditor')),
    team_id TEXT REFERENCES teams(id),
    failed_logins INTEGER NOT NULL DEFAULT 0,
    locked_until TEXT,
    created_at TEXT NOT NULL,
    disabled_at TEXT
);

CREATE TABLE IF NOT EXISTS sessions (
    id_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS quotas (
    id TEXT PRIMARY KEY,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('team','user')),
    scope_id TEXT NOT NULL,
    gpu_limit INTEGER NOT NULL CHECK (gpu_limit >= 0),
    cpu_thread_limit INTEGER NOT NULL CHECK (cpu_thread_limit >= 0),
    memory_bytes_limit INTEGER NOT NULL CHECK (memory_bytes_limit >= 0),
    UNIQUE(scope_type, scope_id)
);

CREATE TABLE IF NOT EXISTS resource_pools (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    cpu_ids_json TEXT NOT NULL,
    gpu_uuids_json TEXT NOT NULL,
    numa_nodes_json TEXT NOT NULL,
    shared INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS compose_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    root_path TEXT NOT NULL,
    entry_file TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    owner_id TEXT NOT NULL REFERENCES users(id),
    team_id TEXT REFERENCES teams(id),
    state TEXT NOT NULL,
    priority TEXT NOT NULL CHECK (priority IN ('P0','P1','P2','P3')),
    spec_json TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    bypass_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(owner_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS job_attempts (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL,
    state TEXT NOT NULL,
    runtime_ref TEXT,
    effective_spec_id TEXT,
    created_at TEXT NOT NULL,
    finished_at TEXT,
    UNIQUE(job_id, attempt_number)
);

CREATE TABLE IF NOT EXISTS allocations (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    service_name TEXT NOT NULL DEFAULT '',
    resource_type TEXT NOT NULL CHECK (resource_type IN ('cpu','gpu','memory_node')),
    resource_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('RESERVED','ACTIVE','RELEASING','RELEASED')),
    created_at TEXT NOT NULL,
    released_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS allocations_exclusive_resource
ON allocations(resource_type, resource_id)
WHERE state IN ('RESERVED','ACTIVE','RELEASING');

CREATE TABLE IF NOT EXISTS effective_specs (
    id TEXT PRIMARY KEY,
    job_attempt_id TEXT NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
    source_digest TEXT NOT NULL,
    normalized_digest TEXT NOT NULL,
    encrypted_spec BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS discovered_containers (
    container_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    managed_job_id TEXT REFERENCES jobs(id),
    state TEXT NOT NULL,
    host_config_digest TEXT NOT NULL,
    cpu_unbounded INTEGER NOT NULL,
    gpu_unbounded INTEGER NOT NULL,
    observed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS job_events (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor_user_id TEXT REFERENCES users(id),
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    detail_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS metric_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    sampled_at TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS metric_snapshots_lookup
ON metric_snapshots(scope_type, scope_id, sampled_at);

CREATE TABLE IF NOT EXISTS metric_rollups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    UNIQUE(scope_type, scope_id, bucket_start)
);

CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    user_id TEXT REFERENCES users(id),
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    read_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id TEXT PRIMARY KEY,
    notification_id TEXT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('webhook','email')),
    attempt INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    next_attempt_at TEXT,
    last_error TEXT
);

