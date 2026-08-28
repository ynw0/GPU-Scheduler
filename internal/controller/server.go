package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"gpu-scheduler/internal/agentapi"
	"gpu-scheduler/internal/auth"
	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/executor"
	"gpu-scheduler/internal/notification"
	"gpu-scheduler/internal/scheduler"
	"gpu-scheduler/internal/store"
	"gpu-scheduler/internal/webui"
)

const sessionCookie = "gpu_scheduler_session"

type identityKey struct{}

type Server struct {
	log          *slog.Logger
	store        *store.Store
	agent        *agentapi.Client
	auth         *auth.Service
	scheduler    *scheduler.Engine
	reservations *scheduler.ReservationService
	notifier     *notification.Worker
	mux          *http.ServeMux
}

func New(log *slog.Logger, database *store.Store, agent *agentapi.Client, authService *auth.Service, notifier *notification.Worker) *Server {
	engine := scheduler.New()
	s := &Server{
		log: log, store: database, agent: agent, auth: authService,
		scheduler: engine, reservations: scheduler.NewReservationService(database, engine), notifier: notifier, mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return requestLog(s.log, securityHeaders(s.mux)) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.getHealth)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.Handle("POST /api/v1/auth/logout", s.protected(http.HandlerFunc(s.logout), true))
	s.mux.Handle("GET /api/v1/me", s.protected(http.HandlerFunc(s.me), false))
	s.mux.Handle("GET /api/v1/topology", s.protected(http.HandlerFunc(s.getTopology), false))
	s.mux.Handle("GET /api/v1/resources/availability", s.protected(http.HandlerFunc(s.getAvailability), false))
	s.mux.Handle("GET /api/v1/images", s.protected(http.HandlerFunc(s.getImages), false))
	s.mux.Handle("GET /api/v1/unmanaged-containers", s.protected(http.HandlerFunc(s.getUnmanaged), false))
	s.mux.Handle("POST /api/v1/job-plans", s.protected(http.HandlerFunc(s.planJob), true))
	s.mux.Handle("GET /api/v1/jobs", s.protected(http.HandlerFunc(s.listJobs), false))
	s.mux.Handle("POST /api/v1/jobs", s.protected(http.HandlerFunc(s.createJob), true))
	s.mux.Handle("GET /api/v1/jobs/{jobId}", s.protected(http.HandlerFunc(s.getJob), false))
	s.mux.Handle("POST /api/v1/jobs/{jobId}/heartbeat", s.protected(http.HandlerFunc(s.heartbeat), true))
	s.mux.Handle("GET /api/v1/events/stream", s.protected(http.HandlerFunc(s.streamEvents), false))
	assets, err := fs.Sub(webui.Assets, "dist")
	if err != nil { panic(err) }
	s.mux.Handle("/", spa(http.FileServer(http.FS(assets)), assets))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request struct { Username, Password string }
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil { writeProblem(w, 400, "Invalid request", err.Error()); return }
	result, err := s.auth.Login(r.Context(), request.Username, request.Password)
	if err != nil { writeProblem(w, 401, "Invalid credentials", "用户名或密码错误"); return }
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: result.SessionToken, Path: "/", Secure: true, HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Expires: result.ExpiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "gpu_scheduler_csrf", Value: result.CSRFToken, Path: "/", Secure: true,
		SameSite: http.SameSiteStrictMode, Expires: result.ExpiresAt,
	})
	writeJSON(w, 200, result.Identity)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookie)
	_ = s.auth.Logout(r.Context(), cookie.Value)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: "gpu_scheduler_csrf", Path: "/", MaxAge: -1, Secure: true})
	w.WriteHeader(204)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, identityFrom(r.Context())) }

func (s *Server) getHealth(w http.ResponseWriter, r *http.Request) {
	agentStatus, status := "online", "ok"
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second); defer cancel()
	if err := s.agent.Health(ctx); err != nil { agentStatus, status = "offline", "degraded" }
	writeJSON(w, 200, map[string]string{"status": status, "version": "dev", "agent": agentStatus})
}

