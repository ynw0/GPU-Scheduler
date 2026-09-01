import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Box } from 'lucide-react'
import { api } from './api'
import type { GPU, HostContainer, User } from './types'

function formatBytes(value = 0): string {
  if (!value) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}

function formatSilent(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${(seconds / 3600).toFixed(1)}h`
}

function formatThreadIds(ids: number[]): string {
  if (!ids.length) return '未绑定具体线程'
  const sorted = [...ids].sort((a, b) => a - b)
  const parts: string[] = []
  let start = sorted[0]
  let prev = sorted[0]
  for (let i = 1; i <= sorted.length; i++) {
    const current = sorted[i]
    if (current === prev + 1) {
      prev = current
      continue
    }
    parts.push(start === prev ? String(start) : `${start}–${prev}`)
    start = prev = current
  }
  return `线程 ${parts.join(', ')}`
}

function gpuName(uuid: string, gpus: GPU[]): string {
  const gpu = gpus.find((item) => item.uuid === uuid)
  return gpu ? `GPU ${gpu.index}` : uuid.slice(0, 8)
}

function cpuSetting(item: HostContainer): { label: string; detail: string } {
  const ids = item.cpuIds ?? []
  if (item.cpuUnbounded) return { label: '未限制', detail: '未绑定 cpuset，可使用宿主机全部线程' }
  if (!ids.length) return { label: '—', detail: '未绑定具体线程' }
  return { label: `${ids.length} 线程`, detail: formatThreadIds(ids) }
}

function gpuSetting(item: HostContainer, gpus: GPU[]): { label: string; detail: string } {
  const uuids = item.gpuUuids ?? []
  if (item.gpuUnbounded) {
    const lines = gpus.length ? gpus.map((gpu) => `GPU ${gpu.index}  ${gpu.name}`) : ['绑定宿主机全部 GPU']
    return { label: '全部 GPU', detail: lines.join('\n') }
  }
  if (!uuids.length) return { label: '无 GPU', detail: '未绑定 GPU' }
  return {
    label: `${uuids.length} 张卡`,
    detail: uuids.map((uuid) => {
      const gpu = gpus.find((item) => item.uuid === uuid)
      return gpu ? `GPU ${gpu.index}  ${gpu.name}\n${uuid}` : uuid
    }).join('\n\n'),
  }
}

function Tip({ label, detail }: { label: string; detail: string }) {
  return (
    <span className="host-tip">
      {label}
      <span className="host-tip-box">{detail}</span>
    </span>
  )
}

function containerStatus(item: HostContainer): { label: string; kind: string } {
  const state = (item.state || '').toLowerCase()
  if (state === 'paused') return { label: '已暂停', kind: 'paused' }
  if (state === 'exited' || state === 'dead' || state === 'removing') return { label: '已退出', kind: 'exited' }
  if (item.dockerHealth === 'unhealthy' || item.oomUnder) return { label: '不健康', kind: 'unhealthy' }
  if (state === 'running') return item.training ? { label: '训练中', kind: 'training' } : { label: '空闲', kind: 'idle' }
  const labels: Record<string, string> = { created: '已创建', restarting: '重启中' }
  return { label: labels[state] ?? (item.state || '未知'), kind: 'other' }
}

function Status({ item }: { item: HostContainer }) {
  const status = containerStatus(item)
  return <span className={`host-status ${status.kind}`}>{status.label}</span>
}

function Occupancy({ item, gpus }: { item: HostContainer; gpus: GPU[] }) {
  const live = item.state === 'running' || item.state === 'paused'
  const ids = item.cpuIds ?? []
  const memory = item.memoryLimitBytes
    ? `${formatBytes(item.memoryUsedBytes)} / ${formatBytes(item.memoryLimitBytes)}`
    : item.memoryUsedBytes ? formatBytes(item.memoryUsedBytes) : '—'
  const pct = `${(item.cpuPercent ?? 0).toFixed(0)}%`
  const cpu = item.cpuUnbounded
    ? (live ? `未限制 · ${pct}` : '未限制')
    : `${ids.length} 线程${live ? ` · ${pct}` : ''}`
  const used = item.gpus ?? []

  return (
    <div className="host-use">
      <div><span>内存</span><strong>{live ? memory : '—'}</strong></div>
      <div><span>CPU</span><strong>{live ? cpu : '—'}</strong></div>
      <div>
        <span>GPU</span>
        <div>
          {!used.length && <strong>未占用</strong>}
          {used.map((gpu) => (
            <strong key={gpu.uuid}>
              {gpuName(gpu.uuid, gpus)}
              {'  '}
              {formatBytes(gpu.memoryUsedBytes)}/{formatBytes(gpu.memoryTotalBytes)}
              {' · '}
              {gpu.utilizationPercent}%
            </strong>
          ))}
        </div>
      </div>
      {item.oomUnder && <div className="host-use-warn">OOM</div>}
    </div>
  )
}

function ContainerName({ item }: { item: HostContainer }) {
  return (
    <span className="job-name">
      <i />
      {item.name}
      <small>{item.image}</small>
    </span>
  )
}

export function HostContainersPage({ role, gpus = [], onOpenJob }: { role: User['role']; gpus?: GPU[]; onOpenJob: (id: string) => void }) {
  const queryClient = useQueryClient()
  const items = useQuery({ queryKey: ['host-containers'], queryFn: api.hostContainers, refetchInterval: 10_000 })
  const refresh = () => { void queryClient.invalidateQueries({ queryKey: ['host-containers'] }) }
  const patch = useMutation({
    mutationFn: ({ id, priority }: { id: string; priority: HostContainer['priority'] }) => api.patchHostContainer(id, priority),
    onSuccess: refresh,
  })
  const stop = useMutation({
    mutationFn: (id: string) => api.stopHostContainer(id),
    onSuccess: refresh,
  })
  const pause = useMutation({
    mutationFn: (id: string) => api.pauseHostContainer(id),
    onSuccess: refresh,
  })
  const unpause = useMutation({
    mutationFn: (id: string) => api.unpauseHostContainer(id),
    onSuccess: refresh,
  })
  const admin = role === 'Admin'
  const rows = items.data ?? []
  const internal = rows.filter((item) => item.managedJobId)
  const external = rows.filter((item) => !item.managedJobId)
  const loadError = items.error instanceof Error ? items.error.message : items.isError ? '无法读取容器' : ''

  return (
    <section className="workload-page overview-enter">
      <div className="section-heading">
        <div><p className="eyebrow">CONTAINERS</p><h2>容器列表</h2></div>
        <span className="module-count">{rows.length} 项</span>
      </div>
      {loadError && <div className="empty-row compact"><Box size={22} /><span>{loadError}</span></div>}
      <div className="host-group">
        <div className="host-group-heading">
          <h3>内部容器</h3>
          <span>{internal.length} 项</span>
        </div>
        <div className="full-job-table">
          <div className="host-row internal header">
            <span>容器</span><span>CPU 设置</span><span>GPU 设置</span><span>当前占用</span><span>状态</span><span>任务</span>
          </div>
          {internal.map((item) => {
            const cpu = cpuSetting(item)
            const gpu = gpuSetting(item, gpus)
            return (
              <div className="host-row internal" key={item.containerId}>
                <ContainerName item={item} />
                <Tip label={cpu.label} detail={cpu.detail} />
                <Tip label={gpu.label} detail={gpu.detail} />
                <Occupancy item={item} gpus={gpus} />
                <Status item={item} />
                {item.managedJobId ? (
                  <button className="text-button" onClick={() => onOpenJob(item.managedJobId!)}>{item.jobName || item.managedJobId.slice(0, 8)}</button>
                ) : <span>—</span>}
              </div>
            )
          })}
          {!loadError && !internal.length && <div className="empty-row compact"><Box size={22} /><span>没有内部容器</span></div>}
        </div>
      </div>
      <div className="host-group">
        <div className="host-group-heading">
          <h3>外部容器</h3>
          <span>{external.length} 项</span>
        </div>
        <div className="full-job-table">
          <div className="host-row header">
            <span>容器</span><span>CPU 设置</span><span>GPU 设置</span><span>当前占用</span><span>状态</span><span>优先级</span><span>静默</span><span>操作</span>
          </div>
          {external.map((item) => {
            const cpu = cpuSetting(item)
            const gpu = gpuSetting(item, gpus)
            return (
              <div className="host-row" key={item.containerId}>
                <ContainerName item={item} />
                <Tip label={cpu.label} detail={cpu.detail} />
                <Tip label={gpu.label} detail={gpu.detail} />
                <Occupancy item={item} gpus={gpus} />
                <Status item={item} />
                <span>
                  {admin ? (
                    <select
                      className="host-priority"
                      value={item.priority}
                      onChange={(event) => patch.mutate({ id: item.containerId, priority: event.target.value as HostContainer['priority'] })}
                    >
                      <option>P0</option><option>P1</option><option>P2</option><option>P3</option>
                    </select>
                  ) : item.priority}
                  {item.priorityLocked ? ' 锁' : ''}
                </span>
                <span className="muted">{formatSilent(item.silentSeconds)}</span>
                <span className="host-actions">
                  {admin && item.state === 'running' && (
                    <button className="text-button" onClick={() => pause.mutate(item.containerId)}>暂停</button>
                  )}
                  {admin && item.state === 'paused' && (
                    <button className="text-button" onClick={() => unpause.mutate(item.containerId)}>继续</button>
                  )}
                  {admin && (item.state === 'running' || item.state === 'paused') && (
                    <button className="text-button" onClick={() => stop.mutate(item.containerId)}>停止</button>
                  )}
                </span>
              </div>
            )
          })}
          {!loadError && !external.length && <div className="empty-row compact"><Box size={22} /><span>没有外部容器</span></div>}
        </div>
      </div>
    </section>
  )
}
