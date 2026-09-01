package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"gpu-scheduler/internal/domain"
)

func (s *Store) ListDiscoveredContainers(ctx context.Context) ([]domain.DiscoveredContainer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT container_id,name,image,COALESCE(managed_job_id,''),state,cpu_unbounded,gpu_unbounded,
		cpu_ids_json,gpu_uuids_json,priority,priority_locked,last_training_at,first_seen_at,observed_at
		FROM discovered_containers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.DiscoveredContainer
	for rows.Next() {
		item, err := scanDiscovered(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetDiscoveredContainer(ctx context.Context, containerID string) (domain.DiscoveredContainer, error) {
	row := s.db.QueryRowContext(ctx, `SELECT container_id,name,image,COALESCE(managed_job_id,''),state,cpu_unbounded,gpu_unbounded,
		cpu_ids_json,gpu_uuids_json,priority,priority_locked,last_training_at,first_seen_at,observed_at
		FROM discovered_containers WHERE container_id=?`, containerID)
	return scanDiscovered(row)
}

func (s *Store) UpsertDiscoveredContainer(ctx context.Context, item domain.DiscoveredContainer) error {
	cpuJSON, _ := json.Marshal(item.CPUIds)
	gpuJSON, _ := json.Marshal(item.GPUUUIDs)
	locked := 0
	if item.PriorityLocked {
		locked = 1
	}
	cpuFlag, gpuFlag := 0, 0
	if item.CPUUnbounded {
		cpuFlag = 1
	}
	if item.GPUUnbounded {
		gpuFlag = 1
	}
	var lastTraining any
	if item.LastTrainingAt != nil {
		lastTraining = item.LastTrainingAt.UTC().Format(time.RFC3339Nano)
	}
	managed := item.ManagedJobID
	if managed == "" {
		managed = ""
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO discovered_containers(
		container_id,name,image,managed_job_id,state,host_config_digest,cpu_unbounded,gpu_unbounded,observed_at,
		cpu_ids_json,gpu_uuids_json,priority,priority_locked,last_training_at,first_seen_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(container_id) DO UPDATE SET
			name=excluded.name, image=excluded.image, managed_job_id=excluded.managed_job_id, state=excluded.state,
			cpu_unbounded=excluded.cpu_unbounded, gpu_unbounded=excluded.gpu_unbounded, observed_at=excluded.observed_at,
			cpu_ids_json=excluded.cpu_ids_json, gpu_uuids_json=excluded.gpu_uuids_json,
			priority=excluded.priority, priority_locked=excluded.priority_locked,
			last_training_at=excluded.last_training_at, first_seen_at=excluded.first_seen_at`,
		item.ContainerID, item.Name, item.Image, nullText(managed), item.State, "live", cpuFlag, gpuFlag,
		item.ObservedAt.UTC().Format(time.RFC3339Nano), string(cpuJSON), string(gpuJSON), string(item.Priority), locked,
		lastTraining, item.FirstSeenAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) UpdateDiscoveredPriority(ctx context.Context, containerID string, priority domain.Priority, locked bool) error {
	flag := 0
	if locked {
		flag = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE discovered_containers SET priority=?, priority_locked=? WHERE container_id=?`,
		string(priority), flag, containerID)
	return err
}

func scanDiscovered(row scanner) (domain.DiscoveredContainer, error) {
	var item domain.DiscoveredContainer
	var cpuFlag, gpuFlag, locked int
	var lastTraining, firstSeen, observed sql.NullString
	var cpuJSON, gpuJSON string
	if err := row.Scan(&item.ContainerID, &item.Name, &item.Image, &item.ManagedJobID, &item.State,
		&cpuFlag, &gpuFlag, &cpuJSON, &gpuJSON, &item.Priority, &locked, &lastTraining, &firstSeen, &observed); err != nil {
		return domain.DiscoveredContainer{}, err
	}
	item.CPUUnbounded = cpuFlag == 1
	item.GPUUnbounded = gpuFlag == 1
	item.PriorityLocked = locked == 1
	_ = json.Unmarshal([]byte(cpuJSON), &item.CPUIds)
	_ = json.Unmarshal([]byte(gpuJSON), &item.GPUUUIDs)
	if item.CPUIds == nil {
		item.CPUIds = []int{}
	}
	if item.GPUUUIDs == nil {
		item.GPUUUIDs = []string{}
	}
	if lastTraining.Valid && lastTraining.String != "" {
		t, err := time.Parse(time.RFC3339Nano, lastTraining.String)
		if err == nil {
			item.LastTrainingAt = &t
		}
	}
	if firstSeen.Valid && firstSeen.String != "" {
		item.FirstSeenAt, _ = time.Parse(time.RFC3339Nano, firstSeen.String)
	}
	if observed.Valid && observed.String != "" {
		item.ObservedAt, _ = time.Parse(time.RFC3339Nano, observed.String)
	}
	if item.FirstSeenAt.IsZero() {
		item.FirstSeenAt = item.ObservedAt
	}
	if item.Priority == "" {
		item.Priority = domain.PriorityNormal
	}
	return item, nil
}