func (s *Server) getTopology(w http.ResponseWriter, r *http.Request) {
	topology, err := s.agent.Topology(r.Context())
	if err != nil { writeProblem(w, 503, "Agent unavailable", err.Error()); return }
	writeJSON(w, 200, topology)
}

func (s *Server) getAvailability(w http.ResponseWriter, r *http.Request) {
	_, availability, err := s.currentAvailability(r.Context())
	if err != nil { writeProblem(w, 503, "Availability unavailable", err.Error()); return }
	writeJSON(w, 200, availability)
}

func (s *Server) getImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.agent.Images(r.Context())
	if err != nil { writeProblem(w, 503, "Images unavailable", err.Error()); return }
	writeJSON(w, 200, images)
}

func (s *Server) getUnmanaged(w http.ResponseWriter, r *http.Request) {
	claims, err := s.agent.Claims(r.Context())
	if err != nil { writeProblem(w, 503, "Containers unavailable", err.Error()); return }
	result := claims[:0]
	for _, claim := range claims { if claim.ManagedJobID == "" { result = append(result, claim) } }
	writeJSON(w, 200, result)
}

func (s *Server) planJob(w http.ResponseWriter, r *http.Request) {
	var spec domain.JobSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil { writeProblem(w, 422, "Invalid job spec", err.Error()); return }
	normalizeJobSpec(&spec)
	if err := validateJobSpec(spec, identityFrom(r.Context())); err != nil { writeProblem(w, 422, "Invalid job spec", err.Error()); return }
	topology, availability, err := s.currentAvailability(r.Context())
	if err != nil { writeProblem(w, 503, "Availability unavailable", err.Error()); return }
	plan := s.reservations.Preview(topology, availability, spec)
	writeJSON(w, 200, plan)
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 { writeProblem(w, 422, "Idempotency-Key required", "至少 8 个字符"); return }
	var spec domain.JobSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil { writeProblem(w, 422, "Invalid job spec", err.Error()); return }
	normalizeJobSpec(&spec)
	if err := validateJobSpec(spec, identity); err != nil { writeProblem(w, 422, "Invalid job spec", err.Error()); return }
	topology, availability, err := s.currentAvailability(r.Context())
	if err != nil { writeProblem(w, 503, "Availability unavailable", err.Error()); return }
	result, err := s.reservations.Create(r.Context(), scheduler.CreateInput{
		OwnerID: identity.UserID, TeamID: identity.TeamID, IdempotencyKey: key,
		Spec: spec, Topology: topology, Availability: availability,
	})
	if err != nil {
		writeJSON(w, 409, map[string]any{"type": "about:blank", "title": "Resource conflict", "status": 409, "conflicts": result.Plan.Conflicts})
		return
	}
	go s.execute(context.Background(), result)
	writeJSON(w, 202, result.Job)
}

func (s *Server) execute(ctx context.Context, reservation scheduler.ReservationResult) {
	job := reservation.Job
	attemptID, err := s.store.BeginAttempt(ctx, job.ID)
	if err != nil { s.failExecution(ctx, job.ID, "", err); return }
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobCreating); err != nil { s.failExecution(ctx, job.ID, attemptID, err); return }
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobStarting); err != nil { s.failExecution(ctx, job.ID, attemptID, err); return }
	var runtime executor.RuntimeRef
	if job.Spec.Workload.Kind == domain.WorkloadImage {
		plan := reservation.ServicePlans["main"]
		runtime, err = s.agent.CreateImage(ctx, executor.ImageCreateRequest{
			JobID: job.ID, AttemptID: attemptID, Name: "gs-"+job.ID[:12], Workload: job.Spec.Workload.AsImage(),
			Plan: toServicePlan("main", plan, job.Spec.Resources), StopTimeout: job.Spec.StopGraceSeconds,
		})
	} else {
		source, sourceErr := s.store.GetComposeSource(ctx, job.Spec.Workload.ComposeSourceID)
		if sourceErr != nil { s.failExecution(ctx, job.ID, attemptID, sourceErr); return }
		plans := make(map[string]executor.ServicePlan)
		for _, service := range job.Spec.Workload.Services {
			plans[service.Name] = toServicePlan(service.Name, reservation.ServicePlans[service.Name], service.Resources)
		}
		runtime, _, err = s.agent.CreateCompose(ctx, executor.ComposeCreateRequest{
			JobID: job.ID, AttemptID: attemptID, ProjectName: "gs-"+job.ID[:12], Source: source, Plans: plans,
		})
	}
	if err != nil { s.failExecution(ctx, job.ID, attemptID, err); return }
	if err := s.store.StartAttempt(ctx, attemptID, runtime); err != nil { s.failExecution(ctx, job.ID, attemptID, err); return }
	if err := s.store.ActivateAllocations(ctx, job.ID); err != nil { s.failExecution(ctx, job.ID, attemptID, err); return }
	if err := s.store.TransitionJob(ctx, job.ID, domain.JobRunning); err != nil { s.failExecution(ctx, job.ID, attemptID, err); return }
	_, _ = s.notifier.Publish(ctx, job.OwnerID, "info", "任务已启动", job.Name+" 已进入运行状态。")
}

