package domain

import "slices"

const (
	PermJobsCreate     = "jobs.create"
	PermJobsManage     = "jobs.manage"
	PermJobsViewAll    = "jobs.viewAll"
	PermJobsViewTeam   = "jobs.viewTeam"
	PermJobsPriorityP0 = "jobs.priorityP0"
	PermHostView       = "host.view"
	PermHostControl    = "host.control"
	PermTemplatesUse   = "templates.use"
	PermTemplatesWrite = "templates.write"
	PermAdminUsers     = "admin.users"
	PermAdminTeams     = "admin.teams"
	PermAdminQuotas    = "admin.quotas"
	PermAdminCompose   = "admin.compose"
	PermAdminPools     = "admin.pools"
	PermAdminMIG       = "admin.mig"
	PermAdminAudit     = "admin.audit"
	PermAdminRoles     = "admin.roles"
	PermAdminReboot    = "admin.reboot"
)

type PermissionDef struct {
	ID    string `json:"id"`
	Group string `json:"group"`
	Label string `json:"label"`
}

var PermissionCatalog = []PermissionDef{
	{ID: PermJobsCreate, Group: "任务", Label: "新建任务"},
	{ID: PermJobsManage, Group: "任务", Label: "暂停 / 继续 / 停止 / 重试"},
	{ID: PermJobsViewAll, Group: "任务", Label: "查看全部任务"},
	{ID: PermJobsViewTeam, Group: "任务", Label: "查看本团队任务"},
	{ID: PermJobsPriorityP0, Group: "任务", Label: "使用 P0 优先级"},
	{ID: PermHostView, Group: "容器", Label: "查看容器列表"},
	{ID: PermHostControl, Group: "容器", Label: "控制外部容器（暂停 / 停止 / 优先级）"},
	{ID: PermTemplatesUse, Group: "模板", Label: "使用任务模板"},
	{ID: PermTemplatesWrite, Group: "模板", Label: "保存 / 覆盖模板"},
	{ID: PermAdminUsers, Group: "系统管理", Label: "用户管理"},
	{ID: PermAdminTeams, Group: "系统管理", Label: "团队管理"},
	{ID: PermAdminQuotas, Group: "系统管理", Label: "配额"},
	{ID: PermAdminCompose, Group: "系统管理", Label: "Compose 来源"},
	{ID: PermAdminPools, Group: "系统管理", Label: "资源池"},
	{ID: PermAdminMIG, Group: "系统管理", Label: "MIG 切分"},
	{ID: PermAdminAudit, Group: "系统管理", Label: "审计日志"},
	{ID: PermAdminRoles, Group: "系统管理", Label: "配置角色权限"},
	{ID: PermAdminReboot, Group: "系统管理", Label: "重启本机"},
}

var AssignableRoles = []Role{RoleAdmin, RoleTeamOperator, RoleUser, RoleViewer, RoleAuditor}

func HasPermission(perms []string, perm string) bool {
	return slices.Contains(perms, perm)
}

func KnownPermission(id string) bool {
	for _, item := range PermissionCatalog {
		if item.ID == id {
			return true
		}
	}
	return false
}

func ValidAssignableRole(role string) bool {
	return slices.Contains(AssignableRoles, Role(role))
}
