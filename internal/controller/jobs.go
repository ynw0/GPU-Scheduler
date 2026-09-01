package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"gpu-scheduler/internal/auth"
	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/executor"
	"gpu-scheduler/internal/scheduler"
)

func (s *Server) planJob(w http.ResponseWriter, r *http.Request) {
	var spec domain.JobSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeProblem(w, 422, "Invalid job spec", err.Error())
		return
	}
	normalizeJobSpec(&spec)
	if err := validateJobSpec(spec, identityFrom(r.Context())); err != nil {
		writeProblem(w, 422, "Invalid job spec", err.Error())
		return
	}
	topology, availability, err := s.availabilityForSpec(r.Context(), spec)
	if err != nil {
		writeProblem(w, 503, "Availability unavailable", err.Error())
		return
	}
	writeJSON(w, 200, s.reservations.Preview(topology, availability, spec))
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	if !canCreateJobs(identity) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 {
		writeProblem(w, 422, "Idempotency-Key required", "至少 8 个字符")
		return
	}
	var spec domain.JobSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeProblem(w, 422, "Invalid job spec", err.Error())
		return
	}
	normalizeJobSpec(&spec)
	if err := validateJobSpec(spec, identity); err != nil {
		writeProblem(w, 422, "Invalid job spec", err.Error())
		return
	}
	topology, availability, err := s.availabilityForSpec(r.Context(), spec)
	if err != nil {
		writeProblem(w, 503, "Availability unavailable", err.Error())
		return
	}
	result, err := s.reservations.Create(r.Context(), scheduler.CreateInput{
		OwnerID: identity.UserID, TeamID: identity.TeamID, IdempotencyKey: key,
		Spec: spec, Topology: topology, Availability: availability,
	})
	if err != nil {
		writeJSON(w, 409, map[string]any{"type": "about:blank", "title": "Resource conflict", "status": 409, "conflicts": result.Plan.Conflicts, "detail": err.Error()})
		return
	}
	_ = s.store.InsertAudit(r.Context(), identity.UserID, "job.create", "job", result.Job.ID, result.Job.Name)
	if result.Created && result.Job.State == domain.JobReserved {
		go s.execute(context.Background(), result)
	}
	s.events.Publish("job", result.Job)
	writeJSON(w, 202, result.Job)
}

func (s *Server) execute(ctx context.Context, reservation scheduler.ReservationResult) {
	job := reservation.Job
	attemptID, err := s.store.BeginAttempt(ctx, job.ID)
	if err != nil {
		s.failExecution(ctx, job.ID, "", err)
		return
	}
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobCreating); err != nil {
		s.failExecution(ctx, job.ID, attemptID, err)
		return
	}
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobStarting); err != nil {
		s.failExecution(ctx, job.ID, attemptID, err)
		return
	}
	var runtime executor.RuntimeRef
	if job.Spec.Workload.Kind == domain.WorkloadImage {
		plan := reservation.ServicePlans["main"]
		runtime, err = s.agent.CreateImage(ctx, executor.ImageCreateRequest{
			JobID: job.ID, AttemptID: attemptID, Name: "gs-" + job.ID[:12], Workload: job.Spec.Workload.AsImage(),
			Plan: toServicePlan("main", plan, job.Spec.Resources, true), StopTimeout: job.Spec.StopGraceSeconds,
		})
	} else {
		source, sourceErr := s.store.GetComposeSource(ctx, job.Spec.Workload.ComposeSourceID)
		if sourceErr != nil {
			s.failExecution(ctx, job.ID, attemptID, sourceErr)
			return
		}
		plans := make(map[string]executor.ServicePlan)
		for _, service := range job.Spec.Workload.Services {
			plans[service.Name] = toServicePlan(service.Name, reservation.ServicePlans[service.Name], service.Resources, strings.EqualFold(service.Class, "training"))
		}
		runtime, _, err = s.agent.CreateCompose(ctx, executor.ComposeCreateRequest{
			JobID: job.ID, AttemptID: attemptID, ProjectName: "gs-" + job.ID[:12], Source: source, Plans: plans,
		})
	}
	if err != nil {
		s.failExecution(ctx, job.ID, attemptID, err)
		return
	}
	if err := s.store.StartAttempt(ctx, attemptID, runtime); err != nil {
		s.failExecution(ctx, job.ID, attemptID, err)
		return
	}
	if err := s.store.ActivateAllocations(ctx, job.ID); err != nil {
		s.failExecution(ctx, job.ID, attemptID, err)
		return
	}
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobRunning); err != nil {
		s.failExecution(ctx, job.ID, attemptID, err)
		return
	}
	_ = s.store.SetJobLastError(ctx, job.ID, "")
	_, _ = s.notifier.Publish(ctx, job.OwnerID, "info", "任务已启动", job.Name+" 已进入运行状态。")
	s.events.Publish("job", job.ID)
}

