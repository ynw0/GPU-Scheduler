package store

import (
	"context"
	"encoding/json"
	"time"

	"gpu-scheduler/internal/domain"
)

func (s *Store) ListMIGLayouts(ctx context.Context) ([]domain.MIGLayout, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT gpu_uuid, enabled, profiles_json FROM gpu_mig_layouts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.MIGLayout
	for rows.Next() {
		item, err := scanMIGLayout(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if result == nil {
		result = []domain.MIGLayout{}
	}
	return result, rows.Err()
}

func (s *Store) UpsertMIGLayout(ctx context.Context, item domain.MIGLayout) error {
	profiles, err := json.Marshal(item.Profiles)
	if err != nil {
		return err
	}
	enabled := 0
	if item.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO gpu_mig_layouts (gpu_uuid, enabled, profiles_json, updated_at)
        VALUES (?,?,?,?) ON CONFLICT(gpu_uuid) DO UPDATE SET enabled=excluded.enabled, profiles_json=excluded.profiles_json, updated_at=excluded.updated_at`,
		item.GPUUUID, enabled, string(profiles), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func scanMIGLayout(row interface {
	Scan(dest ...any) error
}) (domain.MIGLayout, error) {
	var item domain.MIGLayout
	var enabled int
	var profiles string
	if err := row.Scan(&item.GPUUUID, &enabled, &profiles); err != nil {
		return item, err
	}
	item.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(profiles), &item.Profiles); err != nil {
		return item, err
	}
	if item.Profiles == nil {
		item.Profiles = []string{}
	}
	return item, nil
}
