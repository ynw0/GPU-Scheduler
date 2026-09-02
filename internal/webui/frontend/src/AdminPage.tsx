import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { cpuSelectable, resourceNuma, selectedGPUNuma } from './numa'
import { MigSettings } from './MigSettings'
import type { ComposeSource, Team, User } from './types'

export function AdminPage({ role }: { role: User['role'] }) {
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<'users' | 'teams' | 'quotas' | 'compose' | 'pools' | 'mig' | 'audit'>('users')
  const users = useQuery({ queryKey: ['users'], queryFn: api.users, enabled: role === 'Admin' })
  const teams = useQuery({ queryKey: ['teams'], queryFn: api.teams, enabled: role === 'Admin' })
  const quotas = useQuery({ queryKey: ['quotas'], queryFn: api.quotas, enabled: role === 'Admin' })
  const sources = useQuery({ queryKey: ['compose-sources'], queryFn: api.composeSources })
  const pools = useQuery({ queryKey: ['resource-pools'], queryFn: api.resourcePools, enabled: role === 'Admin' })
  const topology = useQuery({ queryKey: ['topology'], queryFn: api.topology, enabled: role === 'Admin' && tab === 'pools' })
  const audit = useQuery({ queryKey: ['audit'], queryFn: api.auditEvents, enabled: role === 'Admin' || role === 'Auditor' })
  const me = useQuery({ queryKey: ['me'], queryFn: api.me })
  const [userForm, setUserForm] = useState({ username: '', password: '', email: '', role: 'User' as User['role'], teamId: '' })
  const [userCreated, setUserCreated] = useState('')
  const [teamName, setTeamName] = useState('')
  const [sourceForm, setSourceForm] = useState({ name: '', rootPath: '', entryFile: 'compose.yaml' })
  const [quotaForm, setQuotaForm] = useState({ scopeType: 'user' as 'user' | 'team', scopeId: '', gpuLimit: 4, cpuThreadLimit: 32, memoryGB: 256 })
  const [poolName, setPoolName] = useState('')
  const [poolShared, setPoolShared] = useState(false)
  const [poolGPUs, setPoolGPUs] = useState<string[]>([])
  const [poolCPUs, setPoolCPUs] = useState<number[]>([])

  const createUser = useMutation({
    mutationFn: () => {
      const payload: { username: string; password: string; role: User['role']; email?: string; teamId?: string } = {
        username: userForm.username, password: userForm.password, role: userForm.role,
      }
      if (userForm.email.trim()) payload.email = userForm.email.trim()
      if (userForm.teamId) payload.teamId = userForm.teamId
      return api.createUser(payload)
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['users'] })
      setUserCreated(`用户 ${userForm.username} 已创建`)
      setUserForm({ username: '', password: '', email: '', role: 'User', teamId: '' })
    },
    onMutate: () => setUserCreated(''),
  })
  const updateUser = useMutation({
    mutationFn: (value: { id: string; role: User['role']; teamId: string; disabled: boolean; email: string }) =>
      api.updateUser(value.id, { role: value.role, teamId: value.teamId, disabled: value.disabled, email: value.email }),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['users'] }) },
  })
  const resetPassword = useMutation({
    mutationFn: ({ id, newPassword }: { id: string; newPassword: string }) => api.resetUserPassword(id, newPassword),
  })
  const createTeam = useMutation({
    mutationFn: () => api.createTeam(teamName),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['teams'] }); setTeamName('') },
  })
  const createSource = useMutation({
    mutationFn: () => api.createComposeSource(sourceForm),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['compose-sources'] }); setSourceForm({ name: '', rootPath: '', entryFile: 'compose.yaml' }) },
  })
  const updateSource = useMutation({
    mutationFn: (item: ComposeSource) => api.updateComposeSource(item.id, {
      name: item.name, rootPath: item.rootPath, entryFile: item.entryFile, enabled: !item.enabled,
    }),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['compose-sources'] }) },
  })
  const saveQuota = useMutation({
    mutationFn: () => api.upsertQuota({
      id: '',
      scopeType: quotaForm.scopeType,
      scopeId: quotaForm.scopeId,
      gpuLimit: quotaForm.gpuLimit,
      cpuThreadLimit: quotaForm.cpuThreadLimit,
      memoryBytesLimit: quotaForm.memoryGB * 1024 ** 3,
    }),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['quotas'] }) },
  })
  const createPool = useMutation({
    mutationFn: () => api.createResourcePool({
      name: poolName,
      cpuIds: poolCPUs,
      gpuUuids: poolGPUs,
      numaNodes: resourceNuma(topology.data, poolGPUs, poolCPUs),
      shared: poolShared,
    }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['resource-pools'] })
      setPoolName('')
      setPoolGPUs([])
      setPoolCPUs([])
      setPoolShared(false)
    },
  })

  const allowedNuma = selectedGPUNuma(topology.data, poolGPUs)
  const togglePoolGPU = (uuid: string) => {
    setPoolGPUs((current) => {
      const next = current.includes(uuid) ? current.filter((item) => item !== uuid) : [...current, uuid]
      const numa = selectedGPUNuma(topology.data, next)
      setPoolCPUs((cpus) => cpus.filter((id) => {
        const cpu = topology.data?.cpus.find((item) => item.id === id)
        return Boolean(cpu && cpuSelectable(cpu, numa))
      }))
      return next
    })
  }

  if (role !== 'Admin' && role !== 'Auditor') {
    return <section className="placeholder-panel overview-enter"><p className="eyebrow">RESTRICTED</p><h2>需要管理员权限</h2></section>
  }

  return (
    <section className="workload-page overview-enter">
      <div className="section-heading">
        <div><p className="eyebrow">CONTROL PLANE</p><h2>系统管理</h2></div>
        <div className="mode-switch">
          {(['users', 'teams', 'quotas', 'compose', 'pools', 'mig', 'audit'] as const).map((item) => (
            <button key={item} className={tab === item ? 'active' : ''} onClick={() => setTab(item)}>
              {{ users: '用户', teams: '团队', quotas: '配额', compose: 'Compose', pools: '资源池', mig: 'MIG切分', audit: '审计' }[item]}
            </button>
          ))}
        </div>
      </div>
      {tab === 'users' && role === 'Admin' && (
        <div className="admin-grid">
          <form className="admin-form" onSubmit={(event) => { event.preventDefault(); createUser.mutate() }}>
            <label>用户名<input value={userForm.username} onChange={(event) => setUserForm({ ...userForm, username: event.target.value })} /></label>
            <label>
              密码
              <input type="password" value={userForm.password} onChange={(event) => setUserForm({ ...userForm, password: event.target.value })} />
              <small className="field-hint">密码不少于配置下限</small>
            </label>
            <label>
              邮箱
              <input type="email" value={userForm.email} onChange={(event) => setUserForm({ ...userForm, email: event.target.value })} />
              <small className="field-hint">可选。开了 SMTP 时通知发到这个地址</small>
            </label>
            <label>角色<select value={userForm.role} onChange={(event) => setUserForm({ ...userForm, role: event.target.value as User['role'] })}>
              <option>User</option><option>TeamOperator</option><option>Admin</option><option>Viewer</option><option>Auditor</option>
            </select></label>
            <label>团队<select value={userForm.teamId} onChange={(event) => setUserForm({ ...userForm, teamId: event.target.value })}>
              <option value="">无团队</option>
              {(teams.data ?? []).map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
            </select></label>
            <button className="primary-action" type="submit" disabled={createUser.isPending || !userForm.username.trim() || !userForm.password}>
              {createUser.isPending ? '正在创建…' : '创建用户'}
            </button>
            {userCreated && <div className="plan-status ok">{userCreated}</div>}
            {createUser.error && <div className="plan-conflict">{String(createUser.error.message)}</div>}
            {updateUser.error && <div className="plan-conflict">{String(updateUser.error.message)}</div>}
            {resetPassword.error && <div className="plan-conflict">{String(resetPassword.error.message)}</div>}
          </form>
          <div className="full-job-table">
            <div className="full-job-row header"><span>用户</span><span>邮箱</span><span>角色</span><span>团队</span><span>操作</span></div>
            {(users.data ?? []).map((user) => (
              <UserRow
                key={user.id}
                user={user}
                teams={teams.data ?? []}
                self={me.data?.id === user.id}
                onUpdate={(value) => updateUser.mutate(value)}
                onReset={(newPassword) => resetPassword.mutateAsync({ id: user.id, newPassword })}
              />
            ))}
          </div>
        </div>
      )}
      {tab === 'teams' && role === 'Admin' && (
        <div className="admin-grid">
          <form className="admin-form" onSubmit={(event) => { event.preventDefault(); createTeam.mutate() }}>
            <label>团队名称<input value={teamName} onChange={(event) => setTeamName(event.target.value)} /></label>
            <button className="primary-action" type="submit" disabled={!teamName || createTeam.isPending}>创建团队</button>
          </form>
          <div className="full-job-table">{(teams.data ?? []).map((team) => <div className="full-job-row" key={team.id}><span>{team.name}</span><span className="muted">{team.id.slice(0, 8)}</span></div>)}</div>
        </div>
      )}
      {tab === 'quotas' && role === 'Admin' && (
        <div className="admin-grid">
          <form className="admin-form" onSubmit={(event) => { event.preventDefault(); saveQuota.mutate() }}>
            <label>范围<select value={quotaForm.scopeType} onChange={(event) => setQuotaForm({ ...quotaForm, scopeType: event.target.value as 'user' | 'team', scopeId: '' })}><option value="user">用户</option><option value="team">团队</option></select></label>
            <label>{quotaForm.scopeType === 'user' ? '用户' : '团队'}<select value={quotaForm.scopeId} onChange={(event) => setQuotaForm({ ...quotaForm, scopeId: event.target.value })}>
              <option value="">选择{quotaForm.scopeType === 'user' ? '用户' : '团队'}</option>
              {quotaForm.scopeType === 'user'
                ? (users.data ?? []).map((user) => <option key={user.id} value={user.id}>{user.username}</option>)
                : (teams.data ?? []).map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
            </select></label>
            <label>GPU 上限<input type="number" min={0} value={quotaForm.gpuLimit} onChange={(event) => setQuotaForm({ ...quotaForm, gpuLimit: Number(event.target.value) })} /></label>
            <label>CPU 线程上限<input type="number" min={0} value={quotaForm.cpuThreadLimit} onChange={(event) => setQuotaForm({ ...quotaForm, cpuThreadLimit: Number(event.target.value) })} /></label>
            <label>内存上限（GB）<input type="number" min={1} value={quotaForm.memoryGB} onChange={(event) => setQuotaForm({ ...quotaForm, memoryGB: Number(event.target.value) })} /></label>
            <button className="primary-action" type="submit" disabled={!quotaForm.scopeId || saveQuota.isPending}>{saveQuota.isPending ? '正在保存…' : '保存配额'}</button>
            {saveQuota.error && <div className="plan-conflict">{String(saveQuota.error.message)}</div>}
          </form>
          <div className="full-job-table">
            <div className="full-job-row header"><span>对象</span><span>范围</span><span>GPU</span><span>CPU</span><span>内存</span></div>
            {(quotas.data ?? []).map((item) => {
              const label = item.scopeType === 'user'
                ? (users.data ?? []).find((user) => user.id === item.scopeId)?.username
                : (teams.data ?? []).find((team) => team.id === item.scopeId)?.name
              return (
                <div className="full-job-row" key={item.id} style={{ cursor: 'pointer' }} onClick={() => setQuotaForm({
                  scopeType: item.scopeType, scopeId: item.scopeId, gpuLimit: item.gpuLimit, cpuThreadLimit: item.cpuThreadLimit,
                  memoryGB: Math.max(1, Math.round(item.memoryBytesLimit / 1024 ** 3)),
                })}>
                  <span>{label || item.scopeId.slice(0, 8)}</span>
                  <span>{item.scopeType === 'user' ? '用户' : '团队'}</span>
                  <span>{item.gpuLimit} GPU</span>
                  <span>{item.cpuThreadLimit} CPU</span>
                  <span>{(item.memoryBytesLimit / 1024 ** 3).toFixed(0)} GB</span>
                </div>
              )
            })}
          </div>
        </div>
      )}
      {tab === 'compose' && role === 'Admin' && (
        <div className="admin-grid">
          <form className="admin-form" onSubmit={(event) => { event.preventDefault(); createSource.mutate() }}>
            <label>名称<input value={sourceForm.name} onChange={(event) => setSourceForm({ ...sourceForm, name: event.target.value })} /></label>
            <label>根路径<input value={sourceForm.rootPath} onChange={(event) => setSourceForm({ ...sourceForm, rootPath: event.target.value })} /></label>
            <label>入口文件<input value={sourceForm.entryFile} onChange={(event) => setSourceForm({ ...sourceForm, entryFile: event.target.value })} /></label>
            <button className="primary-action" type="submit" disabled={!sourceForm.name || !sourceForm.rootPath}>登记来源</button>
          </form>
          <div>
            {updateSource.error && <div className="plan-conflict">{String(updateSource.error.message)}</div>}
            <div className="full-job-table">{(sources.data ?? []).map((item) => (
              <div className="full-job-row" key={item.id}>
                <span>{item.name}</span>
                <span className="muted">{item.rootPath}</span>
                <span>{item.enabled ? '启用' : '停用'}</span>
                <button
                  type="button"
                  className="text-button"
                  disabled={updateSource.isPending}
                  onClick={() => updateSource.mutate(item)}
                >
                  {item.enabled ? '停用' : '启用'}
                </button>
              </div>
            ))}</div>
          </div>
        </div>
      )}
      {tab === 'pools' && role === 'Admin' && (
        <div className="admin-grid">
          <form className="admin-form" onSubmit={(event) => { event.preventDefault(); createPool.mutate() }}>
            <label>名称<input value={poolName} onChange={(event) => setPoolName(event.target.value)} /></label>
            <label className="checkbox-row"><input type="checkbox" checked={poolShared} onChange={(event) => setPoolShared(event.target.checked)} />共享池</label>
            <div className="resource-choice-grid">{(topology.data?.gpus ?? []).filter((gpu) => !gpu.parentUuid && !gpu.migEnabled).map((gpu) => (
              <button type="button" key={gpu.uuid} className={poolGPUs.includes(gpu.uuid) ? 'resource-choice selected' : 'resource-choice'} onClick={() => togglePoolGPU(gpu.uuid)}>
                <span>GPU {gpu.index}</span><small>NUMA {gpu.numaNode}</small>
              </button>
            ))}</div>
            <div className="thread-choice-grid">{(topology.data?.cpus ?? []).map((cpu) => {
              const allowed = cpuSelectable(cpu, allowedNuma)
              return <button type="button" disabled={!allowed} key={cpu.id} className={poolCPUs.includes(cpu.id) ? 'thread-choice selected' : 'thread-choice'} onClick={() => setPoolCPUs((current) => current.includes(cpu.id) ? current.filter((id) => id !== cpu.id) : [...current, cpu.id])}>{cpu.id}<small>N{cpu.numaNode}</small></button>
            })}</div>
            <button className="primary-action" type="submit" disabled={!poolName || (!poolCPUs.length && !poolGPUs.length) || createPool.isPending}>创建资源池</button>
            {createPool.error && <div className="plan-conflict">{String(createPool.error.message)}</div>}
          </form>
          <div className="full-job-table">
            {(pools.data ?? []).map((pool) => (
              <div className="full-job-row" key={pool.id}>
                <span>{pool.name}</span>
                <span>{pool.gpuUuids.length} GPU</span>
                <span>{pool.cpuIds.length} CPU</span>
                <span className="muted">{pool.shared ? '共享' : '独占'} · NUMA {pool.numaNodes.join(',') || '—'}</span>
              </div>
            ))}
          </div>
        </div>
      )}
      {tab === 'mig' && role === 'Admin' && <MigSettings />}
      {tab === 'audit' && (
        <div className="full-job-table">
          {(audit.data ?? []).map((item) => (
            <div className="full-job-row" key={item.id}>
              <span>{item.action}</span><span>{item.targetType}</span><span className="muted">{item.targetId.slice(0, 8)}</span>
              <span className="muted">{new Date(item.createdAt).toLocaleString('zh-CN')}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

function UserRow({
  user, teams, self, onUpdate, onReset,
}: {
  user: User
  teams: Team[]
  self: boolean
  onUpdate: (value: { id: string; role: User['role']; teamId: string; disabled: boolean; email: string }) => void
  onReset: (newPassword: string) => Promise<unknown>
}) {
  const [email, setEmail] = useState(user.email ?? '')
  const [resetOpen, setResetOpen] = useState(false)
  const [newPassword, setNewPassword] = useState('')
  const [saving, setSaving] = useState(false)
  useEffect(() => { setEmail(user.email ?? '') }, [user.email])

  const patch = (extra: { role?: User['role']; teamId?: string; disabled?: boolean; email?: string }) => {
    onUpdate({
      id: user.id,
      role: extra.role ?? user.role,
      teamId: extra.teamId ?? user.teamId ?? '',
      disabled: extra.disabled ?? Boolean(user.disabled),
      email: extra.email ?? email.trim(),
    })
  }

  return (
    <div className="full-job-row">
      <span>{user.username}{user.disabled ? ' · 已停用' : ''}</span>
      <input
        className="host-priority"
        type="email"
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        onBlur={() => {
          const next = email.trim()
          if (next !== (user.email ?? '')) patch({ email: next })
        }}
      />
      <select
        className="host-priority"
        value={user.role}
        onChange={(event) => patch({ role: event.target.value as User['role'] })}
      >
        <option>User</option><option>TeamOperator</option><option>Admin</option><option>Viewer</option><option>Auditor</option>
      </select>
      <select
        className="host-priority"
        value={user.teamId || ''}
        onChange={(event) => patch({ teamId: event.target.value })}
      >
        <option value="">无团队</option>
        {teams.map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
      </select>
      <div className="user-actions">
        <button className="text-button" disabled={self} onClick={() => patch({ disabled: !user.disabled })}>
          {user.disabled ? '启用' : '停用'}
        </button>
        <button className="text-button" type="button" onClick={() => { setResetOpen((value) => !value); setNewPassword('') }}>
          重置密码
        </button>
        {resetOpen && (
          <>
            <input
              className="host-priority"
              type="password"
              autoComplete="new-password"
              placeholder="新密码"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
            <button
              className="text-button"
              type="button"
              disabled={saving || !newPassword}
              onClick={() => {
                setSaving(true)
                void onReset(newPassword).then(() => { setResetOpen(false); setNewPassword('') }).finally(() => setSaving(false))
              }}
            >
              {saving ? '正在保存…' : '确认'}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