func (s *Server) failExecution(ctx context.Context, jobID, attemptID string, err error) {
	s.log.Error("job execution failed", "job_id", jobID, "error", err)
	_ = s.store.SetJobLastError(ctx, jobID, err.Error())
	job, findErr := s.store.GetJob(ctx, jobID)
	if findErr == nil && job.State != domain.JobFailed {
		_ = s.store.TransitionJob(ctx, jobID, domain.JobFailed)
	}
	_ = s.store.ReleaseAllocations(ctx, jobID)
	if attemptID != "" {
		_ = s.store.FinishAttempt(ctx, attemptID, "FAILED", executor.RuntimeRef{})
		_ = s.agent.RemoveAttempt(ctx, attemptID)
	}
	notifyJob, _ := s.store.GetJob(ctx, jobID)
	_, _ = s.notifier.Publish(ctx, notifyJob.OwnerID, "error", "任务启动失败", notifyJob.Name+": "+err.Error())
	s.events.Publish("job", jobID)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	jobs, err := s.store.ListJobsVisible(r.Context(), identity.UserID, identity.TeamID, identity.Role)
	if err != nil {
		writeProblem(w, 500, "List jobs failed", err.Error())
		return
	}
	if jobs == nil {
		jobs = []domain.Job{}
	}
	writeJSON(w, 200, jobs)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeProblem(w, 404, "Job not found", "")
		return
	}
	if !canAccessJob(identityFrom(r.Context()), job) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	detail := domain.JobDetail{Job: job}
	detail.Allocations, _ = s.store.AllocationsForJob(r.Context(), job.ID)
	if heartbeat, err := s.store.GetHeartbeat(r.Context(), job.ID); err == nil {
		detail.Heartbeat = &heartbeat
	}
	if attempt, err := s.store.LatestAttempt(r.Context(), job.ID); err == nil {
		detail.Attempt = &attempt
		if claims, err := s.agent.Claims(r.Context()); err == nil {
			detail.Runtime = runtimeForJob(job.ID, claims)
		}
	}
	writeJSON(w, 200, detail)
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeProblem(w, 404, "Job not found", "")
		return
	}
	if !canManageJob(identityFrom(r.Context()), job) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	var value struct {
		Phase       string
		Epoch, Step *int
		Progress    *float64
	}
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		writeProblem(w, 422, "Invalid heartbeat", err.Error())
		return
	}
	if !slices.Contains([]string{"TRAINING", "CHECKPOINTING", "EVALUATING"}, value.Phase) {
		writeProblem(w, 422, "Invalid heartbeat phase", value.Phase)
		return
	}
	attempt, err := s.store.LatestAttempt(r.Context(), job.ID)
	if err != nil {
		writeProblem(w, 409, "No running attempt", "")
		return
	}
	service := heartbeatServiceName(job)
	if service == "" {
		writeProblem(w, 422, "Job has no training service", "")
		return
	}
	if err := s.store.RecordHeartbeat(r.Context(), domain.TrainingHeartbeatReport{
		AttemptID: attempt.ID, ServiceName: service, Phase: value.Phase,
		Epoch: value.Epoch, Step: value.Step, Progress: value.Progress,
		ReceivedAt: time.Now().UTC(),
	}); err != nil {
		writeProblem(w, 500, "Heartbeat failed", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) availabilityForSpec(ctx context.Context, spec domain.JobSpec) (domain.Topology, domain.Availability, error) {
	topology, availability, err := s.currentAvailability(ctx)
	if err != nil {
		return domain.Topology{}, domain.Availability{}, err
	}
	if spec.ResourcePoolID == "" {
		return topology, availability, nil
	}
	pool, err := s.store.GetResourcePool(ctx, spec.ResourcePoolID)
	if err != nil {
		return domain.Topology{}, domain.Availability{}, err
	}
	return topology, scheduler.IntersectPool(availability, pool), nil
}

func (s *Server) liveTopology(ctx context.Context) (domain.Topology, error) {
	topology, err := s.agent.Topology(ctx)
	if err != nil {
		return domain.Topology{}, err
	}
	for i := range topology.CPUs {
		if slices.Contains(s.cfg.Controller.ReservedCPUIds, topology.CPUs[i].ID) {
			topology.CPUs[i].Allocatable = false
		}
	}
	return topology, nil
}

func (s *Server) currentAvailability(ctx context.Context) (domain.Topology, domain.Availability, error) {
	topology, err := s.liveTopology(ctx)
	if err != nil {
		return domain.Topology{}, domain.Availability{}, err
	}
	allocations, err := s.store.ListActiveAllocations(ctx)
	if err != nil {
		return domain.Topology{}, domain.Availability{}, err
	}
	claims, err := s.agent.Claims(ctx)
	if err != nil {
		return domain.Topology{}, domain.Availability{}, err
	}
	blockedCPU := append([]int{}, s.cfg.Controller.ReservedCPUIds...)
	var blockedGPU []string
	for _, claim := range claims {
		if claim.ManagedJobID != "" {
			continue
		}
		if !slices.Contains([]string{"running", "created", "restarting", "paused"}, strings.ToLower(claim.State)) {
			continue
		}
		if len(claim.TrainingGPUUUIDs) == 0 {
			continue
		}
		if !claim.CPUUnbounded {
			blockedCPU = append(blockedCPU, claim.CPUIds...)
		}
		blockedGPU = append(blockedGPU, claim.TrainingGPUUUIDs...)
	}
	if hostGPUs, err := s.agent.HostGPUProcesses(ctx); err == nil {
		blockedGPU = append(blockedGPU, hostGPUs...)
	}
	return topology, scheduler.BuildAvailability(topology, allocations, blockedCPU, blockedGPU), nil
}

func validateJobSpec(spec domain.JobSpec, identity auth.Identity) error {
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("任务名称不能为空")
	}
	if spec.Priority == domain.PriorityEmergency && identity.Role != domain.RoleAdmin {
		return errors.New("P0 仅管理员可用")
	}
	if spec.Priority == domain.PriorityHigh && identity.Role != domain.RoleAdmin && identity.Role != domain.RoleTeamOperator {
		return errors.New("P1 仅团队管理员可用")
	}
	if spec.Workload.Kind == domain.WorkloadCompose {
		if spec.Workload.ComposeSourceID == "" {
			return errors.New("Compose 来源不能为空")
		}
		if len(spec.Workload.Services) == 0 {
			return errors.New("Compose 至少需要一个已分类服务")
		}
		return nil
	}
	if spec.Workload.Kind != domain.WorkloadImage {
		return errors.New("不支持的任务类型")
	}
	if spec.Workload.Image == "" {
		return errors.New("Image 不能为空")
	}
	for _, item := range spec.Workload.Mounts {
		if strings.TrimSpace(item.Source) == "" || strings.TrimSpace(item.Target) == "" {
			return errors.New("挂载需要宿主机路径和容器路径")
		}
	}
	for _, item := range spec.Workload.PublishedPorts {
		if item.HostPort == 0 || item.ContainerPort == 0 {
			return errors.New("端口映射的宿主机端口和容器端口必须大于 0")
		}
		if item.Protocol != "" && !slices.Contains([]string{"tcp", "udp", "sctp"}, strings.ToLower(item.Protocol)) {
			return errors.New("端口协议只支持 tcp、udp 或 sctp")
		}
	}
	for key := range spec.Workload.Environment {
		if strings.TrimSpace(key) == "" {
			return errors.New("环境变量名不能为空")
		}
	}
	return nil
}

