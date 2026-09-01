package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/queue"
	"gpu-scheduler/internal/reconcile"
	"gpu-scheduler/internal/scheduler"
	"gpu-scheduler/internal/store"
)

func (s *Server) Start(ctx context.Context) {
	go s.queueLoop(ctx)
	go s.reconcileLoop(ctx)
	go s.metricsLoop(ctx)
}

func (s *Server) queueLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchQueue(ctx)
		}
	}
}

func (s *Server) dispatchQueue(ctx context.Context) {
	jobs, err := s.store.ListJobsByState(ctx, domain.JobQueued)
	if err != nil || len(jobs) == 0 {
		return
	}
	topology, availability, err := s.currentAvailability(ctx)
	if err != nil {
		return
	}
	candidates := make([]queue.Candidate, 0, len(jobs))
	for _, job := range jobs {
		avail := availability
		if job.Spec.ResourcePoolID != "" {
			pool, poolErr := s.store.GetResourcePool(ctx, job.Spec.ResourcePoolID)
			if poolErr == nil {
				avail = scheduler.IntersectPool(avail, pool)
			}
		}
		plan := s.reservations.Preview(topology, avail, job.Spec)
		candidates = append(candidates, queue.Candidate{Job: job, Schedulable: plan.Schedulable})
	}
	next, ok := queue.Next(candidates)
	if !ok {
		s.preemptForQueuedJobs(ctx, jobs)
		return
	}
	for _, candidate := range candidates {
		if candidate.Job.Priority != next.Priority {
			continue
		}
		if candidate.Job.ID == next.ID {
			break
		}
		if !candidate.Schedulable {
			_ = s.store.IncrementBypass(ctx, candidate.Job.ID)
		}
	}
	promoteAvail := availability
	if next.Spec.ResourcePoolID != "" {
		if pool, poolErr := s.store.GetResourcePool(ctx, next.Spec.ResourcePoolID); poolErr == nil {
			promoteAvail = scheduler.IntersectPool(availability, pool)
		}
	}
	result, err := s.reservations.Promote(ctx, next, topology, promoteAvail)
	if err != nil {
		return
	}
	go s.execute(context.Background(), result)
	s.events.Publish("job", result.Job)
}

func (s *Server) reconcileLoop(ctx context.Context) {
	interval := s.cfg.ReconcileInterval()
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileOnce(ctx)
		}
	}
}

func (s *Server) reconcileOnce(ctx context.Context) {
	claims, err := s.agent.Claims(ctx)
	if err != nil {
		return
	}
	allocations, err := s.store.ListActiveAllocations(ctx)
	if err != nil {
		return
	}
	topology, err := s.agent.Topology(ctx)
	if err != nil {
		return
	}
	allCPU := make([]string, 0, len(topology.CPUs))
	for _, cpu := range topology.CPUs {
		allCPU = append(allCPU, itoa(cpu.ID))
	}
	allGPU := make([]string, 0, len(topology.GPUs))
	for _, gpu := range topology.GPUs {
		allGPU = append(allGPU, gpu.UUID)
	}
	drifts, _, _ := reconcile.Compare(claims, allocations, allCPU, allGPU)
	byJob := make(map[string][]domain.ContainerClaim)
	for _, claim := range claims {
		if claim.ManagedJobID != "" {
			byJob[claim.ManagedJobID] = append(byJob[claim.ManagedJobID], claim)
		}
	}
	active, err := s.store.ListJobsByState(ctx, domain.JobRunning, domain.JobStarting, domain.JobCreating, domain.JobStopping, domain.JobDrifted, domain.JobDegraded)
	if err != nil {
		return
	}
	drifted := map[string]struct{}{}
	for _, drift := range drifts {
		if drift.JobID != "" {
			drifted[drift.JobID] = struct{}{}
		}
	}
	s.syncHostContainers(ctx, claims)
	s.preemptExternal(ctx, claims)
	s.reconcileHeartbeats(ctx, claims, active)
	for _, job := range active {
		claimsForJob := byJob[job.ID]
		switch job.State {
		case domain.JobRunning:
			if len(claimsForJob) == 0 {
				s.finishJob(ctx, job, domain.JobOrphaned, "找不到对应容器")
				continue
			}
			if allExited(claimsForJob) {
				to := domain.JobSucceeded
				if anyFailed(claimsForJob) {
					to = domain.JobFailed
				}
				s.finishJob(ctx, job, to, "容器已退出")
				continue
			}
			if _, isDrift := drifted[job.ID]; isDrift {
				_ = s.store.TransitionJob(ctx, job.ID, domain.JobDrifted)
				s.events.Publish("job", job)
			}
		case domain.JobStopping:
			if len(claimsForJob) == 0 || allExited(claimsForJob) {
				s.finishJob(ctx, job, domain.JobCanceled, "已停止")
			}
		case domain.JobDrifted, domain.JobDegraded:
			if len(claimsForJob) == 0 {
				s.finishJob(ctx, job, domain.JobOrphaned, "对账后容器消失")
			} else if allExited(claimsForJob) {
				s.finishJob(ctx, job, domain.JobFailed, "漂移任务容器已退出")
			}
		}
	}
}

