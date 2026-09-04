import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Bell,
  Box,
  Boxes,
  ChevronRight,
  CircleGauge,
  Clock3,
  Container,
  Cpu,
  Database,
  Gauge,
  LayoutGrid,
  ListOrdered,
  Plus,
  RefreshCw,
  ShieldCheck,
  TerminalSquare,
  Zap,
} from 'lucide-react'
import { api } from './api'
import { AdminPage } from './AdminPage'
import { HostContainersPage } from './HostContainers'
import { JobDetail } from './JobDetail'
import { NewJobDialog } from './JobDialog'
import { AccountMenu } from './AccountMenu'
import type { GPU, Job, JobAction, JobSpec, LogicalCPU, NUMANode, Topology } from './types'

type Section = 'overview' | 'jobs' | 'host' | 'queue' | 'templates' | 'admin'

const navItems: Array<{ id: Section; label: string; icon: typeof LayoutGrid }> = [
  { id: 'overview', label: '资源总览', icon: LayoutGrid },
  { id: 'jobs', label: '训练任务', icon: TerminalSquare },
  { id: 'host', label: '容器列表', icon: Container },
  { id: 'queue', label: '调度队列', icon: ListOrdered },
  { id: 'templates', label: '任务模板', icon: Boxes },
  { id: 'admin', label: '系统管理', icon: ShieldCheck },
]

