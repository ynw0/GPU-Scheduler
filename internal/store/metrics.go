package store

import (
	"context"
	"encoding/json"
	"time"

	"gpu-scheduler/internal/domain"
)

func (s *Store) InsertMetricSnapshot(ctx context.Context, scopeType, scopeID string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil { return err }
	_, err = s.db.ExecContext(ctx, `INSERT INTO metric_snapshots(scope_type,scope_id,sampled_at,payload_json) VALUES(?,?,?,?)`,
		scopeType, scopeID, time.Now().UTC().Format(time.RFC3339Nano), string(encoded))
	return err
}

func (s *Store) InsertMetricRollup(ctx context.Context, scopeType, scopeID, bucket string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil { return err }
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO metric_rollups(scope_type,scope_id,bucket_start,payload_json) VALUES(?,?,?,?)`,
		scopeType, scopeID, bucket, string(encoded))
	return err
}

func (s *Store) ListJobMetrics(ctx context.Context, jobID string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 { limit = 60 }
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM metric_snapshots WHERE scope_type='job' AND scope_id=? ORDER BY sampled_at DESC LIMIT ?`, jobID, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	var result []json.RawMessage
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil { return nil, err }
		result = append(result, json.RawMessage(payload))
	}
	return result, rows.Err()
}

func (s *Store) PruneMetrics(ctx context.Context, rawDays, rollupDays int) error {
	rawCutoff := time.Now().UTC().Add(-time.Duration(rawDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	rollupCutoff := time.Now().UTC().Add(-time.Duration(rollupDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, "DELETE FROM metric_snapshots WHERE sampled_at<?", rawCutoff); err != nil { return err }
	_, err := s.db.ExecContext(ctx, "DELETE FROM metric_rollups WHERE bucket_start<?", rollupCutoff)
	return err
}

func (s *Store) RecentHostSnapshots(ctx context.Context, since time.Time) ([]domain.HostMetricSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM metric_snapshots WHERE scope_type='host' AND scope_id='local' AND sampled_at>=? ORDER BY sampled_at`,
		since.UTC().Format(time.RFC3339Nano))
	if err != nil { return nil, err }
	defer rows.Close()
	var result []domain.HostMetricSnapshot
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil { return nil, err }
		var snapshot domain.HostMetricSnapshot
		if err := json.Unmarshal([]byte(payload), &snapshot); err != nil { return nil, err }
		result = append(result, snapshot)
	}
	return result, rows.Err()
}