func (s *Server) finishJob(ctx context.Context, job domain.Job, to domain.JobState, reason string) {
	if err := s.store.TransitionJob(ctx, job.ID, to); err != nil {
		return
	}
	_ = s.store.ReleaseAllocations(ctx, job.ID)
	if attempt, err := s.store.LatestAttempt(ctx, job.ID); err == nil {
		_ = s.store.FinishAttempt(ctx, attempt.ID, string(to), store.RuntimeFromAttempt(attempt))
		_ = s.agent.CloseHeartbeat(ctx, attempt.ID)
	}
	_, _ = s.notifier.Publish(ctx, job.OwnerID, severityFor(to), "任务 "+string(to), job.Name+": "+reason)
	s.events.Publish("job", job.ID)
}

func (s *Server) metricsLoop(ctx context.Context) {
	interval := s.cfg.SampleInterval()
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var ticks int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot, err := s.agent.HostMetrics(ctx)
			if err != nil {
				continue
			}
			s.events.Publish("metrics", snapshot)
			ticks++
			if ticks%5 == 0 {
				_ = s.store.InsertMetricSnapshot(ctx, "host", "local", snapshot)
				s.recordJobMetrics(ctx, snapshot)
			}
			if ticks%30 == 0 {
				s.rollupMetrics(ctx)
				_ = s.store.PruneMetrics(ctx, s.cfg.Retention.RawMetricDays, s.cfg.Retention.RollupMetricDays)
			}
		}
	}
}

func (s *Server) recordJobMetrics(ctx context.Context, snapshot domain.HostMetricSnapshot) {
	jobs, err := s.store.ListJobsByState(ctx, domain.JobRunning)
	if err != nil {
		return
	}
	for _, job := range jobs {
		attempt, err := s.store.LatestAttempt(ctx, job.ID)
		if err != nil || len(attempt.ContainerIDs) == 0 {
			continue
		}
		allocs, err := s.store.AllocationsForJob(ctx, job.ID)
		if err != nil {
			continue
		}
		metric := domain.JobMetricSnapshot{SampledAt: snapshot.SampledAt, CPUPercentByID: map[int]float64{}}
		complete := true
		for _, containerID := range attempt.ContainerIDs {
			containerMetric, err := s.agent.ContainerMetrics(ctx, containerID)
			if err != nil {
				complete = false
				break
			}
			metric.Containers = append(metric.Containers, containerMetric)
			metric.MemoryUsedBytes += containerMetric.MemoryUsedBytes
			metric.MemoryLimitBytes += containerMetric.MemoryLimitBytes
			metric.MemoryFailCount += containerMetric.MemoryFailCount
			metric.OOMUnder = metric.OOMUnder || containerMetric.OOMUnder
			metric.BlockIOReadBytes += containerMetric.BlockIOReadBytes
			metric.BlockIOWriteBytes += containerMetric.BlockIOWriteBytes
			for id, percent := range containerMetric.CPUPercentByID {
				metric.CPUPercentByID[id] += percent
			}
		}
		if !complete {
			continue
		}
		metric.GPUs = filterJobGPUs(snapshot.GPUs, allocs)
		_ = s.store.InsertMetricSnapshot(ctx, "job", job.ID, metric)
	}
}

