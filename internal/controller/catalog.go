package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/scheduler"
)

func (s *Server) listComposeSources(w http.ResponseWriter, r *http.Request) {
	enabledOnly := !identityFrom(r.Context()).Has(domain.PermAdminCompose)
	items, err := s.store.ListComposeSources(r.Context(), enabledOnly)
	if err != nil {
		writeProblem(w, 500, "List compose sources failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.ComposeSource{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createComposeSource(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name, RootPath, EntryFile string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	item, err := s.store.CreateComposeSource(r.Context(), body.Name, body.RootPath, body.EntryFile)
	if err != nil {
		writeProblem(w, 409, "Create failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "compose-source.create", "compose_source", item.ID, item.Name)
	writeJSON(w, 201, item)
}

func (s *Server) createComposeFromPath(w http.ResponseWriter, r *http.Request) {
	if !canCreateJobs(identityFrom(r.Context())) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	var body struct{ Name, FilePath string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	root, entry, err := resolveComposeFilePath(body.FilePath)
	if err != nil {
		writeProblem(w, 422, "Invalid compose path", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = entry
	}
	item, err := s.store.CreateComposeSource(r.Context(), name, root, entry)
	if err != nil {
		writeProblem(w, 409, "Create failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "compose-source.create", "compose_source", item.ID, name)
	writeJSON(w, 201, item)
}

func (s *Server) uploadComposeSource(w http.ResponseWriter, r *http.Request) {
	if !canCreateJobs(identityFrom(r.Context())) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	var body struct{ Name, FileName, Content string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeProblem(w, 422, "Invalid request", "compose 内容为空")
		return
	}
	entry := "compose.yaml"
	switch strings.ToLower(filepath.Ext(body.FileName)) {
	case ".yml":
		entry = "compose.yml"
	case ".yaml", "":
		entry = "compose.yaml"
	default:
		writeProblem(w, 422, "Invalid request", "只接受 .yaml 或 .yml")
		return
	}
	id := uuid.NewString()
	root := filepath.Join(s.cfg.Agent.StateDirectory, "compose-uploads", id)
	if err := os.MkdirAll(root, 0o750); err != nil {
		writeProblem(w, 500, "Create failed", err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(root, entry), []byte(body.Content), 0o640); err != nil {
		writeProblem(w, 500, "Create failed", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = entry
	}
	item, err := s.store.CreateComposeSource(r.Context(), name, root, entry)
	if err != nil {
		writeProblem(w, 409, "Create failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "compose-source.upload", "compose_source", item.ID, name)
	writeJSON(w, 201, item)
}

func resolveComposeFilePath(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("路径不能为空")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("路径必须是 compose 文件")
	}
	switch strings.ToLower(filepath.Ext(absolute)) {
	case ".yaml", ".yml":
	default:
		return "", "", fmt.Errorf("只接受 .yaml 或 .yml")
	}
	return filepath.Dir(absolute), filepath.Base(absolute), nil
}

func (s *Server) updateComposeSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, RootPath, EntryFile string
		Enabled                   *bool
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if err := s.store.UpdateComposeSource(r.Context(), r.PathValue("id"), body.Name, body.RootPath, body.EntryFile, enabled); err != nil {
		writeProblem(w, 500, "Update failed", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	items, err := s.store.ListTemplates(r.Context(), identity.UserID, identity.Has(domain.PermJobsViewAll))
	if err != nil {
		writeProblem(w, 500, "List templates failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Template{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	if !identity.Has(domain.PermTemplatesWrite) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	var body struct {
		Name string
		Spec domain.JobSpec
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	item, err := s.store.CreateTemplate(r.Context(), identity.UserID, body.Name, body.Spec)
	if err != nil {
		writeProblem(w, 500, "Create failed", err.Error())
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	identity := identityFrom(r.Context())
	item, err := s.store.GetTemplate(r.Context(), r.PathValue("id"))
	if err != nil {
		writeProblem(w, 404, "Template not found", "")
		return
	}
	if !identity.Has(domain.PermTemplatesWrite) || (item.OwnerID != identity.UserID && !identity.Has(domain.PermJobsViewAll)) {
		writeProblem(w, 403, "Forbidden", "")
		return
	}
	var body struct {
		Name string
		Spec domain.JobSpec
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	if err := s.store.UpdateTemplate(r.Context(), item.ID, body.Name, body.Spec); err != nil {
		writeProblem(w, 500, "Update failed", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListTeams(r.Context())
	if err != nil {
		writeProblem(w, 500, "List teams failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Team{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	item, err := s.store.CreateTeam(r.Context(), body.Name)
	if err != nil {
		writeProblem(w, 409, "Create failed", err.Error())
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeProblem(w, 500, "List users failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.UserAccount{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username, Email, Password, TeamID string
		Role                              domain.Role
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	item, err := s.auth.CreateUser(r.Context(), body.Username, body.Email, body.Password, body.Role, body.TeamID)
	if err != nil {
		writeProblem(w, 409, "Create failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "user.create", "user", item.UserID, body.Username)
	writeJSON(w, 201, item)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role     domain.Role `json:"role"`
		TeamID   string      `json:"teamId"`
		Email    *string     `json:"email"`
		Disabled *bool       `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	id := r.PathValue("id")
	current, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		writeProblem(w, 404, "User not found", "")
		return
	}
	role := body.Role
	if role == "" {
		role = current.Role
	}
	disabled := current.Disabled
	if body.Disabled != nil {
		disabled = *body.Disabled
	}
	email := current.Email
	if body.Email != nil {
		email = *body.Email
	}
	if err := s.store.UpdateUser(r.Context(), id, role, body.TeamID, email, disabled); err != nil {
		writeProblem(w, 500, "Update failed", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "user.update", "user", id, string(role))
	w.WriteHeader(204)
}

func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.GetUser(r.Context(), id); err != nil {
		writeProblem(w, 404, "User not found", "")
		return
	}
	if err := s.auth.SetPassword(r.Context(), id, body.NewPassword); err != nil {
		writeProblem(w, 422, "Invalid password", err.Error())
		return
	}
	_ = s.store.InsertAudit(r.Context(), identityFrom(r.Context()).UserID, "user.password.reset", "user", id, "")
	w.WriteHeader(204)
}

func (s *Server) listQuotas(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListQuotas(r.Context())
	if err != nil {
		writeProblem(w, 500, "List quotas failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Quota{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) upsertQuota(w http.ResponseWriter, r *http.Request) {
	var item domain.Quota
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	if id := r.PathValue("id"); id != "" {
		item.ID = id
	}
	saved, err := s.store.UpsertQuota(r.Context(), item)
	if err != nil {
		writeProblem(w, 409, "Save failed", err.Error())
		return
	}
	writeJSON(w, 200, saved)
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListResourcePools(r.Context())
	if err != nil {
		writeProblem(w, 500, "List pools failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.ResourcePool{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	var pool domain.ResourcePool
	if err := json.NewDecoder(r.Body).Decode(&pool); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	topology, err := s.liveTopology(r.Context())
	if err != nil {
		writeProblem(w, 503, "Topology unavailable", err.Error())
		return
	}
	existing, err := s.store.ListResourcePools(r.Context())
	if err != nil {
		writeProblem(w, 500, "List pools failed", err.Error())
		return
	}
	if err := scheduler.ValidatePool(topology, pool, existing); err != nil {
		writeProblem(w, 422, "Invalid resource pool", err.Error())
		return
	}
	saved, err := s.store.CreateResourcePool(r.Context(), pool)
	if err != nil {
		writeProblem(w, 409, "Create failed", err.Error())
		return
	}
	writeJSON(w, 201, saved)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAuditEvents(r.Context(), 200)
	if err != nil {
		writeProblem(w, 500, "List audit failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.AuditEvent{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListNotifications(r.Context(), identityFrom(r.Context()).UserID)
	if err != nil {
		writeProblem(w, 500, "List notifications failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.Notification{}
	}
	writeJSON(w, 200, items)
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MarkNotificationRead(r.Context(), r.PathValue("id"), identityFrom(r.Context()).UserID); err != nil {
		writeProblem(w, 500, "Update failed", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MarkAllNotificationsRead(r.Context(), identityFrom(r.Context()).UserID); err != nil {
		writeProblem(w, 500, "Update failed", err.Error())
		return
	}
	w.WriteHeader(204)
}

func (s *Server) clearNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearNotifications(r.Context(), identityFrom(r.Context()).UserID); err != nil {
		writeProblem(w, 500, "Clear failed", err.Error())
		return
	}
	w.WriteHeader(204)
}
