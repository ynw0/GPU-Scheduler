package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

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
	if err != nil { return err }
	_, err = s.db.ExecContext(ctx, `UPDATE job_attempts SET state=?,runtime_ref=?,finished_at=? WHERE id=?`,
		state, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), attemptID)
	return err
}

func (s *Store) StartAttempt(ctx context.Context, attemptID string, runtime executor.RuntimeRef) error {
	encoded, err := json.Marshal(runtime)
	if err != nil { return err }
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