func normalizeJobSpec(spec *domain.JobSpec) {
	if spec.Priority == "" {
		spec.Priority = domain.PriorityNormal
	}
	if spec.StopGraceSeconds == 0 {
		spec.StopGraceSeconds = 30
	}
	if spec.Workload.Kind == "" {
		spec.Workload.Kind = domain.WorkloadImage
	}
}

func toServicePlan(name string, plan domain.JobPlan, request domain.ResourceRequest, training bool) executor.ServicePlan {
	return executor.ServicePlan{Name: name, CPUIds: plan.CPUIds, GPUUUIDs: plan.GPUUUIDs, NUMANodes: plan.NUMANodes, MemoryBytes: request.MemoryBytes, Training: training}
}

func canAccessJob(identity auth.Identity, job domain.Job) bool {
	switch identity.Role {
	case domain.RoleAdmin, domain.RoleAuditor, domain.RoleViewer:
		return true
	case domain.RoleTeamOperator:
		return identity.TeamID != "" && identity.TeamID == job.TeamID
	default:
		return identity.UserID == job.OwnerID
	}
}

func canManageJob(identity auth.Identity, job domain.Job) bool {
	switch identity.Role {
	case domain.RoleAdmin:
		return true
	case domain.RoleTeamOperator:
		return identity.TeamID != "" && identity.TeamID == job.TeamID
	case domain.RoleUser:
		return identity.UserID == job.OwnerID
	default:
		return false
	}
}

func canCreateJobs(identity auth.Identity) bool {
	return identity.Role == domain.RoleUser || identity.Role == domain.RoleTeamOperator || identity.Role == domain.RoleAdmin
}
