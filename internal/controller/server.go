package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gpu-scheduler/internal/agentapi"
	"gpu-scheduler/internal/auth"
	"gpu-scheduler/internal/config"
	"gpu-scheduler/internal/notification"
	"gpu-scheduler/internal/scheduler"
	"gpu-scheduler/internal/store"
	"gpu-scheduler/internal/webui"
)

const sessionCookie = "gpu_scheduler_session"

type identityKey struct{}

type Server struct {
	log              *slog.Logger
	cfg              config.Config
	store            *store.Store
	agent            *agentapi.Client
	auth             *auth.Service
	scheduler        *scheduler.Engine
	reservations     *scheduler.ReservationService
	notifier         *notification.Worker
	events           *Hub
	mux              *http.ServeMux
	migRestoreBusy   atomic.Bool
	migRestoreMu     sync.Mutex
	migRestoreErrors []string
}

func New(log *slog.Logger, cfg config.Config, database *store.Store, agent *agentapi.Client, authService *auth.Service, notifier *notification.Worker) *Server {
	engine := scheduler.New()
	s := &Server{
		log: log, cfg: cfg, store: database, agent: agent, auth: authService,
		scheduler: engine, reservations: scheduler.NewReservationService(database, engine),
		notifier: notifier, events: NewHub(), mux: http.NewServeMux(),
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
	s.mux.Handle("POST /api/v1/me/password", s.protected(http.HandlerFunc(s.changePassword), true))
	s.mux.Handle("GET /api/v1/topology", s.protected(http.HandlerFunc(s.getTopology), false))
	s.mux.Handle("GET /api/v1/resources/availability", s.protected(http.HandlerFunc(s.getAvailability), false))
	s.mux.Handle("GET /api/v1/images", s.protected(http.HandlerFunc(s.getImages), false))
	s.mux.Handle("GET /api/v1/platform", s.protected(http.HandlerFunc(s.getPlatform), false))
	s.mux.Handle("GET /api/v1/unmanaged-containers", s.protected(http.HandlerFunc(s.getUnmanaged), false))
	s.mux.Handle("PATCH /api/v1/unmanaged-containers/{id}", s.protected(s.requireAdmin(s.patchHostContainer), true))
	s.mux.Handle("POST /api/v1/unmanaged-containers/{id}/stop", s.protected(s.requireAdmin(s.stopHostContainer), true))
	s.mux.Handle("POST /api/v1/unmanaged-containers/{id}/pause", s.protected(s.requireAdmin(s.pauseHostContainer), true))
	s.mux.Handle("POST /api/v1/unmanaged-containers/{id}/unpause", s.protected(s.requireAdmin(s.unpauseHostContainer), true))
	s.mux.Handle("POST /api/v1/job-plans", s.protected(http.HandlerFunc(s.planJob), true))
	s.mux.Handle("GET /api/v1/jobs", s.protected(http.HandlerFunc(s.listJobs), false))
	s.mux.Handle("POST /api/v1/jobs", s.protected(http.HandlerFunc(s.createJob), true))
	s.mux.Handle("GET /api/v1/jobs/{jobId}", s.protected(http.HandlerFunc(s.getJob), false))
	s.mux.Handle("POST /api/v1/jobs/{jobId}/actions/{action}", s.protected(http.HandlerFunc(s.actOnJob), true))
	s.mux.Handle("POST /api/v1/jobs/{jobId}/heartbeat", s.protected(http.HandlerFunc(s.heartbeat), true))
	s.mux.Handle("GET /api/v1/jobs/{jobId}/metrics", s.protected(http.HandlerFunc(s.jobMetrics), false))
	s.mux.Handle("GET /api/v1/jobs/{jobId}/logs/stream", s.protected(http.HandlerFunc(s.streamLogs), false))
	s.mux.Handle("GET /api/v1/events/stream", s.protected(http.HandlerFunc(s.streamEvents), false))
	s.mux.Handle("GET /api/v1/compose-sources", s.protected(http.HandlerFunc(s.listComposeSources), false))
	s.mux.Handle("POST /api/v1/compose-sources", s.protected(s.requireAdmin(s.createComposeSource), true))
	s.mux.Handle("POST /api/v1/compose-sources/from-path", s.protected(http.HandlerFunc(s.createComposeFromPath), true))
	s.mux.Handle("POST /api/v1/compose-sources/upload", s.protected(http.HandlerFunc(s.uploadComposeSource), true))
	s.mux.Handle("PATCH /api/v1/compose-sources/{id}", s.protected(s.requireAdmin(s.updateComposeSource), true))
	s.mux.Handle("GET /api/v1/templates", s.protected(http.HandlerFunc(s.listTemplates), false))
	s.mux.Handle("POST /api/v1/templates", s.protected(http.HandlerFunc(s.createTemplate), true))
	s.mux.Handle("PATCH /api/v1/templates/{id}", s.protected(http.HandlerFunc(s.updateTemplate), true))
	s.mux.Handle("GET /api/v1/teams", s.protected(s.requireAdmin(s.listTeams), false))
	s.mux.Handle("POST /api/v1/teams", s.protected(s.requireAdmin(s.createTeam), true))
	s.mux.Handle("GET /api/v1/users", s.protected(s.requireAdmin(s.listUsers), false))
	s.mux.Handle("POST /api/v1/users", s.protected(s.requireAdmin(s.createUser), true))
	s.mux.Handle("PATCH /api/v1/users/{id}", s.protected(s.requireAdmin(s.updateUser), true))
	s.mux.Handle("POST /api/v1/users/{id}/password", s.protected(s.requireAdmin(s.resetUserPassword), true))
	s.mux.Handle("GET /api/v1/quotas", s.protected(s.requireAdmin(s.listQuotas), false))
	s.mux.Handle("POST /api/v1/quotas", s.protected(s.requireAdmin(s.upsertQuota), true))
	s.mux.Handle("PATCH /api/v1/quotas/{id}", s.protected(s.requireAdmin(s.upsertQuota), true))
	s.mux.Handle("GET /api/v1/resource-pools", s.protected(http.HandlerFunc(s.listPools), false))
	s.mux.Handle("POST /api/v1/resource-pools", s.protected(s.requireAdmin(s.createPool), true))
	s.mux.Handle("GET /api/v1/mig/layouts", s.protected(s.requireAdmin(s.listMIGLayouts), false))
	s.mux.Handle("PUT /api/v1/mig/layouts/{gpuUuid}", s.protected(s.requireAdmin(s.applyMIGLayout), true))
	s.mux.Handle("POST /api/v1/host/reboot", s.protected(s.requireAdmin(s.rebootHost), true))
	s.mux.Handle("GET /api/v1/audit-events", s.protected(s.requireAudit(s.listAudit), false))
	s.mux.Handle("GET /api/v1/notifications", s.protected(http.HandlerFunc(s.listNotifications), false))
	s.mux.Handle("POST /api/v1/notifications/read-all", s.protected(http.HandlerFunc(s.markAllNotificationsRead), true))
	s.mux.Handle("DELETE /api/v1/notifications", s.protected(http.HandlerFunc(s.clearNotifications), true))
	s.mux.Handle("PATCH /api/v1/notifications/{id}", s.protected(http.HandlerFunc(s.readNotification), true))
	assets, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		panic(err)
	}
	s.mux.Handle("/", spa(http.FileServer(http.FS(assets)), assets))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request struct{ Username, Password string }
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeProblem(w, 400, "Invalid request", err.Error())
		return
	}
	result, err := s.auth.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		writeProblem(w, 401, "Invalid credentials", "用户名或密码错误")
		return
	}
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

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, identityFrom(r.Context()))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	err := s.auth.ChangeOwnPassword(r.Context(), identityFrom(r.Context()).UserID, body.CurrentPassword, body.NewPassword)
	if errors.Is(err, auth.ErrWrongPassword) {
		writeProblem(w, 401, "Wrong password", err.Error())
		return
	}
	if err != nil {
		writeProblem(w, 422, "Invalid password", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "user.password", "user", identityFrom(r.Context()).UserID, "")
	w.WriteHeader(204)
}

