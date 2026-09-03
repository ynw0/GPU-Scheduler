package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gpu-scheduler/internal/domain"
)

func (s *Store) ListJobsVisible(ctx context.Context, userID, teamID string, role domain.Role) ([]domain.Job, error) {
	query := `SELECT id,name,state,priority,owner_id,COALESCE(team_id,''),spec_json,bypass_count,COALESCE(last_error,''),created_at,updated_at FROM jobs`
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
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetJob(ctx context.Context, id string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,state,priority,owner_id,COALESCE(team_id,''),spec_json,bypass_count,COALESCE(last_error,''),created_at,updated_at
        FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

func (s *Store) ListActiveAllocations(ctx context.Context) ([]domain.Allocation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,service_name,resource_type,resource_id,amount_bytes,state
        FROM allocations WHERE state IN ('RESERVED','ACTIVE','RELEASING')`)
	if err != nil {
		return nil, err
	}
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
	if err != nil {
		return err
	}
	if err := domain.ValidateTransition(job.State, to); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE jobs SET state=?,updated_at=? WHERE id=? AND state=?",
		to, time.Now().UTC().Format(time.RFC3339Nano), id, job.State)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("job %s changed concurrently", id)
	}
	return nil
}

func (s *Store) DeleteJob(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE discovered_containers SET managed_job_id=NULL WHERE managed_job_id=?", id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM jobs WHERE id=?", id)
	return err
}

func (s *Store) ReleaseAllocations(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE allocations SET state='RELEASED',released_at=?
        WHERE job_id=? AND state IN ('RESERVED','ACTIVE','RELEASING')`,
		time.Now().UTC().Format(time.RFC3339Nano), jobID)
	return err
}

func (s *Store) ListJobsByState(ctx context.Context, states ...domain.JobState) ([]domain.Job, error) {
	if len(states) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(states))
	args := make([]any, len(states))
	for i, state := range states {
		placeholders[i] = "?"
		args[i] = state
	}
	query := `SELECT id,name,state,priority,owner_id,COALESCE(team_id,''),spec_json,bypass_count,COALESCE(last_error,''),created_at,updated_at
        FROM jobs WHERE state IN (` + strings.Join(placeholders, ",") + `) ORDER BY priority ASC, created_at ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) AllocationsForJob(ctx context.Context, jobID string) ([]domain.Allocation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,job_id,service_name,resource_type,resource_id,amount_bytes,state
        FROM allocations WHERE job_id=? ORDER BY resource_type, resource_id`, jobID)
	if err != nil {
		return nil, err
	}
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

func (s *Store) IncrementBypass(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET bypass_count=bypass_count+1,updated_at=? WHERE id=?",
		time.Now().UTC().Format(time.RFC3339Nano), jobID)
	return err
}

func (s *Store) ResetBypass(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET bypass_count=0,updated_at=? WHERE id=?",
		time.Now().UTC().Format(time.RFC3339Nano), jobID)
	return err
}

func (s *Store) SetJobLastError(ctx context.Context, jobID, message string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET last_error=?,updated_at=? WHERE id=?",
		message, time.Now().UTC().Format(time.RFC3339Nano), jobID)
	return err
}

func (s *Store) FindJobByIdempotency(ctx context.Context, ownerID, key string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,state,priority,owner_id,COALESCE(team_id,''),spec_json,bypass_count,COALESCE(last_error,''),created_at,updated_at
        FROM jobs WHERE owner_id=? AND idempotency_key=?`, ownerID, key)
	return scanJob(row)
}

func (s *Store) InsertAudit(ctx context.Context, actorID, action, targetType, targetID, detail string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,detail_json,created_at)
        VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), nullText(actorID), action, targetType, targetID, detail, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) InsertJobEvent(ctx context.Context, jobID, eventType, payload string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_events(id,job_id,event_type,payload_json,created_at) VALUES(?,?,?,?,?)`,
		uuid.NewString(), jobID, eventType, payload, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (domain.Job, error) {
	var job domain.Job
	var specJSON, createdAt, updatedAt string
	err := row.Scan(&job.ID, &job.Name, &job.State, &job.Priority, &job.OwnerID, &job.TeamID,
		&specJSON, &job.BypassCount, &job.LastError, &createdAt, &updatedAt)
	if err != nil {
		return domain.Job{}, err
	}
	if err := json.Unmarshal([]byte(specJSON), &job.Spec); err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return job, nil
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
