ALTER TABLE discovered_containers ADD COLUMN cpu_ids_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE discovered_containers ADD COLUMN gpu_uuids_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE discovered_containers ADD COLUMN priority TEXT NOT NULL DEFAULT 'P2';
ALTER TABLE discovered_containers ADD COLUMN priority_locked INTEGER NOT NULL DEFAULT 0;
ALTER TABLE discovered_containers ADD COLUMN last_training_at TEXT;
ALTER TABLE discovered_containers ADD COLUMN first_seen_at TEXT NOT NULL DEFAULT '';