function formatBytes(value = 0): string {
  if (!value) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(value >= 1024 ** 4 ? 0 : 1)} GB`
}

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    RUNNING: '运行中', PAUSED: '已暂停', QUEUED: '排队中', RESERVED: '已预约', FAILED: '失败',
    SUCCEEDED: '已完成', CREATING: '创建中', STARTING: '启动中', STOPPING: '停止中',
    CANCELED: '已取消', DRIFTED: '漂移', ORPHANED: '孤儿', DEGRADED: '降级',
  }
  return labels[status] ?? status
}

export function App() {
  const queryClient = useQueryClient()
  const [section, setSection] = useState<Section>('overview')
  const [newJobOpen, setNewJobOpen] = useState(false)
  const [jobDraft, setJobDraft] = useState<JobSpec>()
  const [selectedJobId, setSelectedJobId] = useState<string>()
  const [notifyOpen, setNotifyOpen] = useState(false)
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, retry: false, refetchInterval: false })
  const authenticated = Boolean(me.data)
  const health = useQuery({ queryKey: ['health'], queryFn: api.health })
  const topology = useQuery({ queryKey: ['topology'], queryFn: api.topology, enabled: authenticated, staleTime: 10_000, refetchInterval: 15_000, retry: false })
  const availability = useQuery({ queryKey: ['availability'], queryFn: api.availability, enabled: authenticated })
  const jobs = useQuery({ queryKey: ['jobs'], queryFn: api.jobs, enabled: authenticated })
  const notifications = useQuery({ queryKey: ['notifications'], queryFn: api.notifications, enabled: authenticated })
  const templates = useQuery({ queryKey: ['templates'], queryFn: api.templates, enabled: authenticated && section === 'templates' })
  const selectedJob = useQuery({ queryKey: ['job', selectedJobId], queryFn: () => api.job(selectedJobId!), enabled: Boolean(selectedJobId), refetchInterval: 5000 })

  useEffect(() => {
    if (!authenticated) return
    const source = new EventSource('/api/v1/events/stream', { withCredentials: true })
    const refresh = () => {
      void queryClient.invalidateQueries({ queryKey: ['jobs'] })
      void queryClient.invalidateQueries({ queryKey: ['availability'] })
      void queryClient.invalidateQueries({ queryKey: ['notifications'] })
    }
    source.addEventListener('job', refresh)
    source.addEventListener('metrics', () => { void queryClient.invalidateQueries({ queryKey: ['availability'] }) })
    source.addEventListener('topology', () => {
      void queryClient.invalidateQueries({ queryKey: ['topology'] })
      void queryClient.invalidateQueries({ queryKey: ['availability'] })
      void queryClient.invalidateQueries({ queryKey: ['mig-layouts'] })
    })
    return () => source.close()
  }, [authenticated, queryClient])

  const action = useMutation({
    mutationFn: ({ id, name }: { id: string; name: JobAction }) => api.jobAction(id, name),
    onSuccess: (_job, vars) => {
      void queryClient.invalidateQueries({ queryKey: ['jobs'] })
      if (vars.name === 'stop') {
        if (selectedJobId === vars.id) setSelectedJobId(undefined)
        return
      }
      if (selectedJobId) void queryClient.invalidateQueries({ queryKey: ['job', selectedJobId] })
    },
  })
  const markAllRead = useMutation({
    mutationFn: api.markAllNotificationsRead,
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['notifications'] }) },
  })
  const clearNotify = useMutation({
    mutationFn: api.clearNotifications,
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['notifications'] }) },
  })

  if (me.isLoading) return <BootScreen />
  if (!me.data) return <LoginScreen />

  const occupying = (jobs.data ?? []).filter((job) => job.state === 'RUNNING' || job.state === 'PAUSED')
  const queued = (jobs.data ?? []).filter((job) => job.state === 'QUEUED')
  const unreadCount = (notifications.data ?? []).filter((item) => !item.readAt).length
  const isOffline = health.data?.agent === 'offline'
  const canWrite = me.data.role === 'Admin' || me.data.role === 'TeamOperator' || me.data.role === 'User'

  return (
    <div className="shell">
      <aside className="rail">
        <div className="brand-mark" aria-label="GPU Scheduler">
          <span className="brand-pulse" />
          <span>GS</span>
        </div>
        <nav className="rail-nav" aria-label="主导航">
          {navItems.map((item) => {
            const Icon = item.icon
            return (
              <button
                key={item.id}
                className={section === item.id ? 'rail-item active' : 'rail-item'}
                onClick={() => setSection(item.id)}
                title={item.label}
              >
                <Icon size={19} strokeWidth={1.7} />
                <span>{item.label}</span>
              </button>
            )
          })}
        </nav>
        <div className="rail-bottom">
          <button className={notifyOpen ? 'rail-item active' : 'rail-item'} title="通知" onClick={() => setNotifyOpen((value) => !value)}>
            <Bell size={19} />
            {unreadCount > 0 && <b className="rail-badge">{unreadCount > 99 ? '99+' : unreadCount}</b>}
            <span>通知</span>
          </button>
          <AccountMenu user={me.data} />
        </div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <p className="eyebrow">GPU SCHEDULER / CONTROL PLANE</p>
            <h1>{navItems.find((item) => item.id === section)?.label}</h1>
          </div>
          <div className="topbar-actions">
            <div className={isOffline ? 'agent-state offline' : 'agent-state'}>
              <span className="state-dot" />
              Agent {isOffline ? '离线' : '在线'}
            </div>
            <button className="icon-button" onClick={() => void topology.refetch()} aria-label="刷新">
              <RefreshCw size={17} />
            </button>
            {canWrite && (
              <button className="primary-action" onClick={() => { setJobDraft(undefined); setNewJobOpen(true) }}><Plus size={17} />新建任务</button>
            )}
          </div>
        </header>

        {isOffline && (
          <div className="offline-banner">
            <Zap size={18} />
            <div><strong>Agent 通道不可用</strong><span>资源数据已停止更新，平台不会在状态未知时分配资源。</span></div>
          </div>
        )}
        {!isOffline && (health.data?.warnings ?? []).map((warning) => (
          <div className="offline-banner" key={warning}>
            <Zap size={18} />
            <div><strong>运行能力提示</strong><span>{warning}</span></div>
          </div>
        ))}
        {!isOffline && topology.isError && (
          <div className="offline-banner">
            <Zap size={18} />
            <div><strong>拓扑发现失败</strong><span>{topology.error instanceof Error ? topology.error.message : '无法读取 CPU / GPU / NUMA 拓扑。'}</span></div>
          </div>
        )}
        {!isOffline && availability.isError && (
          <div className="offline-banner">
            <Zap size={18} />
            <div><strong>可用资源读取失败</strong><span>{availability.error instanceof Error ? availability.error.message : '无法计算空闲 CPU / GPU。'}</span></div>
          </div>
        )}

        {section === 'overview' && (
          <Overview
            topology={topology.data}
            freeCPUIds={new Set(availability.data?.cpuIds ?? [])}
            freeGPUUUIDs={new Set(availability.data?.gpuUuids ?? [])}
            jobs={jobs.data ?? []}
            runningCount={occupying.length}
            queueCount={queued.length}
            canWrite={canWrite}
            onShowAll={() => setSection('jobs')}
            onOpenJob={setSelectedJobId}
            onAction={(id, name) => action.mutate({ id, name })}
          />
        )}
        {section !== 'overview' && (
          <SectionContent
            section={section}
            jobs={jobs.data ?? []}
            role={me.data.role}
            templates={templates.data ?? []}
            gpus={topology.data?.gpus ?? []}
            onOpenJob={setSelectedJobId}
            onUseTemplate={(spec) => { setJobDraft(spec); setNewJobOpen(true) }}
            onAction={(id, name) => action.mutate({ id, name })}
            canWrite={canWrite}
          />
        )}
      </main>
      {newJobOpen && (
        <NewJobDialog
          topology={topology.data}
          freeGPUUUIDs={new Set(availability.data?.gpuUuids ?? [])}
          initialSpec={jobDraft}
          onClose={() => { setNewJobOpen(false); setJobDraft(undefined) }}
        />
      )}
      {notifyOpen && (
        <aside className="notify-drawer">
          <header>
            <p className="eyebrow">NOTIFICATIONS</p>
            <h3>站内通知</h3>
            <div className="notify-actions">
              <button className="text-button" disabled={!unreadCount || markAllRead.isPending} onClick={() => markAllRead.mutate()}>一键已读</button>
              <button className="text-button" disabled={!(notifications.data ?? []).length || clearNotify.isPending} onClick={() => clearNotify.mutate()}>清除通知</button>
            </div>
          </header>
          {(notifications.data ?? []).map((item) => (
            <button className={item.readAt ? 'notify-item read' : 'notify-item'} key={item.id} onClick={() => { void api.markNotificationRead(item.id); void queryClient.invalidateQueries({ queryKey: ['notifications'] }) }}>
              <strong>{item.title}</strong><span>{item.body}</span>
            </button>
          ))}
          {!(notifications.data ?? []).length && <div className="empty-row">暂无通知</div>}
        </aside>
      )}
      {selectedJob.data && (
        <JobDetail
          job={selectedJob.data}
          topology={topology.data}
          canWrite={canWrite}
          onClose={() => setSelectedJobId(undefined)}
          onAction={(name) => action.mutate({ id: selectedJob.data!.id, name })}
          onClone={(spec) => {
            setSelectedJobId(undefined)
            setJobDraft(spec)
            setNewJobOpen(true)
          }}
        />
      )}
    </div>
  )
}

function LoginScreen() {
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const login = useMutation({
    mutationFn: () => api.login(username, password),
    onSuccess: (user) => queryClient.setQueryData(['me'], user),
  })
  return (
    <main className="login-shell">
      <section className="login-intro">
        <div className="login-brand"><span className="brand-pulse" />GS / CONTROL PLANE</div>
        <div>
          <p className="eyebrow">SINGLE-NODE AI ORCHESTRATION</p>
          <h1>把拓扑变成<br /><em>可调度的秩序。</em></h1>
          <p>Docker 生命周期、整卡 GPU、CPU 线程与 NUMA 本地性，在同一个控制面中保持一致。</p>
        </div>
        <div className="login-signals"><span>CGROUP V1</span><span>NVML</span><span>STRICT NUMA</span><span>RECONCILIATION</span></div>
      </section>
      <section className="login-panel">
        <form onSubmit={(event) => { event.preventDefault(); login.mutate() }}>
          <p className="eyebrow">AUTHORIZED ACCESS</p>
          <h2>登录调度平台</h2>
          <label>用户名<input autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} /></label>
          <label>密码<input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>
          {login.isError && <div className="login-error">认证失败，请检查用户名和密码。</div>}
          <button className="login-submit" type="submit" disabled={login.isPending || !username || !password}>
            {login.isPending ? '正在认证…' : '进入控制面'}<ChevronRight size={17} />
          </button>
          <small>会话通过 Secure / HttpOnly Cookie 建立，所有写操作受 CSRF 保护。</small>
        </form>
      </section>
    </main>
  )
}

function BootScreen() {
  return <main className="boot-screen"><div className="boot-mark">GS</div><span>INITIALIZING CONTROL PLANE</span></main>
}

function canPauseJob(state: string): boolean {
  return state === 'RUNNING' || state === 'DEGRADED' || state === 'DRIFTED'
}

function canRetryJob(state: string): boolean {
  return state === 'FAILED' || state === 'CANCELED' || state === 'ORPHANED'
}

function JobOps({ job, canWrite, onAction }: { job: Job; canWrite: boolean; onAction: (id: string, name: JobAction) => void }) {
  if (!canWrite) return <span className="job-ops">—</span>
  return (
    <span className="job-ops" onClick={(event) => event.stopPropagation()}>
      {canPauseJob(job.state) && <button className="job-op" type="button" onClick={() => onAction(job.id, 'pause')}>暂停</button>}
      {job.state === 'PAUSED' && <button className="job-op" type="button" onClick={() => onAction(job.id, 'resume')}>继续</button>}
      <button className="job-op" type="button" onClick={() => onAction(job.id, 'stop')}>停止</button>
      {canRetryJob(job.state) && <button className="job-op" type="button" onClick={() => onAction(job.id, 'retry')}>重试</button>}
    </span>
  )
}

function Overview({
  topology,
  freeCPUIds,
  freeGPUUUIDs,
  jobs,
  runningCount,
  queueCount,
  canWrite,
  onShowAll,
  onOpenJob,
  onAction,
}: {
  topology?: Topology
  freeCPUIds: Set<number>
  freeGPUUUIDs: Set<string>
  jobs: Job[]
  runningCount: number
  queueCount: number
  canWrite: boolean
  onShowAll: () => void
  onOpenJob: (id: string) => void
  onAction: (id: string, name: JobAction) => void
}) {
  const totalMemory = topology?.numaNodes.reduce((sum, node) => sum + node.memoryBytes, 0) ?? 0
  const schedulable = topology?.gpus.filter((gpu) => !gpu.migEnabled || gpu.parentUuid) ?? []
  const freeGPUs = schedulable.filter((gpu) => freeGPUUUIDs.has(gpu.uuid)).length
  const stats = [
    { label: '运行任务', value: runningCount, suffix: 'RUNNING', icon: Activity, tone: 'green' },
    { label: '可调度', value: `${freeGPUs}/${schedulable.length}`, suffix: 'GPU / MIG', icon: Gauge, tone: 'cyan' },
    { label: '可用线程', value: `${freeCPUIds.size}/${topology?.cpus.length ?? 0}`, suffix: 'LOGICAL CPU', icon: Cpu, tone: 'amber' },
    { label: '等待队列', value: queueCount, suffix: 'PENDING', icon: Clock3, tone: 'neutral' },
  ]

  return (
    <div className="content-grid overview-enter">
      <section className="stat-strip">
        {stats.map((stat) => {
          const Icon = stat.icon
          return (
            <article className={`stat-cell ${stat.tone}`} key={stat.label}>
              <div className="stat-icon"><Icon size={19} /></div>
              <div><span>{stat.label}</span><strong>{stat.value}</strong><small>{stat.suffix}</small></div>
            </article>
          )
        })}
      </section>

      <section className="topology-panel">
        <div className="section-heading">
          <div><p className="eyebrow">LIVE TOPOLOGY</p><h2>NUMA 资源地图</h2></div>
          <div className="host-meta">
            <span>{topology?.hostname || '等待 Agent'}</span>
            <span>{topology ? new Date(topology.observedAt).toLocaleTimeString('zh-CN') : '—'}</span>
          </div>
        </div>
        <div className="numa-grid">
          {(topology?.numaNodes ?? []).map((node) => (
            <NumaCard
              key={node.id}
              node={node}
              cpus={(topology?.cpus ?? []).filter((cpu) => cpu.numaNode === node.id)}
              gpus={(topology?.gpus ?? []).filter((gpu) => gpu.numaNode === node.id && !gpu.parentUuid)}
              instances={(topology?.gpus ?? []).filter((gpu) => gpu.numaNode === node.id && Boolean(gpu.parentUuid))}
              freeCPUIds={freeCPUIds}
              freeGPUUUIDs={freeGPUUUIDs}
            />
          ))}
          {!topology?.numaNodes.length && <EmptyTopology />}
        </div>
        <footer className="topology-legend">
          <span><i className="legend-dot free" />空闲</span>
          <span><i className="legend-dot allocated" />已分配</span>
          <span><i className="legend-dot hot" />高负载</span>
          <span><i className="legend-dot unavailable" />不可用</span>
          <span className="capacity-note"><Database size={14} />物理内存 {formatBytes(totalMemory)}</span>
        </footer>
      </section>

      <section className="jobs-panel">
        <div className="section-heading compact">
          <div><p className="eyebrow">WORKLOADS</p><h2>当前训练任务</h2></div>
          <button className="text-button" onClick={onShowAll}>查看全部 <ChevronRight size={15} /></button>
        </div>
        <div className="job-table">
          <div className="job-row header"><span>任务</span><span>状态</span><span>优先级</span><span>更新时间</span><span>操作</span></div>
          {jobs.slice(0, 6).map((job) => (
            <JobRow key={job.id} job={job} canWrite={canWrite} onOpen={onOpenJob} onAction={onAction} />
          ))}
          {!jobs.length && <div className="empty-row"><CircleGauge size={21} /><span>当前没有可见任务</span></div>}
        </div>
      </section>
    </div>
  )
}

function NumaCard({ node, cpus, gpus, instances, freeCPUIds, freeGPUUUIDs }: {
  node: NUMANode
  cpus: LogicalCPU[]
  gpus: GPU[]
  instances: GPU[]
  freeCPUIds: Set<number>
  freeGPUUUIDs: Set<string>
}) {
  const cores = useMemo(() => {
    const map = new Map<number, LogicalCPU[]>()
    cpus.forEach((cpu) => map.set(cpu.coreId, [...(map.get(cpu.coreId) ?? []), cpu]))
    return [...map.entries()].sort(([a], [b]) => a - b)
  }, [cpus])

  return (
    <article className="numa-card">
      <header>
        <div><span className="node-index">0{node.id}</span><div><h3>NUMA NODE {node.id}</h3><p>{formatBytes(node.memoryBytes)} LOCAL MEMORY</p></div></div>
        <span className="node-health">HEALTHY</span>
      </header>
      <div className="gpu-lane">
        {gpus.map((gpu) => <GpuCard key={gpu.uuid} gpu={gpu} instances={instances.filter((item) => item.parentUuid === gpu.uuid)} freeGPUUUIDs={freeGPUUUIDs} />)}
      </div>
      <div className="cpu-section">
        <div className="cpu-section-title"><span>PHYSICAL CORE / SMT</span><span>{cores.length}C · {cpus.length}T</span></div>
        <div className="core-grid">
          {cores.map(([coreId, threads]) => (
            <div className="core-pair" key={coreId} title={`Core ${coreId} · CPU ${threads.map((cpu) => cpu.id).join(', ')}`}>
              <small>{coreId}</small>
              {threads.sort((a, b) => a.id - b.id).map((thread) => (
                <i key={thread.id} className={!thread.allocatable ? 'unavailable' : freeCPUIds.has(thread.id) ? 'free' : 'allocated'} />
              ))}
            </div>
          ))}
        </div>
      </div>
    </article>
  )
}

function GpuCard({ gpu, instances, freeGPUUUIDs }: { gpu: GPU; instances: GPU[]; freeGPUUUIDs: Set<string> }) {
  const memoryPercent = gpu.memoryBytes ? ((gpu.memoryUsedBytes ?? 0) / gpu.memoryBytes) * 100 : 0
  const free = !gpu.migEnabled && freeGPUUUIDs.has(gpu.uuid)
  return (
    <div className={free ? 'gpu-card free' : 'gpu-card allocated'}>
      <div className="gpu-card-top"><strong>GPU {gpu.index}</strong><span>{gpu.migEnabled ? 'MIG' : free ? 'FREE' : 'ALLOCATED'}</span></div>
      <p>{gpu.name}</p>
      {gpu.migEnabled && instances.length === 0 && <p className="muted">MIG 已开，尚无实例</p>}
      {instances.length > 0 && (
        <ul className="mig-instances">
          {instances.map((item) => (
            <li key={item.uuid} className={freeGPUUUIDs.has(item.uuid) ? 'free' : 'allocated'}>
              <span>GPU {item.index} · {item.profile || item.uuid.slice(0, 8)}</span>
              <span>{freeGPUUUIDs.has(item.uuid) ? 'FREE' : 'BUSY'}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="gpu-metric"><span>VRAM</span><b>{formatBytes(gpu.memoryUsedBytes)} / {formatBytes(gpu.memoryBytes)}</b></div>
      <div className="meter"><i style={{ width: `${Math.min(memoryPercent, 100)}%` }} /></div>
      <div className="gpu-foot"><span>{gpu.utilization ?? 0}% UTIL</span><span>{gpu.temperatureCelsius ?? '—'}°C</span></div>
    </div>
  )
}

function JobRow({ job, canWrite, onOpen, onAction }: {
  job: Job
  canWrite: boolean
  onOpen: (id: string) => void
  onAction: (id: string, name: JobAction) => void
}) {
  return (
    <div className="job-row" style={{ cursor: 'pointer' }} onClick={() => onOpen(job.id)}>
      <span className="job-name"><i />{job.name}</span>
      <span><b className={`status-pill status-${job.state.toLowerCase()}`}>{statusLabel(job.state)}</b></span>
      <span className="priority">{job.priority}</span>
      <span className="muted">{new Date(job.updatedAt).toLocaleString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</span>
      <JobOps job={job} canWrite={canWrite} onAction={onAction} />
    </div>
  )
}

function EmptyTopology() {
  return <div className="empty-topology"><Box size={28} /><strong>等待真实拓扑</strong><span>Agent 上线后，这里将按 NUMA 节点显示 CPU 与 GPU。</span></div>
}

function SectionContent({
  section, jobs, role, templates, gpus, onOpenJob, onUseTemplate, onAction, canWrite,
}: {
  section: Section
  jobs: Job[]
  role: import('./types').User['role']
  templates: import('./types').Template[]
  gpus: GPU[]
  onOpenJob: (id: string) => void
  onUseTemplate: (spec: JobSpec) => void
  onAction: (id: string, name: JobAction) => void
  canWrite: boolean
}) {
  if (section === 'admin') return <AdminPage role={role} />
  if (section === 'host') return <HostContainersPage role={role} gpus={gpus} canWrite={canWrite} onOpenJob={onOpenJob} onJobAction={onAction} />
  if (section === 'templates') {
    return (
      <section className="workload-page overview-enter">
        <div className="section-heading">
          <div><p className="eyebrow">TEMPLATES</p><h2>任务模板</h2></div>
          <span className="module-count">{templates.length} 项</span>
        </div>
        <div className="full-job-table">
          {templates.map((item) => (
            <div className="full-job-row" key={item.id}>
              <span>{item.name}</span>
              <span>{item.spec.workload.kind}</span>
              <span className="muted">{new Date(item.updatedAt).toLocaleString('zh-CN')}</span>
              <span>
                {canWrite && <button className="text-button" onClick={() => onUseTemplate(item.spec)}>使用</button>}
              </span>
            </div>
          ))}
          {!templates.length && <div className="empty-row"><Boxes size={22} /><span>还没有模板</span></div>}
        </div>
      </section>
    )
  }
  const visible = section === 'queue' ? jobs.filter((job) => job.state === 'QUEUED') : jobs
  return (
    <section className="workload-page overview-enter">
      <div className="section-heading">
        <div><p className="eyebrow">{section === 'queue' ? 'SCHEDULER QUEUE' : 'WORKLOAD REGISTRY'}</p><h2>{section === 'queue' ? '等待调度' : '全部训练任务'}</h2></div>
        <span className="module-count">{visible.length} 项</span>
      </div>
      <div className="full-job-table">
        <div className="full-job-row header"><span>任务</span><span>状态</span><span>优先级</span><span>创建时间</span><span>操作</span></div>
        {visible.map((job) => (
          <div className="full-job-row" key={job.id} style={{ cursor: 'pointer' }} onClick={() => onOpenJob(job.id)}>
            <span className="job-name"><i />{job.name}<small>{job.id.slice(0, 8)}</small></span>
            <span><b className={`status-pill status-${job.state.toLowerCase()}`}>{statusLabel(job.state)}</b></span>
            <span className="priority">{job.priority}</span>
            <span className="muted">{new Date(job.createdAt).toLocaleString('zh-CN')}</span>
            <JobOps job={job} canWrite={canWrite} onAction={onAction} />
          </div>
        ))}
        {!visible.length && <div className="empty-row"><ListOrdered size={22} /><span>{section === 'queue' ? '队列为空' : '当前没有可见任务'}</span></div>}
      </div>
    </section>
  )
}
