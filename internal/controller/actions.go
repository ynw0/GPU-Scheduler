package controller

import (
	"context"
	"net/http"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/store"
)

func (s *Server) actOnJob(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeProblem(w, 404, "Job not found", "")
		return
	}
	if !canManageJob(identity, job) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	action := r.PathValue("action")
	var actErr error
	switch action {
	case "start":
		actErr = s.startJob(r.Context(), job)
	case "stop":
		actErr = s.stopJob(r.Context(), job, domain.JobCanceled)
	case "cancel":
		actErr = s.cancelJob(r.Context(), job)
	case "retry":
		actErr = s.retryJob(r.Context(), job)
	default:
		writeProblem(w, 422, "Unknown action", action)
		return
	}
	if actErr != nil {
		writeProblem(w, 409, "Action conflict", actErr.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identity.UserID, "job."+action, "job", job.ID, action)
	s.events.Publish("job", job.ID)
	updated, _ := s.store.GetJob(r.Context(), job.ID)
	writeJSON(w, 202, updated)
}

func (s *Server) startJob(ctx context.Context, job domain.Job) error {
	if job.State != domain.JobQueued {
		return domain.ValidateTransition(job.State, domain.JobReserved)
	}
	topology, availability, err := s.availabilityForSpec(ctx, job.Spec)
	if err != nil {
		return err
	}
	result, err := s.reservations.Promote(ctx, job, topology, availability)
	if err != nil {
		return err
	}
	go s.execute(context.Background(), result)
	return nil
}

func (s *Server) cancelJob(ctx context.Context, job domain.Job) error {
	switch job.State {
	case domain.JobQueued, domain.JobReserved, domain.JobOrphaned:
		if err := s.store.TransitionJob(ctx, job.ID, domain.JobCanceled); err != nil {
			return err
		}
		_ = s.store.ReleaseAllocations(ctx, job.ID)
		return nil
	case domain.JobCreating, domain.JobStarting:
		if attempt, err := s.store.LatestAttempt(ctx, job.ID); err == nil {
			_ = s.agent.RemoveAttempt(ctx, attempt.ID)
			_ = s.store.FinishAttempt(ctx, attempt.ID, "CANCELED", store.RuntimeFromAttempt(attempt))
		}
		if err := s.store.TransitionJob(ctx, job.ID, domain.JobCanceled); err != nil {
			return err
		}
		_ = s.store.ReleaseAllocations(ctx, job.ID)
		return nil
	case domain.JobRunning, domain.JobDrifted, domain.JobDegraded, domain.JobStopping:
		return s.stopJob(ctx, job, domain.JobCanceled)
	default:
		return domain.ValidateTransition(job.State, domain.JobCanceled)
	}
}

func (s *Server) stopJob(ctx context.Context, job domain.Job, terminal domain.JobState) error {
	if job.State != domain.JobStopping {
		if err := s.store.TransitionJob(ctx, job.ID, domain.JobStopping); err != nil {
			return err
		}
	}
	attempt, err := s.store.LatestAttempt(ctx, job.ID)
	if err == nil && len(attempt.ContainerIDs) > 0 {
		timeout := job.Spec.StopGraceSeconds
		if timeout <= 0 {
			timeout = 30
		}
		if stopErr := s.agent.Stop(ctx, store.RuntimeFromAttempt(attempt), timeout); stopErr != nil {
			s.log.Error("stop workload", "job_id", job.ID, "error", stopErr)
		}
		_ = s.agent.RemoveAttempt(ctx, attempt.ID)
		_ = s.store.FinishAttempt(ctx, attempt.ID, string(terminal), store.RuntimeFromAttempt(attempt))
	}
	if err := s.store.TransitionJob(ctx, job.ID, terminal); err != nil {
		return err
	}
	_ = s.store.ReleaseAllocations(ctx, job.ID)
	_, _ = s.notifier.Publish(ctx, job.OwnerID, "warning", "任务已停止", job.Name)
	return nil
}

func (s *Server) retryJob(ctx context.Context, job domain.Job) error {
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobQueued); err != nil {
		return err
	}
	_ = s.store.ResetBypass(ctx, job.ID)
	go s.dispatchQueue(context.Background())
	return nil
}