func (s *Server) getHealth(w http.ResponseWriter, r *http.Request) {
	agentStatus, status := "online", "ok"
	warnings := make([]string, 0)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	agentWarnings, err := s.agent.Health(ctx)
	if err != nil {
		agentStatus, status = "offline", "degraded"
	} else {
		warnings = agentWarnings
	}
	s.migRestoreMu.Lock()
	warnings = append(warnings, s.migRestoreErrors...)
	if len(s.migRestoreErrors) > 0 && status == "ok" {
		status = "degraded"
	}
	s.migRestoreMu.Unlock()
	writeJSON(w, 200, map[string]any{"status": status, "version": "dev", "agent": agentStatus, "warnings": warnings})
}

func (s *Server) getTopology(w http.ResponseWriter, r *http.Request) {
	topology, _, err := s.currentAvailability(r.Context())
	if err != nil {
		writeProblem(w, 503, "Agent unavailable", err.Error())
		return
	}
	writeJSON(w, 200, topology)
}

func (s *Server) getAvailability(w http.ResponseWriter, r *http.Request) {
	_, availability, err := s.currentAvailability(r.Context())
	if err != nil {
		writeProblem(w, 503, "Availability unavailable", err.Error())
		return
	}
	writeJSON(w, 200, availability)
}

func (s *Server) getImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.agent.Images(r.Context())
	if err != nil {
		writeProblem(w, 503, "Images unavailable", err.Error())
		return
	}
	writeJSON(w, 200, images)
}

func (s *Server) getPlatform(w http.ResponseWriter, r *http.Request) {
	roots := s.cfg.Agent.AllowedMountRoots
	if roots == nil {
		roots = []string{}
	}
	writeJSON(w, 200, map[string]any{"allowedMountRoots": roots})
}

func (s *Server) protected(next http.Handler, csrf bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeProblem(w, 401, "Authentication required", "")
			return
		}
		identity, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			writeProblem(w, 401, "Authentication required", "")
			return
		}
		if csrf && !s.auth.VerifyCSRF(r.Context(), cookie.Value, r.Header.Get("X-CSRF-Token")) {
			writeProblem(w, 403, "CSRF validation failed", "")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
	})
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if identityFrom(r.Context()).Role != "Admin" {
			writeProblem(w, 403, "Forbidden", "")
			return
		}
		next(w, r)
	})
}

func (s *Server) requireAudit(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := identityFrom(r.Context()).Role
		if role != "Admin" && role != "Auditor" {
			writeProblem(w, 403, "Forbidden", "")
			return
		}
		next(w, r)
	})
}

func identityFrom(ctx context.Context) auth.Identity {
	value, _ := ctx.Value(identityKey{}).(auth.Identity)
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	writeJSON(w, status, map[string]any{"type": "about:blank", "title": title, "status": status, "detail": detail})
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