func filterJobGPUs(gpus []domain.GPUMetric, allocs []domain.Allocation) []domain.GPUMetric {
	gpuOK := map[string]struct{}{}
	for _, alloc := range allocs {
		if alloc.ResourceType == "gpu" {
			gpuOK[alloc.ResourceID] = struct{}{}
		}
	}
	filtered := make([]domain.GPUMetric, 0)
	for _, gpu := range gpus {
		if _, ok := gpuOK[gpu.UUID]; ok {
			filtered = append(filtered, gpu)
		}
	}
	return filtered
}

func (s *Server) rollupMetrics(ctx context.Context) {
	since := time.Now().UTC().Add(-time.Minute)
	snapshots, err := s.store.RecentHostSnapshots(ctx, since)
	if err != nil || len(snapshots) == 0 {
		return
	}
	bucket := since.Truncate(time.Minute).Format(time.RFC3339)
	_ = s.store.InsertMetricRollup(ctx, "host", "local", bucket, snapshots[len(snapshots)-1])
}

func allExited(claims []domain.ContainerClaim) bool {
	if len(claims) == 0 {
		return false
	}
	for _, claim := range claims {
		state := strings.ToLower(claim.State)
		if state == "running" || state == "created" || state == "restarting" || state == "paused" {
			return false
		}
	}
	return true
}

func anyFailed(claims []domain.ContainerClaim) bool {
	for _, claim := range claims {
		if claim.ExitCode != 0 {
			return true
		}
	}
	return false
}

func severityFor(state domain.JobState) string {
	switch state {
	case domain.JobSucceeded:
		return "info"
	case domain.JobCanceled:
		return "warning"
	default:
		return "error"
	}
}

func itoa(value int) string { return strconv.Itoa(value) }

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, 500, "Streaming unavailable", "")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := s.events.Subscribe()
	defer s.events.Unsubscribe(ch)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case now := <-ticker.C:
			_, _ = w.Write([]byte("event: keepalive\ndata: {\"at\":\"" + now.UTC().Format(time.RFC3339) + "\"}\n\n"))
			flusher.Flush()
		case event := <-ch:
			payload, _ := json.Marshal(event)
			_, _ = w.Write([]byte("event: " + event.Type + "\ndata: " + string(payload) + "\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeProblem(w, 404, "Job not found", "")
		return
	}
	if !canAccessJob(identityFrom(r.Context()), job) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	attempt, err := s.store.LatestAttempt(r.Context(), job.ID)
	if err != nil || len(attempt.ContainerIDs) == 0 {
		writeProblem(w, 409, "No runtime logs", "")
		return
	}
	var containerID string
	for _, id := range attempt.ContainerIDs {
		containerID = id
		break
	}
	reader, err := s.agent.Logs(r.Context(), containerID, true)
	if err != nil {
		writeProblem(w, 503, "Logs unavailable", err.Error())
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			_, _ = w.Write([]byte("data: " + strings.ReplaceAll(string(buf[:n]), "\n", "\ndata: ") + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && r.Context().Err() == nil {
				s.log.Error("log stream", "error", readErr)
			}
			return
		}
	}
}

func (s *Server) jobMetrics(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeProblem(w, 404, "Job not found", "")
		return
	}
	if !canAccessJob(identityFrom(r.Context()), job) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	items, err := s.store.ListJobMetrics(r.Context(), job.ID, 120)
	if err != nil {
		writeProblem(w, 500, "Metrics unavailable", err.Error())
		return
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	writeJSON(w, 200, items)
}
