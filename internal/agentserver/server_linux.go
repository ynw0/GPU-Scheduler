//go:build linux

package agentserver

import (
	"encoding/json"
	"net/http"

	"gpu-scheduler/internal/config"
	"gpu-scheduler/internal/executor"
	"gpu-scheduler/internal/inventory"
	"gpu-scheduler/internal/monitor"
)

type Server struct {
	discoverer *inventory.LinuxDiscoverer
	docker     *executor.Docker
	compose    *executor.Compose
	validator  *executor.SecurityValidator
	monitor    *monitor.LinuxCollector
	mux        *http.ServeMux
}

func New(cfg config.Config) (*Server, error) {
	docker, err := executor.NewDocker()
	if err != nil { return nil, err }
	validator, err := executor.NewSecurityValidator(cfg.Agent.AllowedMountRoots)
	if err != nil { docker.Close(); return nil, err }
	collector, err := monitor.NewLinuxCollector()
	if err != nil { docker.Close(); return nil, err }
	s := &Server{
		discoverer: inventory.NewLinuxDiscoverer(), docker: docker, validator: validator,
		compose: executor.NewCompose(docker, validator, cfg.Agent.StateDirectory), monitor: collector,
		mux: http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Close() {
	s.monitor.Close()
	_ = s.docker.Close()
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /internal/v1/health", s.health)
	s.mux.HandleFunc("GET /internal/v1/topology", s.discoverer.HandleTopology)
	s.mux.HandleFunc("GET /internal/v1/images", s.images)
	s.mux.HandleFunc("GET /internal/v1/metrics/host", s.metrics)
	s.mux.HandleFunc("GET /internal/v1/container-claims", s.claims)
	s.mux.HandleFunc("POST /internal/v1/workloads/image", s.createImage)
	s.mux.HandleFunc("POST /internal/v1/workloads/compose", s.createCompose)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.docker.Ping(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"status": "degraded", "detail": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	items, err := s.docker.ListImages(r.Context())
	if err != nil { writeError(w, err); return }
	writeJSON(w, 200, items)
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	snapshot, err := s.monitor.Sample()
	if err != nil { writeError(w, err); return }
	writeJSON(w, 200, snapshot)
}

func (s *Server) claims(w http.ResponseWriter, r *http.Request) {
	claims, err := s.docker.DiscoverContainerClaims(r.Context())
	if err != nil { writeError(w, err); return }
	writeJSON(w, 200, claims)
}

func (s *Server) createImage(w http.ResponseWriter, r *http.Request) {
	var request executor.ImageCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil { writeJSON(w, 400, map[string]string{"error": err.Error()}); return }
	if err := s.validator.ValidateImageMounts(request.Workload.Mounts); err != nil { writeJSON(w, 422, map[string]string{"error": err.Error()}); return }
	ref, err := s.docker.CreateImage(r.Context(), request)
	if err != nil { writeError(w, err); return }
	writeJSON(w, 201, ref)
}

type composeRequest struct {
	Request   executor.ComposeCreateRequest `json:"request"`
	Effective *executor.EffectiveCompose    `json:"effective,omitempty"`
}

func (s *Server) createCompose(w http.ResponseWriter, r *http.Request) {
	var body composeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil { writeJSON(w, 400, map[string]string{"error": err.Error()}); return }
	effective, err := s.compose.Prepare(r.Context(), body.Request)
	if err != nil { writeJSON(w, 422, map[string]string{"error": err.Error()}); return }
	ref, err := s.compose.CreateAndStart(r.Context(), body.Request, effective)
	if err != nil { writeError(w, err); return }
	writeJSON(w, 201, map[string]any{"runtime": ref, "effective": effective})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, 503, map[string]any{"type": "about:blank", "title": "Agent operation failed", "status": 503, "detail": err.Error()})
}

