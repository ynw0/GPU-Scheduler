package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"gpu-scheduler/internal/domain"
	"gpu-scheduler/internal/quota"
)

func (s *Store) ListTeams(ctx context.Context) ([]domain.Team, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,created_at FROM teams ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []domain.Team
	for rows.Next() {
		var team domain.Team
		var created string
		if err := rows.Scan(&team.ID, &team.Name, &created); err != nil {
			return nil, err
		}
		team.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (s *Store) CreateTeam(ctx context.Context, name string) (domain.Team, error) {
	team := domain.Team{ID: uuid.NewString(), Name: name, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx, "INSERT INTO teams(id,name,created_at) VALUES(?,?,?)",
		team.ID, team.Name, team.CreatedAt.Format(time.RFC3339Nano))
	return team, err
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.UserAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,COALESCE(email,''),role,COALESCE(team_id,''),disabled_at,created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []domain.UserAccount
	for rows.Next() {
		user, err := scanUserAccount(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) GetUser(ctx context.Context, id string) (domain.UserAccount, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,username,COALESCE(email,''),role,COALESCE(team_id,''),disabled_at,created_at FROM users WHERE id=?`, id)
	return scanUserAccount(row)
}

func (s *Store) UpdateUser(ctx context.Context, id string, role domain.Role, teamID, email string, disabled bool) error {
	var disabledAt any
	if disabled {
		disabledAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, "UPDATE users SET role=?,team_id=?,email=?,disabled_at=? WHERE id=?",
		role, nullText(teamID), nullText(email), disabledAt, id)
	return err
}

func (s *Store) ListQuotas(ctx context.Context) ([]domain.Quota, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope_type,scope_id,gpu_limit,cpu_thread_limit,memory_bytes_limit FROM quotas ORDER BY scope_type,scope_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Quota
	for rows.Next() {
		var item domain.Quota
		if err := rows.Scan(&item.ID, &item.ScopeType, &item.ScopeID, &item.GPULimit, &item.CPUThreadLimit, &item.MemoryBytesLimit); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) UpsertQuota(ctx context.Context, item domain.Quota) (domain.Quota, error) {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO quotas(id,scope_type,scope_id,gpu_limit,cpu_thread_limit,memory_bytes_limit)
        VALUES(?,?,?,?,?,?) ON CONFLICT(scope_type,scope_id) DO UPDATE SET
        gpu_limit=excluded.gpu_limit,cpu_thread_limit=excluded.cpu_thread_limit,memory_bytes_limit=excluded.memory_bytes_limit`,
		item.ID, item.ScopeType, item.ScopeID, item.GPULimit, item.CPUThreadLimit, item.MemoryBytesLimit)
	return item, err
}

func (s *Store) QuotaFor(ctx context.Context, scopeType, scopeID string) (quota.Limit, bool, error) {
	var limit quota.Limit
	err := s.db.QueryRowContext(ctx, `SELECT gpu_limit,cpu_thread_limit,memory_bytes_limit FROM quotas WHERE scope_type=? AND scope_id=?`,
		scopeType, scopeID).Scan(&limit.GPUCount, &limit.CPUThreadCount, &limit.MemoryBytes)
	if err == sql.ErrNoRows {
		return quota.Limit{GPUCount: -1, CPUThreadCount: -1}, false, nil
	}
	return limit, err == nil, err
}

func (s *Store) QuotaUsage(ctx context.Context, ownerID, teamID string) (userUsage, teamUsage quota.Usage, err error) {
	userUsage, err = s.usage(ctx, "owner_id", ownerID)
	if err != nil {
		return
	}
	if teamID != "" {
		teamUsage, err = s.usage(ctx, "team_id", teamID)
	}
	return
}

func (s *Store) usage(ctx context.Context, column, id string) (quota.Usage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.resource_type, a.resource_id, a.amount_bytes
        FROM allocations a JOIN jobs j ON j.id=a.job_id
        WHERE a.state IN ('RESERVED','ACTIVE','RELEASING') AND j.`+column+`=?`, id)
	if err != nil {
		return quota.Usage{}, err
	}
	defer rows.Close()
	var usage quota.Usage
	for rows.Next() {
		var kind, resourceID string
		var bytes uint64
		if err := rows.Scan(&kind, &resourceID, &bytes); err != nil {
			return quota.Usage{}, err
		}
		switch kind {
		case "gpu":
			usage.GPUIds = append(usage.GPUIds, resourceID)
		case "cpu":
			usage.CPUThreadCount++
		case "memory_node":
			usage.MemoryBytes += bytes
		}
	}
	return usage, rows.Err()
}

func (s *Store) ListComposeSources(ctx context.Context, enabledOnly bool) ([]domain.ComposeSource, error) {
	query := `SELECT id,name,root_path,entry_file,enabled,created_at FROM compose_sources`
	if enabledOnly {
		query += " WHERE enabled=1"
	}
	query += " ORDER BY name"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.ComposeSource
	for rows.Next() {
		var item domain.ComposeSource
		var enabled int
		var created string
		if err := rows.Scan(&item.ID, &item.Name, &item.RootPath, &item.EntryFile, &enabled, &created); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateComposeSource(ctx context.Context, name, root, entry string) (domain.ComposeSource, error) {
	item := domain.ComposeSource{ID: uuid.NewString(), Name: name, RootPath: root, EntryFile: entry, Enabled: true, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO compose_sources(id,name,root_path,entry_file,enabled,created_at) VALUES(?,?,?,?,1,?)`,
		item.ID, item.Name, item.RootPath, item.EntryFile, item.CreatedAt.Format(time.RFC3339Nano))
	return item, err
}

func (s *Store) UpdateComposeSource(ctx context.Context, id, name, root, entry string, enabled bool) error {
	flag := 0
	if enabled {
		flag = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE compose_sources SET name=?,root_path=?,entry_file=?,enabled=? WHERE id=?`,
		name, root, entry, flag, id)
	return err
}

func (s *Store) ListTemplates(ctx context.Context, ownerID string, admin bool) ([]domain.Template, error) {
	query := `SELECT id,name,owner_id,spec_json,created_at,updated_at FROM templates`
	args := []any{}
	if !admin {
		query += " WHERE owner_id=?"
		args = append(args, ownerID)
	}
	query += " ORDER BY updated_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Template
	for rows.Next() {
		item, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) CreateTemplate(ctx context.Context, ownerID, name string, spec domain.JobSpec) (domain.Template, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return domain.Template{}, err
	}
	now := time.Now().UTC()
	item := domain.Template{ID: uuid.NewString(), Name: name, OwnerID: ownerID, Spec: spec, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO templates(id,name,owner_id,spec_json,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		item.ID, name, ownerID, string(encoded), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return item, err
}

func (s *Store) UpdateTemplate(ctx context.Context, id, name string, spec domain.JobSpec) error {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE templates SET name=?,spec_json=?,updated_at=? WHERE id=?`,
		name, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) GetTemplate(ctx context.Context, id string) (domain.Template, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,owner_id,spec_json,created_at,updated_at FROM templates WHERE id=?`, id)
	return scanTemplate(row)
}

func (s *Store) ListResourcePools(ctx context.Context) ([]domain.ResourcePool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,cpu_ids_json,gpu_uuids_json,numa_nodes_json,shared,created_at FROM resource_pools ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.ResourcePool
	for rows.Next() {
		item, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetResourcePool(ctx context.Context, id string) (domain.ResourcePool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,cpu_ids_json,gpu_uuids_json,numa_nodes_json,shared,created_at FROM resource_pools WHERE id=?`, id)
	return scanPool(row)
}

func (s *Store) CreateResourcePool(ctx context.Context, pool domain.ResourcePool) (domain.ResourcePool, error) {
	pool.ID = uuid.NewString()
	pool.CreatedAt = time.Now().UTC()
	cpuJSON, _ := json.Marshal(pool.CPUIds)
	gpuJSON, _ := json.Marshal(pool.GPUUUIDs)
	numaJSON, _ := json.Marshal(pool.NUMANodes)
	_, err := s.db.ExecContext(ctx, `INSERT INTO resource_pools(id,name,cpu_ids_json,gpu_uuids_json,numa_nodes_json,shared,created_at)
        VALUES(?,?,?,?,?,?,?)`, pool.ID, pool.Name, string(cpuJSON), string(gpuJSON), string(numaJSON), boolInt(pool.Shared), pool.CreatedAt.Format(time.RFC3339Nano))
	return pool, err
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(actor_user_id,''),action,target_type,target_id,detail_json,created_at
        FROM audit_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.AuditEvent
	for rows.Next() {
		var item domain.AuditEvent
		var created string
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.TargetType, &item.TargetID, &item.Detail, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListNotifications(ctx context.Context, userID string) ([]domain.Notification, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(user_id,''),severity,title,body,read_at,created_at
        FROM notifications WHERE user_id=? OR user_id IS NULL ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Notification
	for rows.Next() {
		var item domain.Notification
		var created string
		var read sql.NullString
		if err := rows.Scan(&item.ID, &item.UserID, &item.Severity, &item.Title, &item.Body, &read, &created); err != nil {
			return nil, err
		}
		if read.Valid {
			parsed, _ := time.Parse(time.RFC3339Nano, read.String)
			item.ReadAt = &parsed
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) MarkNotificationRead(ctx context.Context, id, userID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE notifications SET read_at=? WHERE id=? AND user_id=?",
		time.Now().UTC().Format(time.RFC3339Nano), id, userID)
	return err
}

func scanUserAccount(row scanner) (domain.UserAccount, error) {
	var user domain.UserAccount
	var disabled, created string
	var disabledAt sql.NullString
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.TeamID, &disabledAt, &created)
	if err != nil {
		return domain.UserAccount{}, err
	}
	user.Disabled = disabledAt.Valid
	user.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	_ = disabled
	return user, nil
}

func scanTemplate(row scanner) (domain.Template, error) {
	var item domain.Template
	var specJSON, created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.OwnerID, &specJSON, &created, &updated); err != nil {
		return domain.Template{}, err
	}
	if err := json.Unmarshal([]byte(specJSON), &item.Spec); err != nil {
		return domain.Template{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func scanPool(row scanner) (domain.ResourcePool, error) {
	var item domain.ResourcePool
	var cpuJSON, gpuJSON, numaJSON, created string
	var shared int
	if err := row.Scan(&item.ID, &item.Name, &cpuJSON, &gpuJSON, &numaJSON, &shared, &created); err != nil {
		return domain.ResourcePool{}, err
	}
	_ = json.Unmarshal([]byte(cpuJSON), &item.CPUIds)
	_ = json.Unmarshal([]byte(gpuJSON), &item.GPUUUIDs)
	_ = json.Unmarshal([]byte(numaJSON), &item.NUMANodes)
	item.Shared = shared == 1
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
