import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Bell,
  Box,
  Boxes,
  ChevronRight,
  CircleGauge,
  Clock3,
  Cpu,
  Database,
  Gauge,
  LayoutGrid,
  ListOrdered,
  Plus,
  RefreshCw,
  Settings,
  ShieldCheck,
  TerminalSquare,
  Users,
  Zap,
} from 'lucide-react'
import { api } from './api'
import type { GPU, Job, LogicalCPU, NUMANode, Topology } from './types'

type Section = 'overview' | 'jobs' | 'queue' | 'templates' | 'admin'

const navItems: Array<{ id: Section; label: string; icon: typeof LayoutGrid }> = [
  { id: 'overview', label: '资源总览', icon: LayoutGrid },
  { id: 'jobs', label: '训练任务', icon: TerminalSquare },
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
    RUNNING: '运行中', QUEUED: '排队中', RESERVED: '已预约', FAILED: '失败',
    SUCCEEDED: '已完成', CREATING: '创建中', STARTING: '启动中', STOPPING: '停止中',
  }
  return labels[status] ?? status
}

export function App() {
  const [section, setSection] = useState<Section>('overview')
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, retry: false, refetchInterval: false })
  const authenticated = Boolean(me.data)
  const health = useQuery({ queryKey: ['health'], queryFn: api.health })
  const topology = useQuery({ queryKey: ['topology'], queryFn: api.topology, enabled: authenticated })
  const availability = useQuery({ queryKey: ['availability'], queryFn: api.availability, enabled: authenticated })
  const jobs = useQuery({ queryKey: ['jobs'], queryFn: api.jobs, enabled: authenticated })

  if (me.isLoading) return <BootScreen />
  if (!me.data) return <LoginScreen />

  const running = (jobs.data ?? []).filter((job) => job.state === 'RUNNING')
  const queued = (jobs.data ?? []).filter((job) => job.state === 'QUEUED')
  const isOffline = health.data?.agent === 'offline' || topology.isError

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
          <button className="rail-item" title="通知"><Bell size={19} /><span>通知</span></button>
          <button className="rail-item" title="设置"><Settings size={19} /><span>设置</span></button>
          <div className="avatar" title={`${me.data.username} · ${me.data.role}`}>{me.data.username.slice(0, 2).toUpperCase()}</div>
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
            <button className="primary-action"><Plus size={17} />新建任务</button>
          </div>
        </header>

        {isOffline && (
          <div className="offline-banner">
            <Zap size={18} />
            <div><strong>Agent 通道不可用</strong><span>资源数据已停止更新，平台不会在状态未知时分配资源。</span></div>
          </div>
        )}

        {section === 'overview' && (
          <Overview
            topology={topology.data}
            freeCPUIds={new Set(availability.data?.cpuIds ?? [])}
            freeGPUUUIDs={new Set(availability.data?.gpuUuids ?? [])}
            jobs={jobs.data ?? []}
            runningCount={running.length}
            queueCount={queued.length}
          />
        )}
        {section !== 'overview' && <SectionPlaceholder section={section} jobs={jobs.data ?? []} />}
      </main>
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
        <div className="login-signals"><span>CGROUP V2</span><span>NVML</span><span>STRICT NUMA</span><span>RECONCILIATION</span></div>
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