func (s *Server) failExecution(ctx context.Context, jobID, attemptID string, err error) {
	s.log.Error("job execution failed", "job_id", jobID, "error", err)
	job, findErr := s.store.GetJob(ctx, jobID)
	if findErr == nil && job.State != domain.JobFailed { _ = s.store.TransitionJob(ctx, jobID, domain.JobFailed) }
	_ = s.store.ReleaseAllocations(ctx, jobID)
	if attemptID != "" { _ = s.store.FinishAttempt(ctx, attemptID, "FAILED", executor.RuntimeRef{}) }
	notifyJob, _ := s.store.GetJob(ctx, jobID)
	_, _ = s.notifier.Publish(ctx, notifyJob.OwnerID, "error", "任务启动失败", notifyJob.Name+": "+err.Error())
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	jobs, err := s.store.ListJobsVisible(r.Context(), identity.UserID, identity.TeamID, identity.Role)
	if err != nil { writeProblem(w, 500, "List jobs failed", err.Error()); return }
	if jobs == nil { jobs = []domain.Job{} }
	writeJSON(w, 200, jobs)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil { writeProblem(w, 404, "Job not found", ""); return }
	if !canAccessJob(identityFrom(r.Context()), job) { writeProblem(w, 403, "Forbidden", ""); return }
	writeJSON(w, 200, job)
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	job, err := s.store.GetJob(r.Context(), r.PathValue("jobId"))
	if err != nil { writeProblem(w, 404, "Job not found", ""); return }
	if !canAccessJob(identityFrom(r.Context()), job) { writeProblem(w, 403, "Forbidden", ""); return }
	var value struct { Phase string; Epoch, Step *int; Progress *float64 }
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil { writeProblem(w, 422, "Invalid heartbeat", err.Error()); return }
	if !slices.Contains([]string{"TRAINING", "CHECKPOINTING", "EVALUATING"}, value.Phase) {
		writeProblem(w, 422, "Invalid heartbeat phase", value.Phase); return
	}
	if err := s.store.RecordHeartbeat(r.Context(), job.ID, value.Phase, value.Epoch, value.Step, value.Progress); err != nil {
		writeProblem(w, 500, "Heartbeat failed", err.Error()); return
	}
	w.WriteHeader(204)
}

func (s *Server) currentAvailability(ctx context.Context) (domain.Topology, domain.Availability, error) {
	topology, err := s.agent.Topology(ctx)
	if err != nil { return domain.Topology{}, domain.Availability{}, err }
	allocations, err := s.store.ListActiveAllocations(ctx)
	if err != nil { return domain.Topology{}, domain.Availability{}, err }
	claims, err := s.agent.Claims(ctx)
	if err != nil { return domain.Topology{}, domain.Availability{}, err }
	var blockedCPU []int
	var blockedGPU []string
	for _, claim := range claims {
		if claim.ManagedJobID != "" { continue }
		if !slices.Contains([]string{"running", "created", "restarting", "paused"}, strings.ToLower(claim.State)) { continue }
		if claim.CPUUnbounded { for _, cpu := range topology.CPUs { blockedCPU = append(blockedCPU, cpu.ID) } } else { blockedCPU = append(blockedCPU, claim.CPUIds...) }
		if claim.GPUUnbounded { for _, gpu := range topology.GPUs { blockedGPU = append(blockedGPU, gpu.UUID) } } else { blockedGPU = append(blockedGPU, claim.GPUUUIDs...) }
	}
	return topology, scheduler.BuildAvailability(topology, allocations, blockedCPU, blockedGPU), nil
}

