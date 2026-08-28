package controller

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gpu-scheduler/internal/agentapi"
	"gpu-scheduler/internal/store"
	"gpu-scheduler/internal/webui"
)

type Server struct {
	log   *slog.Logger
	store *store.Store
	agent *agentapi.Client
	mux   *http.ServeMux
}

func New(log *slog.Logger, store *store.Store, agent *agentapi.Client) *Server {
	s := &Server{log: log, store: store, agent: agent, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return requestLog(s.log, securityHeaders(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.getHealth)
	s.mux.HandleFunc("GET /api/v1/topology", s.getTopology)
	s.mux.HandleFunc("GET /api/v1/events/stream", s.streamEvents)
	assets, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(assets))
	s.mux.Handle("/", spa(fileServer, assets))
}

func (s *Server) getHealth(w http.ResponseWriter, r *http.Request) {
	agentStatus := "online"
	status := "ok"
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.agent.Health(ctx); err != nil {
		agentStatus = "offline"
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": status, "version": "dev", "agent": agentStatus,
	})
}

func (s *Server) getTopology(w http.ResponseWriter, r *http.Request) {
	topology, err := s.agent.Topology(r.Context())
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Agent unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, topology)
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "Streaming unavailable", "response writer does not support flushing")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case now := <-ticker.C:
			_, _ = w.Write([]byte("event: keepalive\ndata: {\"at\":\"" + now.UTC().Format(time.RFC3339) + "\"}\n\n"))
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	writeJSON(w, status, map[string]any{
		"type": "about:blank", "title": title, "status": status, "detail": detail,
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func requestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func spa(next http.Handler, assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(assets, path); err == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		next.ServeHTTP(w, r)
	})
}

