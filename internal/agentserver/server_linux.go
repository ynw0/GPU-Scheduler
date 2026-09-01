//go:build linux

package agentserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"gpu-scheduler/internal/config"
	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/executor"
	"gpu-scheduler/internal/heartbeat"
	"gpu-scheduler/internal/host"
	"gpu-scheduler/internal/monitor"
	"gpu-scheduler/internal/platform"
)

type Server struct {
	discoverer *host.LinuxDiscoverer
	docker     *executor.Docker
	compose    *executor.Compose
	validator  *executor.SecurityValidator
	monitor    *monitor.LinuxCollector
	heartbeats *heartbeat.Hub
	platform   platform.Runtime
	mux        *http.ServeMux
}

func New(ctx context.Context, cfg config.Config) (*Server, error) {
	docker, err := executor.NewDocker(cfg.Agent.DockerSocket)
	if err != nil {
		return nil, err
	}
	info, version, err := docker.PlatformInfo(ctx)
	if err != nil {
		docker.Close()
		return nil, err
	}
	target, err := platform.Validate(ctx, cfg.Agent.DockerSocket, info.Info, version)
	if err != nil {
		docker.Close()
		return nil, err
	}
	validator, err := executor.NewSecurityValidator(cfg.Agent.AllowedMountRoots)
	if err != nil {
		docker.Close()
		return nil, err
	}
	collector, err := monitor.NewLinuxCollector()
	if err != nil {
		docker.Close()
		return nil, err
	}
	s := &Server{
		discoverer: host.NewLinuxDiscoverer(), docker: docker, validator: validator,
		compose: executor.NewCompose(docker, validator, cfg.Agent.StateDirectory, cfg.Agent.DockerSocket),
		monitor: collector, heartbeats: heartbeat.NewHub(cfg.Agent.StateDirectory), platform: target,
		mux: http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Close() {
	s.heartbeats.Close()
	s.monitor.Close()
	_ = s.docker.Close()
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /internal/v1/health", s.health)
	s.mux.HandleFunc("GET /internal/v1/topology", s.discoverer.HandleTopology)
	s.mux.HandleFunc("GET /internal/v1/images", s.images)
	s.mux.HandleFunc("GET /internal/v1/metrics/host", s.metrics)
	s.mux.HandleFunc("GET /internal/v1/metrics/container", s.containerMetrics)
	s.mux.HandleFunc("GET /internal/v1/container-claims", s.claims)
	s.mux.HandleFunc("POST /internal/v1/workloads/image", s.createImage)
	s.mux.HandleFunc("POST /internal/v1/workloads/compose", s.createCompose)
	s.mux.HandleFunc("POST /internal/v1/workloads/stop", s.stopWorkload)
	s.mux.HandleFunc("POST /internal/v1/workloads/remove", s.removeAttempt)
	s.mux.HandleFunc("GET /internal/v1/workloads/logs", s.logs)
	s.mux.HandleFunc("GET /internal/v1/gpu-host-processes", s.hostGPUProcesses)
	s.mux.HandleFunc("POST /internal/v1/containers/stop", s.stopContainer)
	s.mux.HandleFunc("GET /internal/v1/heartbeats", s.listHeartbeats)
	s.mux.HandleFunc("POST /internal/v1/heartbeats/ensure", s.ensureHeartbeat)
	s.mux.HandleFunc("POST /internal/v1/heartbeats/close", s.closeHeartbeat)
}

func (s *Server) containerMetrics(w http.ResponseWriter, r *http.Request) {
	containerID := r.URL.Query().Get("containerId")
	if containerID == "" {
		writeJSON(w, 400, map[string]string{"error": "containerId required"})
		return
	}
	pid, err := s.docker.ContainerPID(r.Context(), containerID)
	if err != nil {
		writeError(w, err)
		return
	}
	snapshot, err := s.monitor.SampleContainer(containerID, pid)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, snapshot)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.docker.Ping(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"status": "degraded", "detail": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "platform": s.platform})
}

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	items, err := s.docker.ListImages(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, items)
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	snapshot, err := s.monitor.Sample()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, snapshot)
}

