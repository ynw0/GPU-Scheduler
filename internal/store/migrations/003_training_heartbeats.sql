DROP TABLE IF EXISTS training_heartbeats;

CREATE TABLE training_heartbeats (
    attempt_id TEXT NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
    service_name TEXT NOT NULL,
    sequence INTEGER NOT NULL DEFAULT 0,
    phase TEXT NOT NULL CHECK (phase IN ('TRAINING','CHECKPOINTING','EVALUATING')),
    epoch INTEGER,
    step INTEGER,
    progress REAL,
    reported_at TEXT NOT NULL,
    PRIMARY KEY (attempt_id, service_name)
);

ALTER TABLE job_attempts ADD COLUMN heartbeat_misses INTEGER NOT NULL DEFAULT 0;
