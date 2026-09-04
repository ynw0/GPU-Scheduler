import { Fragment, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import type { PermissionDef, User } from './types'

const roleOrder: User['role'][] = ['Admin', 'TeamOperator', 'User', 'Viewer', 'Auditor']
const roleLabel: Record<User['role'], string> = {
  Admin: '管理员',
  TeamOperator: '团队操作',
  User: '用户',
  Viewer: '只读',
  Auditor: '审计',
}

function cloneRoles(source: Record<string, string[]>): Record<string, string[]> {
  const next: Record<string, string[]> = {}
  for (const role of roleOrder) {
    next[role] = [...(source[role] ?? [])]
  }
  return next
}

function groupsOf(catalog: PermissionDef[]): Array<{ name: string; items: PermissionDef[] }> {
  const groups: Array<{ name: string; items: PermissionDef[] }> = []
  for (const item of catalog) {
    const last = groups[groups.length - 1]
    if (!last || last.name !== item.group) {
      groups.push({ name: item.group, items: [item] })
      continue
    }
    last.items.push(item)
  }
  return groups
}

export function RolePermissions() {
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey: ['role-permissions'], queryFn: api.rolePermissions })
  const [draft, setDraft] = useState<Record<string, string[]> | undefined>()
  useEffect(() => {
    if (query.data) setDraft(cloneRoles(query.data.roles))
  }, [query.data])

  const save = useMutation({
    mutationFn: () => api.saveRolePermissions(draft ?? {}),
    onSuccess: (value) => {
      queryClient.setQueryData(['role-permissions'], value)
      void queryClient.invalidateQueries({ queryKey: ['me'] })
    },
  })

  const groups = useMemo(() => groupsOf(query.data?.catalog ?? []), [query.data])
  if (!query.data || !draft) {
    return <div className="empty-row compact"><span>{query.isError ? String(query.error) : '正在读取角色权限…'}</span></div>
  }

  const toggle = (role: User['role'], perm: string) => {
    if (role === 'Admin' && perm === 'admin.roles') return
    setDraft((current) => {
      const next = cloneRoles(current ?? {})
      const list = next[role]
      next[role] = list.includes(perm) ? list.filter((item) => item !== perm) : [...list, perm]
      return next
    })
  }

  return (
    <div className="perm-matrix">
      <p className="field-hint">勾选后点保存。之后该角色的账号按这张表生效。</p>
      <div className="perm-scroll">
        <table className="perm-table">
          <thead>
            <tr>
              <th>权限</th>
              {roleOrder.map((role) => <th key={role}>{roleLabel[role]}<small>{role}</small></th>)}
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => (
              <Fragment key={group.name}>
                <tr className="perm-group"><td colSpan={roleOrder.length + 1}>{group.name}</td></tr>
                {group.items.map((item) => (
                  <tr key={item.id}>
                    <td>{item.label}</td>
                    {roleOrder.map((role) => {
                      const locked = role === 'Admin' && item.id === 'admin.roles'
                      const checked = draft[role]?.includes(item.id)
                      return (
                        <td key={role}>
                          <input
                            type="checkbox"
                            checked={Boolean(checked)}
                            disabled={locked}
                            onChange={() => toggle(role, item.id)}
                            aria-label={`${roleLabel[role]} ${item.label}`}
                          />
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
      {save.error && <div className="plan-conflict">{String(save.error.message)}</div>}
      <button className="primary-action" type="button" disabled={save.isPending} onClick={() => save.mutate()}>
        {save.isPending ? '正在保存…' : '保存权限'}
      </button>
    </div>
  )
}
