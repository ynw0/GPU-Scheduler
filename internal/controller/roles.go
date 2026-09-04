package controller

import (
	"encoding/json"
	"fmt"
	"net/http"

	"gpu-scheduler/internal/domain"
)

func (s *Server) listRolePermissions(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.AllRolePermissions(r.Context())
	if err != nil {
		writeProblem(w, 500, "List role permissions failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"catalog": domain.PermissionCatalog,
		"roles":   roles,
	})
}

func (s *Server) saveRolePermissions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Roles map[string][]string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, 422, "Invalid request", err.Error())
		return
	}
	if err := validateRoleMatrix(body.Roles); err != nil {
		writeProblem(w, 422, "Invalid permissions", err.Error())
		return
	}
	if err := s.store.ReplaceAllRolePermissions(r.Context(), body.Roles); err != nil {
		writeProblem(w, 500, "Save failed", err.Error())
		return
	}
	identity := identityFrom(r.Context())
	_ = s.store.InsertAudit(r.Context(), identity.UserID, "roles.permissions", "role", "", "")
	roles, err := s.store.AllRolePermissions(r.Context())
	if err != nil {
		writeProblem(w, 500, "List role permissions failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"catalog": domain.PermissionCatalog,
		"roles":   roles,
	})
}

func validateRoleMatrix(roles map[string][]string) error {
	if roles == nil {
		return fmt.Errorf("roles 不能为空")
	}
	for role, perms := range roles {
		if !domain.ValidAssignableRole(role) {
			return fmt.Errorf("未知角色 %s", role)
		}
		for _, perm := range perms {
			if !domain.KnownPermission(perm) {
				return fmt.Errorf("未知权限 %s", perm)
			}
		}
	}
	if !domain.HasPermission(roles[string(domain.RoleAdmin)], domain.PermAdminRoles) {
		return fmt.Errorf("Admin 必须保留「配置角色权限」")
	}
	return nil
}
