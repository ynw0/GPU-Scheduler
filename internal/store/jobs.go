package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"gpu-scheduler/internal/domain"
)

func (s *Store) ListJobsVisible(ctx context.Context, userID, teamID string, role domain.Role) ([]domain.Job, error) {
	query := `SELECT id,name,state,priority,owner_id,COALESCE(team_id,''),spec_json,bypass_count,created_at,updated_at FROM jobs`
	args := []any{}
	switch role {
	case domain.RoleAdmin, domain.RoleAuditor, domain.RoleViewer:
	case domain.RoleTeamOperator:
		query += " WHERE team_id=?"
		args = append(args, teamID)
	default:
		query += " WHERE owner_id=?"
		args = append(args, userID)
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil { return nil, err }
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetJob(ctx context.Context, id string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,state,priority,owner_id,COALESCE(team_id,''),spec_json,bypass_count,created_at,updated_at
        FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

func (s *Store) ListActiveAllocations(ctx context.Context) ([]domain.Allocation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,service_name,resource_type,resource_id,amount_bytes,state
        FROM allocations WHERE state IN ('RESERVED','ACTIVE','RELEASING')`)
	if err != nil { return nil, err }
	defer rows.Close()
	var result []domain.Allocation
	for rows.Next() {
		var item domain.Allocation
		if err := rows.Scan(&item.ID, &item.JobID, &item.ServiceName, &item.ResourceType, &item.ResourceID, &item.AmountBytes, &item.State); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) TransitionJob(ctx context.Context, id string, to domain.JobState) error {
	job, err := s.GetJob(ctx, id)
	if err != nil { return err }
	if err := domain.ValidateTransition(job.State, to); err != nil { return err }
	result, err := s.db.ExecContext(ctx, "UPDATE jobs SET state=?,updated_at=? WHERE id=? AND state=?",
		to, time.Now().UTC().Format(time.RFC3339Nano), id, job.State)
	if err != nil { return err }
	changed, _ := result.RowsAffected()
	if changed != 1 { return fmt.Errorf("job %s changed concurrently", id) }
	return nil
}

func (s *Store) ReleaseAllocations(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE allocations SET state='RELEASED',released_at=?
        WHERE job_id=? AND state IN ('RESERVED','ACTIVE','RELEASING')`,
		time.Now().UTC().Format(time.RFC3339Nano), jobID)
	return err
}

func (s *Store) RecordHeartbeat(ctx context.Context, jobID, phase string, epoch, step *int, progress *float64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO training_heartbeats(job_id,phase,epoch,step,progress,reported_at)
        VALUES(?,?,?,?,?,?) ON CONFLICT(job_id) DO UPDATE SET
        phase=excluded.phase,epoch=excluded.epoch,step=excluded.step,progress=excluded.progress,reported_at=excluded.reported_at`,
		jobID, phase, epoch, step, progress, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

type scanner interface { Scan(...any) error }

func scanJob(row scanner) (domain.Job, error) {
	var job domain.Job
	var specJSON, createdAt, updatedAt string
	err := row.Scan(&job.ID, &job.Name, &job.State, &job.Priority, &job.OwnerID, &job.TeamID,
		&specJSON, &job.BypassCount, &createdAt, &updatedAt)
	if err != nil { return domain.Job{}, err }
	if err := json.Unmarshal([]byte(specJSON), &job.Spec); err != nil { return domain.Job{}, err }
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return job, nil
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }

