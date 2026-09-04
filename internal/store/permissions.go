package store

import (
	"context"
	"database/sql"

	"gpu-scheduler/internal/domain"
)

func (s *Store) PermissionsForRole(ctx context.Context, role domain.Role) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT permission FROM role_permissions WHERE role=? ORDER BY permission`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return nil, err
		}
		result = append(result, perm)
	}
	return result, rows.Err()
}

func (s *Store) AllRolePermissions(ctx context.Context) (map[string][]string, error) {
	result := make(map[string][]string, len(domain.AssignableRoles))
	for _, role := range domain.AssignableRoles {
		result[string(role)] = []string{}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT role, permission FROM role_permissions ORDER BY role, permission`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			return nil, err
		}
		result[role] = append(result[role], perm)
	}
	return result, rows.Err()
}

func (s *Store) ReplaceAllRolePermissions(ctx context.Context, roles map[string][]string) error {
	return s.Immediate(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `DELETE FROM role_permissions`); err != nil {
			return err
		}
		for _, role := range domain.AssignableRoles {
			seen := map[string]struct{}{}
			for _, perm := range roles[string(role)] {
				if _, ok := seen[perm]; ok {
					continue
				}
				seen[perm] = struct{}{}
				if _, err := conn.ExecContext(ctx, `INSERT INTO role_permissions(role, permission) VALUES(?,?)`, role, perm); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
