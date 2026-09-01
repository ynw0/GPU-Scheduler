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

  return (
    <section className="workload-page overview-enter">
      <div className="section-heading">
        <div><p className="eyebrow">HOST DOCKER</p><h2>宿主机容器</h2></div>
        <span className="module-count">{rows.length} 项</span>
      </div>
      <div className="full-job-table">
        <div className="host-row header">
          <span>容器</span><span>CPU 设置</span><span>GPU 设置</span><span>当前占用</span><span>优先级</span><span>静默</span><span>操作</span>
        </div>
        {rows.map((item) => (
          <div className="host-row" key={item.containerId}>
            <span className="job-name">
              <i />
              {item.name}
              <small>{item.image}</small>
            </span>
            <span>{cpuLabel(item)}</span>
            <span>{gpuLabel(item)}</span>
            <span>
              {item.training ? '训练中' : '空闲'}
              {item.dockerHealth && item.dockerHealth !== 'none' ? ` · ${item.dockerHealth}` : ''}
              {item.cpuActivity ? ' · CPU' : ''}
              {item.gpuActivity ? ' · GPU' : ''}
              {item.oomUnder ? ' · OOM' : ''}
              {item.memoryUsedBytes ? ` · ${formatBytes(item.memoryUsedBytes)}` : ''}
              {item.gpus?.length ? ` · GPU ${item.gpus.map((gpu) => `${gpu.utilizationPercent}%`).join('/')}` : ''}
            </span>
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
        {!rows.length && <div className="empty-row"><Box size={22} /><span>没有外部容器</span></div>}
      </div>
    </section>
  )
}