function Overview({
  topology,
  freeCPUIds,
  freeGPUUUIDs,
  jobs,
  runningCount,
  queueCount,
}: {
  topology?: Topology
  freeCPUIds: Set<number>
  freeGPUUUIDs: Set<string>
  jobs: Job[]
  runningCount: number
  queueCount: number
}) {
  const totalMemory = topology?.numaNodes.reduce((sum, node) => sum + node.memoryBytes, 0) ?? 0
  const freeGPUs = topology?.gpus.filter((gpu) => freeGPUUUIDs.has(gpu.uuid)).length ?? 0
  const stats = [
    { label: '运行任务', value: runningCount, suffix: 'RUNNING', icon: Activity, tone: 'green' },
    { label: '可用 GPU', value: `${freeGPUs}/${topology?.gpus.length ?? 0}`, suffix: 'WHOLE GPU', icon: Gauge, tone: 'cyan' },
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
              gpus={(topology?.gpus ?? []).filter((gpu) => gpu.numaNode === node.id)}
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
          <button className="text-button">查看全部 <ChevronRight size={15} /></button>
        </div>
        <div className="job-table">
          <div className="job-row header"><span>任务</span><span>状态</span><span>优先级</span><span>更新时间</span></div>
          {jobs.slice(0, 6).map((job) => <JobRow key={job.id} job={job} />)}
          {!jobs.length && <div className="empty-row"><CircleGauge size={21} /><span>当前没有可见任务</span></div>}
        </div>
      </section>
    </div>
  )
}

function NumaCard({ node, cpus, gpus, freeCPUIds, freeGPUUUIDs }: {
  node: NUMANode
  cpus: LogicalCPU[]
  gpus: GPU[]
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
        {gpus.map((gpu) => <GpuCard key={gpu.uuid} gpu={gpu} free={freeGPUUUIDs.has(gpu.uuid)} />)}
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

function GpuCard({ gpu, free }: { gpu: GPU; free: boolean }) {
  const memoryPercent = gpu.memoryBytes ? ((gpu.memoryUsedBytes ?? 0) / gpu.memoryBytes) * 100 : 0
  return (
    <div className={free ? 'gpu-card free' : 'gpu-card allocated'}>
      <div className="gpu-card-top"><strong>GPU {gpu.index}</strong><span>{free ? 'FREE' : 'ALLOCATED'}</span></div>
      <p>{gpu.name}</p>
      <div className="gpu-metric"><span>VRAM</span><b>{formatBytes(gpu.memoryUsedBytes)} / {formatBytes(gpu.memoryBytes)}</b></div>
      <div className="meter"><i style={{ width: `${Math.min(memoryPercent, 100)}%` }} /></div>
      <div className="gpu-foot"><span>{gpu.utilization ?? 0}% UTIL</span><span>{gpu.temperatureCelsius ?? '—'}°C</span></div>
    </div>
  )
}

function JobRow({ job }: { job: Job }) {
  return (
    <div className="job-row">
      <span className="job-name"><i />{job.name}</span>
      <span><b className={`status-pill status-${job.state.toLowerCase()}`}>{statusLabel(job.state)}</b></span>
      <span className="priority">{job.priority}</span>
      <span className="muted">{new Date(job.updatedAt).toLocaleString('zh-CN', { hour: '2-digit', minute: '2-digit' })}</span>
    </div>
  )
}

function EmptyTopology() {
  return <div className="empty-topology"><Box size={28} /><strong>等待真实拓扑</strong><span>Agent 上线后，这里将按 NUMA 节点显示 CPU 与 GPU。</span></div>
}

function SectionPlaceholder({ section, jobs }: { section: Section; jobs: Job[] }) {
  const config = {
    jobs: ['训练任务', '集中查看任务生命周期、资源分配与训练阶段。', TerminalSquare],
    queue: ['调度队列', '按优先级、提交时间与防饥饿规则管理等待任务。', ListOrdered],
    templates: ['任务模板', '固化 Image、Compose 与资源请求，减少重复配置。', Boxes],
    admin: ['系统管理', '管理用户、团队、配额、资源池、Compose 来源和审计。', Users],
    overview: ['', '', LayoutGrid],
  }[section] as [string, string, typeof LayoutGrid]
  const Icon = config[2]
  return (
    <section className="placeholder-panel overview-enter">
      <div className="placeholder-icon"><Icon size={32} /></div>
      <p className="eyebrow">MODULE READY</p>
      <h2>{config[0]}</h2>
      <p>{config[1]}</p>
      {section === 'jobs' && <span className="module-count">{jobs.length} 个可见任务</span>}
    </section>
  )
}
