package controller

import (
	"context"
	"strings"
	"time"

	"gpu-scheduler/internal/domain"
)

const (
	heartbeatExpire     = 90 * time.Second
	heartbeatStartGrace = 10 * time.Minute
	heartbeatMissLimit  = 3
)

func (s *Server) reconcileHeartbeats(ctx context.Context, claims []domain.ContainerClaim, jobs []domain.Job) {
	if reports, err := s.agent.ListHeartbeats(ctx); err == nil {
		for _, report := range reports {
			_ = s.store.RecordHeartbeat(ctx, report)
		}
	}
	byJob := make(map[string][]domain.ContainerClaim)
	for _, claim := range claims {
		if claim.ManagedJobID != "" {
			byJob[claim.ManagedJobID] = append(byJob[claim.ManagedJobID], claim)
		}
	}
	for _, job := range jobs {
		if job.State != domain.JobRunning && job.State != domain.JobDegraded {
			continue
		}
		attempt, err := s.store.LatestAttempt(ctx, job.ID)
		if err != nil {
			continue
		}
		jobClaims := byJob[job.ID]
		runtime := runtimeForJob(job.ID, jobClaims)
		if dockerUnhealthy(jobClaims) && job.State == domain.JobRunning {
			if err := s.store.TransitionJob(ctx, job.ID, domain.JobDegraded); err == nil {
				_, _ = s.notifier.Publish(ctx, job.OwnerID, "warning", "任务降级", job.Name+"：Docker Health 为 unhealthy。")
				s.events.Publish("job", job.ID)
				job.State = domain.JobDegraded
			}
		}
		mounted := jobHasHeartbeatMount(jobClaims)
		if mounted {
			_ = s.agent.EnsureHeartbeat(ctx, attempt.ID)
		}
		status, hbErr := s.store.GetHeartbeatForAttempt(ctx, attempt.ID)
		if !jobNeedsTrainingHeartbeat(job) || (!mounted && hbErr != nil) {
			s.recoverIfHealthy(ctx, job, runtime, true)
			continue
		}
		now := time.Now().UTC()
		inGrace := now.Sub(attempt.CreatedAt) < heartbeatStartGrace
		fresh := hbErr == nil && now.Sub(status.ReportedAt) <= heartbeatExpire
		if inGrace || fresh {
			if attempt.HeartbeatMisses != 0 {
				_ = s.store.SetHeartbeatMisses(ctx, attempt.ID, 0)
			}
			s.recoverIfHealthy(ctx, job, runtime, inGrace || fresh)
			continue
		}
		misses := attempt.HeartbeatMisses + 1
		_ = s.store.SetHeartbeatMisses(ctx, attempt.ID, misses)
		if misses < heartbeatMissLimit {
			continue
		}
		active := runtime != nil && (runtime.CPUActivity || runtime.GPUActivity)
		if active {
			if misses == heartbeatMissLimit {
				_, _ = s.notifier.Publish(ctx, job.OwnerID, "warning", "训练心跳超时", job.Name+"：训练心跳已过期，但 CPU/GPU 仍有活动。")
			}
			continue
		}
		if job.State == domain.JobRunning {
			if err := s.store.TransitionJob(ctx, job.ID, domain.JobDegraded); err == nil {
				_, _ = s.notifier.Publish(ctx, job.OwnerID, "warning", "任务降级", job.Name+"：训练心跳超时且 CPU/GPU 无活动。")
				s.events.Publish("job", job.ID)
			}
		}
	}
}

func (s *Server) recoverIfHealthy(ctx context.Context, job domain.Job, runtime *domain.RuntimeHeartbeat, heartbeatOK bool) {
	if job.State != domain.JobDegraded || !heartbeatOK {
		return
	}
	if runtime != nil && strings.EqualFold(runtime.DockerHealth, "unhealthy") {
		return
	}
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobRunning); err == nil {
		_, _ = s.notifier.Publish(ctx, job.OwnerID, "info", "任务已恢复", job.Name+"：心跳与健康检查已恢复。")
		s.events.Publish("job", job.ID)
	}
}

func runtimeForJob(jobID string, claims []domain.ContainerClaim) *domain.RuntimeHeartbeat {
	var runtime *domain.RuntimeHeartbeat
	for _, claim := range claims {
		if claim.ManagedJobID != jobID {
			continue
		}
		if runtime == nil {
			runtime = &domain.RuntimeHeartbeat{
				ObservedAt: claim.RuntimeHeartbeatAt, DockerHealth: claim.DockerHealth,
			}
		}
		if claim.RuntimeHeartbeatAt.After(runtime.ObservedAt) {
			runtime.ObservedAt = claim.RuntimeHeartbeatAt
		}
		if strings.EqualFold(claim.DockerHealth, "unhealthy") {
			runtime.DockerHealth = claim.DockerHealth
		}
		runtime.CPUActivity = runtime.CPUActivity || claim.CPUActivity
		runtime.GPUActivity = runtime.GPUActivity || claim.GPUActivity
		runtime.OOMUnder = runtime.OOMUnder || claim.OOMUnder
	}
	return runtime
}

func dockerUnhealthy(claims []domain.ContainerClaim) bool {
	for _, claim := range claims {
		if strings.EqualFold(claim.DockerHealth, "unhealthy") {
			return true
		}
	}
	return false
}

func jobNeedsTrainingHeartbeat(job domain.Job) bool {
	if job.Spec.Workload.Kind != domain.WorkloadCompose {
		return true
	}
	for _, service := range job.Spec.Workload.Services {
		if strings.EqualFold(service.Class, "training") {
			return true
		}
	}
	return false
}

func jobHasHeartbeatMount(claims []domain.ContainerClaim) bool {
	for _, claim := range claims {
		if claim.HeartbeatMounted {
			return true
		}
	}
	return false
}

func heartbeatServiceName(job domain.Job) string {
	if job.Spec.Workload.Kind == domain.WorkloadCompose {
		for _, service := range job.Spec.Workload.Services {
			if strings.EqualFold(service.Class, "training") {
				return service.Name
			}
		}
		return ""
	}
	return "main"
}
