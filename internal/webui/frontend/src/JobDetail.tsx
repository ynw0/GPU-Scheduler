import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Job } from './types'

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    RUNNING: '运行中', QUEUED: '排队中', RESERVED: '已预约', FAILED: '失败',
    SUCCEEDED: '已完成', CREATING: '创建中', STARTING: '启动中', STOPPING: '停止中',
    CANCELED: '已取消', DRIFTED: '漂移', ORPHANED: '孤儿', DEGRADED: '降级',
  }
  return labels[status] ?? status
}

export function JobDetail({
  job,
  onClose,
  onAction,
}: {
  job: Job
  onClose: () => void
  onAction: (name: 'start' | 'stop' | 'cancel' | 'retry') => void
}) {
  const [logText, setLogText] = useState('')
  const [logError, setLogError] = useState('')
  const metrics = useQuery({
    queryKey: ['job-metrics', job.id],
    queryFn: () => api.jobMetrics(job.id),
    refetchInterval: 5000,
  })
  const latest = metrics.data?.[0]

  useEffect(() => {
    setLogText('')
    setLogError('')
    const source = new EventSource(`/api/v1/jobs/${job.id}/logs/stream`, { withCredentials: true })
    source.onmessage = (event) => {
      setLogText((current) => {
        const next = current ? `${current}\n${event.data}` : event.data
        return next.length > 200_000 ? next.slice(-200_000) : next
      })
    }
    source.onerror = () => {
      setLogError((current) => current || '日志流不可用')
      source.close()
    }
    return () => source.close()
  }, [job.id])

  const cpuValues = Object.values(latest?.cpuPercentById ?? {})
  const cpuAvg = cpuValues.length ? cpuValues.reduce((sum, value) => sum + value, 0) / cpuValues.length : 0

  return (
    <div className="dialog-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section className="job-dialog job-detail-dialog">
        <header className="dialog-header">
          <div><p className="eyebrow">JOB DETAIL</p><h2>{job.name}</h2></div>
          <button className="icon-button" onClick={onClose}>×</button>
        </header>
        <div className="dialog-body job-detail-body">
          <div className="form-column">
            <p>状态 {statusLabel(job.state)} · 优先级 {job.priority}</p>
            {job.heartbeat && <p>训练阶段 {job.heartbeat.phase}</p>}
            {(job.allocations ?? []).map((item) => (
              <div className="plan-line" key={item.id}><span>{item.resourceType}</span><strong>{item.resourceId}</strong></div>
            ))}
            <div className="job-metric-grid">
              <div className="plan-line"><span>CPU</span><strong>{cpuValues.length ? `${cpuAvg.toFixed(1)}%` : '—'}</strong></div>
              {(latest?.gpus ?? []).map((gpu) => (
                <div className="plan-line" key={gpu.uuid}>
                  <span>GPU</span>
                  <strong>{gpu.utilizationPercent}% · {(gpu.memoryUsedBytes / 1024 ** 3).toFixed(1)} GB</strong>
                </div>
              ))}
            </div>
            <div className="dialog-actions">
              {job.state === 'QUEUED' && <button className="secondary-action" onClick={() => onAction('start')}>立即调度</button>}
              {job.state === 'RUNNING' && <button className="secondary-action" onClick={() => onAction('stop')}>停止</button>}
              {['QUEUED', 'RESERVED', 'RUNNING', 'CREATING', 'STARTING'].includes(job.state) && <button className="secondary-action" onClick={() => onAction('cancel')}>取消</button>}
              {job.state === 'FAILED' && <button className="primary-action" onClick={() => onAction('retry')}>重试</button>}
            </div>
          </div>
          <aside className="plan-column">
            <p className="eyebrow">RUNTIME LOG</p>
            <h3>容器日志</h3>
            <pre className="job-log">{logText || logError || '等待日志…'}</pre>
          </aside>
        </div>
      </section>
    </div>
  )
}