func (s *Server) protected(next http.Handler, csrf bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil { writeProblem(w, 401, "Authentication required", ""); return }
		identity, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil { writeProblem(w, 401, "Authentication required", ""); return }
		if csrf && !s.auth.VerifyCSRF(r.Context(), cookie.Value, r.Header.Get("X-CSRF-Token")) {
			writeProblem(w, 403, "CSRF validation failed", ""); return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
	})
}

func identityFrom(ctx context.Context) auth.Identity { value, _ := ctx.Value(identityKey{}).(auth.Identity); return value }

func validateJobSpec(spec domain.JobSpec, identity auth.Identity) error {
	if strings.TrimSpace(spec.Name) == "" { return errors.New("任务名称不能为空") }
	if spec.Priority == domain.PriorityEmergency && identity.Role != domain.RoleAdmin { return errors.New("P0 仅管理员可用") }
	if spec.Priority == domain.PriorityHigh && identity.Role != domain.RoleAdmin && identity.Role != domain.RoleTeamOperator { return errors.New("P1 仅团队管理员可用") }
	if spec.Workload.Kind == domain.WorkloadImage && spec.Workload.Image == "" { return errors.New("Image 不能为空") }
	if spec.Workload.Kind == domain.WorkloadCompose && spec.Workload.ComposeSourceID == "" { return errors.New("Compose 来源不能为空") }
	return nil
}

func normalizeJobSpec(spec *domain.JobSpec) {
	if spec.Priority == "" { spec.Priority = domain.PriorityNormal }
	if spec.StopGraceSeconds == 0 { spec.StopGraceSeconds = 30 }
}

func toServicePlan(name string, plan domain.JobPlan, request domain.ResourceRequest) executor.ServicePlan {
	return executor.ServicePlan{Name: name, CPUIds: plan.CPUIds, GPUUUIDs: plan.GPUUUIDs, NUMANodes: plan.NUMANodes, MemoryBytes: request.MemoryBytes}
}

func canAccessJob(identity auth.Identity, job domain.Job) bool {
	switch identity.Role {
	case domain.RoleAdmin, domain.RoleAuditor, domain.RoleViewer: return true
	case domain.RoleTeamOperator: return identity.TeamID != "" && identity.TeamID == job.TeamID
	default: return identity.UserID == job.OwnerID
	}
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok { writeProblem(w, 500, "Streaming unavailable", ""); return }
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(20*time.Second); defer ticker.Stop()
	for { select {
	case <-r.Context().Done(): return
	case now := <-ticker.C:
		_, _ = w.Write([]byte("event: keepalive\ndata: {\"at\":\""+now.UTC().Format(time.RFC3339)+"\"}\n\n")); flusher.Flush()
	} }
}

func writeJSON(w http.ResponseWriter, status int, value any) { w.Header().Set("Content-Type", "application/json; charset=utf-8"); w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
func writeProblem(w http.ResponseWriter, status int, title, detail string) { writeJSON(w, status, map[string]any{"type":"about:blank","title":title,"status":status,"detail":detail}) }

func securityHeaders(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff"); w.Header().Set("Referrer-Policy", "no-referrer"); w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
	next.ServeHTTP(w,r)
}) }

func requestLog(log *slog.Logger, next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) { start:=time.Now(); next.ServeHTTP(w,r); log.Info("http request","method",r.Method,"path",r.URL.Path,"duration",time.Since(start)) }) }

func spa(next http.Handler, assets fs.FS) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
	path:=strings.TrimPrefix(r.URL.Path,"/"); if path!="" { if _,err:=fs.Stat(assets,path);err==nil { next.ServeHTTP(w,r);return } }
	r.URL.Path="/"; next.ServeHTTP(w,r)
}) }
