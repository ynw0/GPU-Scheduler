import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Box } from 'lucide-react'
import { api } from './api'
import type { HostContainer, User } from './types'

function formatBytes(value = 0): string {
  if (!value) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(1)} GB`
}

function formatSilent(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${(seconds / 3600).toFixed(1)}h`
}

function cpuLabel(item: HostContainer): string {
  if (item.cpuUnbounded) return '未限制'
  if (!item.cpuIds.length) return '—'
  return `${item.cpuIds.length} 线程`
}

function gpuLabel(item: HostContainer): string {
  if (item.gpuUnbounded) return '全部 GPU'
  if (!item.gpuUuids.length) return '无 GPU'
  return `${item.gpuUuids.length} 张卡`
}

function occupancyLabel(item: HostContainer): string {
  const parts = [item.training ? '训练中' : '空闲']
  if (item.dockerHealth && item.dockerHealth !== 'none') parts.push(item.dockerHealth)
  if (item.cpuActivity) parts.push('CPU')
  if (item.gpuActivity) parts.push('GPU')
  if (item.oomUnder) parts.push('OOM')
  if (item.memoryUsedBytes) parts.push(formatBytes(item.memoryUsedBytes))
  if (item.gpus?.length) parts.push(`GPU ${item.gpus.map((gpu) => `${gpu.utilizationPercent}%`).join('/')}`)
  return parts.join(' · ')
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

export function HostContainersPage({ role }: { role: User['role'] }) {
  const queryClient = useQueryClient()
  const items = useQuery({ queryKey: ['host-containers'], queryFn: api.hostContainers, refetchInterval: 10_000 })
  const patch = useMutation({
    mutationFn: ({ id, priority }: { id: string; priority: HostContainer['priority'] }) => api.patchHostContainer(id, priority),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['host-containers'] }) },
  })
  const stop = useMutation({
    mutationFn: (id: string) => api.stopHostContainer(id),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['host-containers'] }) },
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
            <span>容器</span><span>CPU 设置</span><span>GPU 设置</span><span>当前占用</span><span>任务</span><span>状态</span>
          </div>
          {internal.map((item) => (
            <div className="host-row internal" key={item.containerId}>
              <ContainerName item={item} />
              <span>{cpuLabel(item)}</span>
              <span>{gpuLabel(item)}</span>
              <span>{occupancyLabel(item)}</span>
              <span>{item.jobName || item.managedJobId?.slice(0, 8)}</span>
              <span>{item.state}</span>
            </div>
          ))}
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
            <span>容器</span><span>CPU 设置</span><span>GPU 设置</span><span>当前占用</span><span>优先级</span><span>静默</span><span>操作</span>
          </div>
          {external.map((item) => (
            <div className="host-row" key={item.containerId}>
              <ContainerName item={item} />
              <span>{cpuLabel(item)}</span>
              <span>{gpuLabel(item)}</span>
              <span>{occupancyLabel(item)}</span>
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
              <span>
                {admin && (item.state === 'running' || item.state === 'paused') && (
                  <button className="text-button" onClick={() => stop.mutate(item.containerId)}>停止</button>
                )}
              </span>
            </div>
          ))}
          {!loadError && !external.length && <div className="empty-row compact"><Box size={22} /><span>没有外部容器</span></div>}
        </div>
      </div>
    </section>
  )
}
