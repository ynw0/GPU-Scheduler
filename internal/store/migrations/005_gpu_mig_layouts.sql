CREATE TABLE gpu_mig_layouts (
    gpu_uuid TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL,
    profiles_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