func (s *Server) claims(w http.ResponseWriter, r *http.Request) {
	claims, err := s.docker.DiscoverContainerClaims(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	compute, err := s.monitor.GPUCompute()
	if err != nil {
		compute = monitor.GPUCompute{ByContainer: map[string][]string{}}
	}
	for i := range claims {
		for containerID, uuids := range compute.ByContainer {
			if monitor.MatchContainerID(claims[i].ContainerID, containerID) {
				claims[i].TrainingGPUUUIDs = append(claims[i].TrainingGPUUUIDs, uuids...)
			}
		}
		s.enrichRuntime(claims[i].ContainerID, &claims[i])
	}
	writeJSON(w, 200, claims)
}

func (s *Server) enrichRuntime(containerID string, claim *domain.ContainerClaim) {
	claim.GPUActivity = len(claim.TrainingGPUUUIDs) > 0
	if claim.PID <= 0 || (claim.State != "running" && claim.State != "paused") {
		return
	}
	sample, err := s.monitor.SampleContainer(containerID, claim.PID)
	if err != nil {
		return
	}
	claim.OOMUnder = sample.OOMUnder
	claim.MemoryUsedBytes = sample.MemoryUsedBytes
	claim.MemoryLimitBytes = sample.MemoryLimitBytes
	var sum float64
	for _, percent := range sample.CPUPercentByID {
		sum += percent
		if percent >= 0.5 {
			claim.CPUActivity = true
		}
	}
	if n := len(sample.CPUPercentByID); n > 0 {
		claim.CPUPercent = sum / float64(n)
	}
}

func (s *Server) createImage(w http.ResponseWriter, r *http.Request) {
	var request executor.ImageCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := s.validator.ValidateImageMounts(request.Workload.Mounts); err != nil {
		writeJSON(w, 422, map[string]string{"error": err.Error()})
		return
	}
	dir, err := s.heartbeats.Ensure(request.AttemptID)
	if err != nil {
		writeError(w, err)
		return
	}
	request.HeartbeatDir = dir
	ref, err := s.docker.CreateImage(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, ref)
}

type composeRequest struct {
	Request   executor.ComposeCreateRequest `json:"request"`
	Effective *executor.EffectiveCompose    `json:"effective,omitempty"`
}

func (s *Server) createCompose(w http.ResponseWriter, r *http.Request) {
	var body composeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if needsTrainingHeartbeat(body.Request.Plans) {
		if _, err := s.heartbeats.Ensure(body.Request.AttemptID); err != nil {
			writeError(w, err)
			return
		}
	}
	effective, err := s.compose.Prepare(r.Context(), body.Request)
	if err != nil {
		writeJSON(w, 422, map[string]string{"error": err.Error()})
		return
	}
	ref, err := s.compose.CreateAndStart(r.Context(), body.Request, effective)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"runtime": ref, "effective": effective})
}

func (s *Server) stopWorkload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Runtime        executor.RuntimeRef `json:"runtime"`
		TimeoutSeconds int                 `json:"timeoutSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := s.docker.Stop(r.Context(), body.Runtime, body.TimeoutSeconds); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) removeAttempt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AttemptID string `json:"attemptId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := s.docker.RemoveAttempt(r.Context(), body.AttemptID); err != nil {
		writeError(w, err)
		return
	}
	_ = s.heartbeats.CloseAttempt(body.AttemptID)
	w.WriteHeader(204)
}

func (s *Server) listHeartbeats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.heartbeats.Latest())
}

func (s *Server) ensureHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AttemptID string `json:"attemptId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AttemptID == "" {
		writeJSON(w, 400, map[string]string{"error": "attemptId required"})
		return
	}
	if _, err := s.heartbeats.Ensure(body.AttemptID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) closeHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AttemptID string `json:"attemptId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AttemptID == "" {
		writeJSON(w, 400, map[string]string{"error": "attemptId required"})
		return
	}
	if err := s.heartbeats.CloseAttempt(body.AttemptID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func needsTrainingHeartbeat(plans map[string]executor.ServicePlan) bool {
	for _, plan := range plans {
		if plan.Training {
			return true
		}
	}
	return false
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	containerID := r.URL.Query().Get("containerId")
	if containerID == "" {
		writeJSON(w, 400, map[string]string{"error": "containerId required"})
		return
	}
	reader, err := s.docker.Logs(r.Context(), containerID, r.URL.Query().Get("follow") == "1")
	if err != nil {
		writeError(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(200)
	_, _ = io.Copy(w, reader)
}

func (s *Server) stopContainer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContainerID    string `json:"containerId"`
		TimeoutSeconds int    `json:"timeoutSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ContainerID == "" {
		writeJSON(w, 400, map[string]string{"error": "containerId required"})
		return
	}
	timeout := body.TimeoutSeconds
	if timeout <= 0 {
		timeout = 10
	}
	if err := s.docker.Stop(r.Context(), executor.RuntimeRef{ContainerIDs: map[string]string{"main": body.ContainerID}}, timeout); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) hostGPUProcesses(w http.ResponseWriter, _ *http.Request) {
	uuids, err := s.monitor.HostOccupiedGPUs()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, uuids)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, 503, map[string]any{"type": "about:blank", "title": "Agent operation failed", "status": 503, "detail": err.Error()})
}
