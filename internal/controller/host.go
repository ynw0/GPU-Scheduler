package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gpu-scheduler/internal/domain"
)

const hostSilentLimit = 2 * time.Hour

func (s *Server) getUnmanaged(w http.ResponseWriter, r *http.Request) {
	claims, err := s.agent.Claims(r.Context())
	if err != nil {
		writeProblem(w, 503, "Containers unavailable", err.Error())
		return
	}
	s.syncHostContainers(r.Context(), claims)
	records, _ := s.store.ListDiscoveredContainers(r.Context())
	byID := make(map[string]domain.DiscoveredContainer, len(records))
	for _, item := range records {
		byID[item.ContainerID] = item
	}
	hostMetrics, _ := s.agent.HostMetrics(r.Context())
	gpuByUUID := make(map[string]domain.GPUMetric, len(hostMetrics.GPUs))
	for _, gpu := range hostMetrics.GPUs {
		gpuByUUID[gpu.UUID] = gpu
	}
	now := time.Now().UTC()
	jobs := map[string]domain.Job{}
	result := make([]domain.HostContainer, 0, len(claims))
	for _, claim := range claims {
		record := byID[claim.ContainerID]
		if claim.ManagedJobID != "" {
			if _, ok := jobs[claim.ManagedJobID]; !ok {
				if found, err := s.store.GetJob(r.Context(), claim.ManagedJobID); err == nil {
					jobs[claim.ManagedJobID] = found
				}
			}
		}
		item := hostContainerFrom(claim, record, jobs[claim.ManagedJobID], now)
		item.MemoryUsedBytes = claim.MemoryUsedBytes
		item.MemoryLimitBytes = claim.MemoryLimitBytes
		item.CPUPercent = claim.CPUPercent
		for _, uuid := range item.TrainingGPUUUIDs {
			if gpu, ok := gpuByUUID[uuid]; ok {
				item.GPUs = append(item.GPUs, gpu)
			}
		}
		result = append(result, item)
	}
	writeJSON(w, 200, result)
}

