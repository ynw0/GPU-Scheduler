package store

import (
	"context"
	"time"

	"gpu-scheduler/internal/domain"
)

func (s *Store) RecordHeartbeat(ctx context.Context, report domain.TrainingHeartbeatReport) error {
	if report.ServiceName == "" {
		report.ServiceName = "main"
	}
	if report.ReceivedAt.IsZero() {
		report.ReceivedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO training_heartbeats(attempt_id,service_name,sequence,phase,epoch,step,progress,reported_at)
        VALUES(?,?,?,?,?,?,?,?)
        ON CONFLICT(attempt_id, service_name) DO UPDATE SET
        sequence=excluded.sequence, phase=excluded.phase, epoch=excluded.epoch, step=excluded.step,
        progress=excluded.progress, reported_at=excluded.reported_at
        WHERE excluded.sequence=0 OR excluded.sequence>=training_heartbeats.sequence`,
		report.AttemptID, report.ServiceName, report.Sequence, report.Phase, report.Epoch, report.Step, report.Progress,
		report.ReceivedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetHeartbeat(ctx context.Context, jobID string) (domain.TrainingStatus, error) {
	var status domain.TrainingStatus
	var reported string
	err := s.db.QueryRowContext(ctx, `SELECT h.attempt_id,h.service_name,h.sequence,h.phase,h.epoch,h.step,h.progress,h.reported_at
        FROM training_heartbeats h
        JOIN job_attempts a ON a.id=h.attempt_id
        WHERE a.job_id=?
        ORDER BY h.reported_at DESC LIMIT 1`, jobID).
		Scan(&status.AttemptID, &status.ServiceName, &status.Sequence, &status.Phase, &status.Epoch, &status.Step, &status.Progress, &reported)
	if err != nil {
		return domain.TrainingStatus{}, err
	}
	status.ReportedAt, _ = time.Parse(time.RFC3339Nano, reported)
	if age := time.Now().UTC().Sub(status.ReportedAt); age > 0 {
		status.AgeSeconds = int64(age.Seconds())
	}
	return status, nil
}

func (s *Store) GetHeartbeatForAttempt(ctx context.Context, attemptID string) (domain.TrainingStatus, error) {
	var status domain.TrainingStatus
	var reported string
	err := s.db.QueryRowContext(ctx, `SELECT attempt_id,service_name,sequence,phase,epoch,step,progress,reported_at
        FROM training_heartbeats WHERE attempt_id=? ORDER BY reported_at DESC LIMIT 1`, attemptID).
		Scan(&status.AttemptID, &status.ServiceName, &status.Sequence, &status.Phase, &status.Epoch, &status.Step, &status.Progress, &reported)
	if err != nil {
		return domain.TrainingStatus{}, err
	}
	status.ReportedAt, _ = time.Parse(time.RFC3339Nano, reported)
	if age := time.Now().UTC().Sub(status.ReportedAt); age > 0 {
		status.AgeSeconds = int64(age.Seconds())
	}
	return status, nil
}

func (s *Store) SetHeartbeatMisses(ctx context.Context, attemptID string, misses int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE job_attempts SET heartbeat_misses=? WHERE id=?`, misses, attemptID)
	return err
}
