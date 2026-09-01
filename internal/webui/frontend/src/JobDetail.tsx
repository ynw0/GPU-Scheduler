import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Job, JobSpec, ResourceRequest } from './types'

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    RUNNING: '运行中', QUEUED: '排队中', RESERVED: '已预约', FAILED: '失败',
    SUCCEEDED: '已完成', CREATING: '创建中', STARTING: '启动中', STOPPING: '停止中',
    CANCELED: '已取消', DRIFTED: '漂移', ORPHANED: '孤儿', DEGRADED: '降级',
  }
  return labels[status] ?? status
}

function formatBytes(value = 0): string {
  if (!value) return '0 B'
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`
  return `${(value / 1024).toFixed(1)} KB`
}

function specResources(spec: JobSpec): ResourceRequest {
  if (spec.workload.kind === 'compose') {
    return spec.workload.services.find((item) => item.class === 'training')?.resources ?? spec.resources
  }
  return spec.resources
}

function gpuLabel(resources: ResourceRequest): string {
  if (resources.gpuMode === 'manual') return `${(resources.gpuUuids ?? []).length} 张整卡`
  return `${resources.gpuCount} 张（自动）`
}

function cpuLabel(resources: ResourceRequest): string {
  if (resources.cpuMode === 'manual-threads') return `${(resources.cpuIds ?? []).length} 线程`
  return `${resources.cpuCount} 物理核`
}

export function JobDetail({
  job,
  canWrite,
  onClose,
  onAction,
  onClone,
}: {
  job: Job
  canWrite: boolean
  onClose: () => void
  onAction: (name: 'start' | 'stop' | 'cancel' | 'retry') => void
  onClone: (spec: JobSpec) => void
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
            {job.heartbeat && (
              <p>
                训练心跳 {job.heartbeat.phase}
                {job.heartbeat.epoch != null ? ` · epoch ${job.heartbeat.epoch}` : ''}
                {job.heartbeat.step != null ? ` · step ${job.heartbeat.step}` : ''}
                {` · ${job.heartbeat.ageSeconds}s 前`}
              </p>
            )}
            {job.runtime && (
              <p>
                运行探测 {job.runtime.dockerHealth || 'none'}
                {job.runtime.cpuActivity ? ' · CPU 活动' : ' · CPU 静'}
                {job.runtime.gpuActivity ? ' · GPU 活动' : ' · GPU 静'}
                {job.runtime.oomUnder ? ' · OOM' : ''}
              </p>
            )}
            {(job.allocations ?? []).map((item) => (
              <div className="plan-line" key={item.id}><span>{item.resourceType}</span><strong>{item.resourceId}</strong></div>
            ))}
            {Object.entries(job.attempt?.containerIds ?? {}).map(([service, id]) => (
              <div className="plan-line" key={service}><span>容器 {service}</span><strong>{id}</strong></div>
            ))}
            {job.spec && <JobSpecView spec={job.spec} />}
            <div className="job-metric-grid">
              <div className="plan-line"><span>CPU</span><strong>{cpuValues.length ? `${cpuAvg.toFixed(1)}%` : '—'}</strong></div>
              <div className="plan-line"><span>内存</span><strong>{latest ? `${formatBytes(latest.memoryUsedBytes)} / ${formatBytes(latest.memoryLimitBytes)}` : '—'}</strong></div>
              <div className="plan-line"><span>块设备读取</span><strong>{latest ? formatBytes(latest.blockIoReadBytes) : '—'}</strong></div>
              <div className="plan-line"><span>块设备写入</span><strong>{latest ? formatBytes(latest.blockIoWriteBytes) : '—'}</strong></div>
              {latest?.oomUnder && <div className="plan-conflict">容器当前处于 OOM 状态</div>}
              {latest && latest.memoryFailCount > 0 && <div className="plan-conflict">内存分配失败计数 {latest.memoryFailCount}</div>}
              {(latest?.gpus ?? []).map((gpu) => (
                <div className="plan-line" key={gpu.uuid}>
                  <span>GPU</span>
                  <strong>{gpu.utilizationPercent}% · {(gpu.memoryUsedBytes / 1024 ** 3).toFixed(1)} GB</strong>
                </div>
              ))}
            </div>
            {canWrite && (
              <div className="dialog-actions">
                {job.spec && <button className="secondary-action" onClick={() => onClone(job.spec!)}>再开一次</button>}
                {job.state === 'QUEUED' && <button className="secondary-action" onClick={() => onAction('start')}>立即调度</button>}
                {(job.state === 'RUNNING' || job.state === 'DEGRADED' || job.state === 'DRIFTED') && <button className="secondary-action" onClick={() => onAction('stop')}>停止</button>}
                {['QUEUED', 'RESERVED', 'RUNNING', 'CREATING', 'STARTING', 'DEGRADED', 'DRIFTED'].includes(job.state) && <button className="secondary-action" onClick={() => onAction('cancel')}>取消</button>}
                {job.state === 'FAILED' && <button className="primary-action" onClick={() => onAction('retry')}>重试</button>}
              </div>
            )}
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

function JobSpecView({ spec }: { spec: JobSpec }) {
  const resources = specResources(spec)
  const workload = spec.workload
  return (
    <div className="job-spec">
      <p className="eyebrow">LAUNCH SPEC</p>
      <div className="plan-line"><span>类型</span><strong>{workload.kind === 'image' ? 'Image' : 'Compose'}</strong></div>
      {spec.resourcePoolId && <div className="plan-line"><span>资源池</span><strong>{spec.resourcePoolId.slice(0, 8)}</strong></div>}
      {workload.kind === 'image' && (
        <>
          <div className="plan-line"><span>镜像</span><strong>{workload.image}</strong></div>
          {workload.command?.length ? <div className="plan-line"><span>命令</span><strong>{workload.command.join(' ')}</strong></div> : null}
          {workload.entrypoint?.length ? <div className="plan-line"><span>入口</span><strong>{workload.entrypoint.join(' ')}</strong></div> : null}
          {workload.workingDirectory ? <div className="plan-line"><span>工作目录</span><strong>{workload.workingDirectory}</strong></div> : null}
          {(workload.mounts ?? []).map((item, index) => (
            <div className="plan-line" key={`${item.source}-${index}`}>
              <span>挂载{item.readOnly ? '（只读）' : ''}</span>
              <strong>{item.source} → {item.target}</strong>
            </div>
          ))}
          {Object.entries(workload.environment ?? {}).map(([key, value]) => (
            <div className="plan-line" key={key}><span>环境</span><strong>{key}={value}</strong></div>
          ))}
          {(workload.publishedPorts ?? []).map((item, index) => (
            <div className="plan-line" key={`${item.hostPort}-${index}`}>
              <span>端口</span>
              <strong>{item.hostPort}→{item.containerPort}/{item.protocol || 'tcp'}</strong>
            </div>
          ))}
          {workload.shmSizeBytes ? <div className="plan-line"><span>共享内存</span><strong>{formatBytes(workload.shmSizeBytes)}</strong></div> : null}
          {workload.user ? <div className="plan-line"><span>用户</span><strong>{workload.user}</strong></div> : null}
        </>
      )}
      {workload.kind === 'compose' && (
        <>
          <div className="plan-line"><span>Compose 来源</span><strong>{workload.composeSourceId}</strong></div>
          {workload.services.map((item) => (
            <div className="plan-line" key={item.name}><span>服务</span><strong>{item.name} · {item.class}</strong></div>
          ))}
        </>
      )}
      <div className="plan-line"><span>GPU</span><strong>{gpuLabel(resources)}</strong></div>
      <div className="plan-line"><span>CPU</span><strong>{cpuLabel(resources)}</strong></div>
      <div className="plan-line"><span>内存</span><strong>{formatBytes(resources.memoryBytes)}</strong></div>
      <div className="plan-line"><span>停止宽限</span><strong>{spec.stopGraceSeconds}s</strong></div>
    </div>
  )
}