func (s *Server) patchHostContainer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Priority domain.Priority `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	if body.Priority != domain.PriorityEmergency && body.Priority != domain.PriorityHigh && body.Priority != domain.PriorityNormal && body.Priority != domain.PriorityLow {
		writeProblem(w, 422, "Invalid priority", string(body.Priority))
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.GetDiscoveredContainer(r.Context(), id); err != nil {
		writeProblem(w, 404, "Container not found", "")
		return
	}
	if err := s.store.UpdateDiscoveredPriority(r.Context(), id, body.Priority, false); err != nil {
		writeProblem(w, 500, "Update failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "host-container.priority", "container", id, string(body.Priority))
	w.WriteHeader(204)
}

func (s *Server) stopHostContainer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.unmanagedContainerID(w, r)
	if !ok {
		return
	}
	if err := s.agent.StopContainer(r.Context(), id, 10); err != nil {
		writeProblem(w, 503, "Stop failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "host-container.stop", "container", id, "")
	w.WriteHeader(204)
}

func (s *Server) pauseHostContainer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.unmanagedContainerID(w, r)
	if !ok {
		return
	}
	if err := s.agent.PauseContainer(r.Context(), id); err != nil {
		writeProblem(w, 503, "Pause failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "host-container.pause", "container", id, "")
	w.WriteHeader(204)
}

func (s *Server) unpauseHostContainer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.unmanagedContainerID(w, r)
	if !ok {
		return
	}
	if err := s.agent.UnpauseContainer(r.Context(), id); err != nil {
		writeProblem(w, 503, "Unpause failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "host-container.unpause", "container", id, "")
	w.WriteHeader(204)
}

func (s *Server) unmanagedContainerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	claims, err := s.agent.Claims(r.Context())
	if err != nil {
		writeProblem(w, 503, "Containers unavailable", err.Error())
		return "", false
	}
	for _, claim := range claims {
		if claim.ContainerID == id && claim.ManagedJobID != "" {
			writeProblem(w, 409, "Managed container", "内部容器请从任务详情停止")
			return "", false
		}
	}
	return id, true
}

func (s *Server) syncHostContainers(ctx context.Context, claims []domain.ContainerClaim) {
	records, err := s.store.ListDiscoveredContainers(ctx)
	if err != nil {
		return
	}
	existing := make(map[string]domain.DiscoveredContainer, len(records))
	for _, item := range records {
		existing[item.ContainerID] = item
	}
	now := time.Now().UTC()
	for _, claim := range claims {
		if claim.ManagedJobID != "" {
			continue
		}
		item := domain.DiscoveredContainer{
			ContainerID: claim.ContainerID, Name: claim.Name, Image: claim.Image,
			State: claim.State, CPUIds: claim.CPUIds, GPUUUIDs: claim.GPUUUIDs,
			CPUUnbounded: claim.CPUUnbounded, GPUUnbounded: claim.GPUUnbounded,
			Priority: domain.PriorityNormal, ObservedAt: now, FirstSeenAt: now,
		}
		if prev, ok := existing[claim.ContainerID]; ok {
			item.Priority = prev.Priority
			item.PriorityLocked = prev.PriorityLocked
			item.LastTrainingAt = prev.LastTrainingAt
			item.FirstSeenAt = prev.FirstSeenAt
			if item.FirstSeenAt.IsZero() {
				item.FirstSeenAt = now
			}
		}
		if len(claim.TrainingGPUUUIDs) > 0 && claimLive(claim.State) {
			stamp := now
			item.LastTrainingAt = &stamp
		} else if !item.PriorityLocked {
			silentFrom := item.FirstSeenAt
			if item.LastTrainingAt != nil {
				silentFrom = *item.LastTrainingAt
			}
			if now.Sub(silentFrom) >= hostSilentLimit {
				item.Priority = domain.PriorityLow
				item.PriorityLocked = true
			}
		}
		_ = s.store.UpsertDiscoveredContainer(ctx, item)
	}
}

func (s *Server) preemptExternal(ctx context.Context, claims []domain.ContainerClaim) {
	records, err := s.store.ListDiscoveredContainers(ctx)
	if err != nil {
		return
	}
	priority := make(map[string]domain.Priority, len(records))
	for _, item := range records {
		priority[item.ContainerID] = item.Priority
	}
	allocations, err := s.store.ListActiveAllocations(ctx)
	if err != nil {
		return
	}
	jobPriority := map[string]domain.Priority{}
	gpuOwner := map[string]string{}
	for _, allocation := range allocations {
		if allocation.ResourceType != "gpu" {
			continue
		}
		gpuOwner[allocation.ResourceID] = allocation.JobID
		if _, ok := jobPriority[allocation.JobID]; ok {
			continue
		}
		job, err := s.store.GetJob(ctx, allocation.JobID)
		if err != nil {
			continue
		}
		jobPriority[allocation.JobID] = job.Priority
	}
	for _, claim := range claims {
		if claim.ManagedJobID != "" || !claimLive(claim.State) || len(claim.TrainingGPUUUIDs) == 0 {
			continue
		}
		hostPriority := priority[claim.ContainerID]
		if hostPriority == "" {
			hostPriority = domain.PriorityNormal
		}
		for _, uuid := range claim.TrainingGPUUUIDs {
			jobID, ok := gpuOwner[uuid]
			if !ok {
				continue
			}
			if jobPriority[jobID] < hostPriority {
				s.stopExternal(ctx, claim.ContainerID, "preempt-running-job")
				break
			}
		}
	}
}

func (s *Server) preemptForQueuedJobs(ctx context.Context, jobs []domain.Job) {
	if len(jobs) == 0 {
		return
	}
	top := jobs[0]
	for _, job := range jobs[1:] {
		if job.Priority < top.Priority || (job.Priority == top.Priority && job.CreatedAt.Before(top.CreatedAt)) {
			top = job
		}
	}
	claims, err := s.agent.Claims(ctx)
	if err != nil {
		return
	}
	s.syncHostContainers(ctx, claims)
	records, err := s.store.ListDiscoveredContainers(ctx)
	if err != nil {
		return
	}
	priority := make(map[string]domain.Priority, len(records))
	for _, item := range records {
		priority[item.ContainerID] = item.Priority
	}
	for _, claim := range claims {
		if claim.ManagedJobID != "" || !claimLive(claim.State) || len(claim.TrainingGPUUUIDs) == 0 {
			continue
		}
		hostPriority := priority[claim.ContainerID]
		if hostPriority == "" {
			hostPriority = domain.PriorityNormal
		}
		if top.Priority < hostPriority {
			s.stopExternal(ctx, claim.ContainerID, "preempt-queued-job")
		}
	}
}

func (s *Server) stopExternal(ctx context.Context, containerID, reason string) {
	if err := s.agent.StopContainer(ctx, containerID, 10); err != nil {
		return
	}
	_ = s.store.InsertAudit(ctx, "", "host-container.preempt", "container", containerID, reason)
}

func hostContainerFrom(claim domain.ContainerClaim, record domain.DiscoveredContainer, job domain.Job, now time.Time) domain.HostContainer {
	priority := domain.PriorityNormal
	firstSeen := now
	if record.ContainerID != "" {
		priority = record.Priority
		firstSeen = record.FirstSeenAt
	}
	silentFrom := firstSeen
	if record.LastTrainingAt != nil {
		silentFrom = *record.LastTrainingAt
	}
	silent := int64(0)
	if now.After(silentFrom) {
		silent = int64(now.Sub(silentFrom).Seconds())
	}
	item := domain.HostContainer{
		ContainerID: claim.ContainerID, Name: claim.Name, Image: claim.Image, State: claim.State,
		CPUIds: orEmptyInts(claim.CPUIds), GPUUUIDs: orEmptyStrings(claim.GPUUUIDs),
		CPUUnbounded: claim.CPUUnbounded, GPUUnbounded: claim.GPUUnbounded,
		Training:         len(claim.TrainingGPUUUIDs) > 0 && claimLive(claim.State),
		TrainingGPUUUIDs: orEmptyStrings(claim.TrainingGPUUUIDs),
		DockerHealth:     claim.DockerHealth, CPUActivity: claim.CPUActivity, GPUActivity: claim.GPUActivity, OOMUnder: claim.OOMUnder,
		Priority: priority, PriorityLocked: record.PriorityLocked,
		LastTrainingAt: record.LastTrainingAt, FirstSeenAt: firstSeen, SilentSeconds: silent,
		ManagedJobID: claim.ManagedJobID, JobName: job.Name,
	}
	if job.ID != "" {
		item.Priority = job.Priority
		item.PriorityLocked = false
		item.SilentSeconds = 0
	}
	return item
}

func claimLive(state string) bool {
	switch strings.ToLower(state) {
	case "running", "created", "restarting", "paused":
		return true
	default:
		return false
	}
}

func orEmptyInts(values []int) []int {
	if values == nil {
		return []int{}
	}
	return values
}

func orEmptyStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func (s *Server) rebootHost(w http.ResponseWriter, r *http.Request) {
	if err := s.agent.Reboot(r.Context()); err != nil {
		writeProblem(w, 503, "Reboot failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "host.reboot", "host", "", "")
	w.WriteHeader(204)
}
