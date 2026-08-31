package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/executor"
)

func (s *Store) BeginAttempt(ctx context.Context, jobID string) (string, error) {
	var number int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(attempt_number),0)+1 FROM job_attempts WHERE job_id=?", jobID).Scan(&number); err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_attempts(id,job_id,attempt_number,state,created_at)
        VALUES(?,?,?,'CREATING',?)`, id, jobID, number, time.Now().UTC().Format(time.RFC3339Nano))
	return id, err
}

func (s *Store) FinishAttempt(ctx context.Context, attemptID, state string, runtime executor.RuntimeRef) error {
	encoded, err := json.Marshal(runtime)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE job_attempts SET state=?,runtime_ref=?,finished_at=? WHERE id=?`,
		state, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), attemptID)
	return err
}

func (s *Store) StartAttempt(ctx context.Context, attemptID string, runtime executor.RuntimeRef) error {
	encoded, err := json.Marshal(runtime)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE job_attempts SET state='RUNNING',runtime_ref=? WHERE id=?`, string(encoded), attemptID)
	return err
}

func (s *Store) ActivateAllocations(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE allocations SET state='ACTIVE' WHERE job_id=? AND state='RESERVED'", jobID)
	return err
}

func (s *Store) GetComposeSource(ctx context.Context, id string) (executor.ComposeSource, error) {
	var source executor.ComposeSource
	err := s.db.QueryRowContext(ctx, `SELECT id,root_path,entry_file FROM compose_sources WHERE id=? AND enabled=1`, id).
		Scan(&source.ID, &source.RootPath, &source.EntryFile)
	return source, err
}

func (s *Store) LatestAttempt(ctx context.Context, jobID string) (domain.JobAttempt, error) {
	var attempt domain.JobAttempt
	var runtimeJSON, createdAt string
	var finishedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,job_id,attempt_number,state,COALESCE(runtime_ref,''),created_at,finished_at
        FROM job_attempts WHERE job_id=? ORDER BY attempt_number DESC LIMIT 1`, jobID).
		Scan(&attempt.ID, &attempt.JobID, &attempt.AttemptNumber, &attempt.State, &runtimeJSON, &createdAt, &finishedAt)
	if err != nil {
		return domain.JobAttempt{}, err
	}
	if runtimeJSON != "" {
		var runtime executor.RuntimeRef
		if err := json.Unmarshal([]byte(runtimeJSON), &runtime); err != nil {
			return domain.JobAttempt{}, err
		}
		attempt.Kind, attempt.Project, attempt.ContainerIDs = runtime.Kind, runtime.Project, runtime.ContainerIDs
	}
	attempt.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if finishedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, finishedAt.String)
		attempt.FinishedAt = &parsed
	}
	return attempt, nil
}

func RuntimeFromAttempt(attempt domain.JobAttempt) executor.RuntimeRef {
	return executor.RuntimeRef{Kind: attempt.Kind, Project: attempt.Project, ContainerIDs: attempt.ContainerIDs}
}
