import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, CircleGauge, Cpu, Gauge, HardDrive, LayoutGrid, X } from 'lucide-react'
import { api } from './api'
import { cpuSelectable, selectedGPUNuma } from './numa'
import type { JobPlan, JobSpec, ResourceRequest, Topology } from './types'

function formatBytes(value = 0): string {
  if (!value) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(value >= 1024 ** 4 ? 0 : 1)} GB`
}

function emptyResources(): ResourceRequest {
  return { cpuMode: 'auto-cores', cpuCount: 8, gpuMode: 'auto', gpuCount: 1, memoryBytes: 64 * 1024 ** 3, numaPolicy: 'strict' }
}

export function NewJobDialog({ topology, onClose }: { topology?: Topology; onClose: () => void }) {
  const queryClient = useQueryClient()
  const images = useQuery({ queryKey: ['images'], queryFn: api.images, refetchInterval: false })
  const sources = useQuery({ queryKey: ['compose-sources'], queryFn: api.composeSources, refetchInterval: false })
  const [kind, setKind] = useState<'image' | 'compose'>('image')
  const [name, setName] = useState('')
  const [image, setImage] = useState('')
  const [command, setCommand] = useState('python\ntrain.py')
  const [workingDirectory, setWorkingDirectory] = useState('/workspace')
  const [composeSourceId, setComposeSourceId] = useState('')
  const [serviceName, setServiceName] = useState('train')
  const [priority, setPriority] = useState<JobSpec['priority']>('P2')
  const [gpuMode, setGpuMode] = useState<ResourceRequest['gpuMode']>('auto')
  const [gpuCount, setGpuCount] = useState(1)
  const [selectedGPUs, setSelectedGPUs] = useState<string[]>([])
  const [cpuMode, setCpuMode] = useState<ResourceRequest['cpuMode']>('auto-cores')
  const [cpuCount, setCpuCount] = useState(8)
  const [selectedCPUs, setSelectedCPUs] = useState<number[]>([])
  const [memoryGB, setMemoryGB] = useState(64)
  const [plan, setPlan] = useState<JobPlan>()
  const [planKey, setPlanKey] = useState('')

  const resources = (): ResourceRequest => ({
    cpuMode, cpuCount: cpuMode === 'manual-threads' ? selectedCPUs.length : cpuCount,
    cpuIds: cpuMode === 'manual-threads' ? selectedCPUs : undefined,
    gpuMode, gpuCount: gpuMode === 'manual' ? selectedGPUs.length : gpuCount,
    gpuUuids: gpuMode === 'manual' ? selectedGPUs : undefined,
    memoryBytes: memoryGB * 1024 ** 3, numaPolicy: 'strict',
  })

  const buildSpec = (): JobSpec => ({
    name, priority,
    workload: kind === 'image' ? {
      kind: 'image', image,
      command: command.split('\n').map((item) => item.trim()).filter(Boolean),
      workingDirectory,
    } : {
      kind: 'compose', composeSourceId,
      services: [{ name: serviceName, class: 'training', resources: resources() }],
    },
    resources: kind === 'image' ? resources() : emptyResources(),
    stopGraceSeconds: 30,
  })

  const preview = useMutation({
    mutationFn: () => api.planJob(buildSpec()),
    onSuccess: (nextPlan) => { setPlan(nextPlan); setPlanKey(JSON.stringify(buildSpec())) },
  })
  const create = useMutation({
    mutationFn: () => api.createJob(buildSpec()),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['jobs'] }),
        queryClient.invalidateQueries({ queryKey: ['availability'] }),
      ])
      onClose()
    },
  })

  const toggleGPU = (uuid: string) => {
    setSelectedGPUs((current) => {
      const next = current.includes(uuid) ? current.filter((item) => item !== uuid) : [...current, uuid]
      const numa = selectedGPUNuma(topology, next)
      setSelectedCPUs((cpus) => cpus.filter((id) => {
        const cpu = topology?.cpus.find((item) => item.id === id)
        return Boolean(cpu && cpuSelectable(cpu, numa))
      }))
      return next
    })
    setPlan(undefined)
  }
  const toggleCPU = (id: number) => setSelectedCPUs((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
  const ready = name.trim() && (kind === 'image' ? image && command.trim() : composeSourceId && serviceName.trim()) && memoryGB > 0
  const planCurrent = planKey === JSON.stringify(buildSpec())
  const allowedNuma = gpuMode === 'manual' ? selectedGPUNuma(topology, selectedGPUs) : new Set<number>()

  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section className="job-dialog" role="dialog" aria-modal="true" aria-labelledby="new-job-title">
        <header className="dialog-header">
          <div>
            <p className="eyebrow">NEW WORKLOAD</p>
            <h2 id="new-job-title">创建训练任务</h2>
          </div>
          <button className="icon-button" onClick={onClose} aria-label="关闭"><X size={18} /></button>
        </header>
        <div className="dialog-body">
          <div className="form-column">
            <fieldset>
              <legend>01 / 基本信息</legend>
              <div className="mode-switch">
                <button className={kind === 'image' ? 'active' : ''} onClick={() => { setKind('image'); setPlan(undefined) }}>Image</button>
                <button className={kind === 'compose' ? 'active' : ''} onClick={() => { setKind('compose'); setPlan(undefined) }}>Compose</button>
              </div>
              <div className="field-grid two">
                <label>任务名称<input value={name} onChange={(event) => setName(event.target.value)} placeholder="experiment-a800-01" /></label>
                <label>优先级<select value={priority} onChange={(event) => setPriority(event.target.value as JobSpec['priority'])}><option>P2</option><option>P3</option><option>P1</option><option>P0</option></select></label>
              </div>
              {kind === 'image' ? <>
                <label>本机镜像<select value={image} onChange={(event) => setImage(event.target.value)}><option value="">选择 Docker Image</option>{(images.data ?? []).map((item) => <option key={`${item.id}-${item.tag}`} value={`${item.repository}:${item.tag}`}>{item.repository}:{item.tag} · {formatBytes(item.sizeBytes)}</option>)}</select></label>
                <div className="field-grid two">
                  <label>工作目录<input value={workingDirectory} onChange={(event) => setWorkingDirectory(event.target.value)} /></label>
                  <label>内存限制（GB）<input type="number" min={1} value={memoryGB} onChange={(event) => setMemoryGB(Number(event.target.value))} /></label>
                </div>
                <label>启动 argv <small>每行一个参数，不经过 Shell</small><textarea value={command} onChange={(event) => setCommand(event.target.value)} rows={4} /></label>
              </> : <>
                <label>Compose 来源<select value={composeSourceId} onChange={(event) => setComposeSourceId(event.target.value)}><option value="">选择已登记来源</option>{(sources.data ?? []).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.entryFile}</option>)}</select></label>
                <div className="field-grid two">
                  <label>训练服务名<input value={serviceName} onChange={(event) => setServiceName(event.target.value)} /></label>
                  <label>内存限制（GB）<input type="number" min={1} value={memoryGB} onChange={(event) => setMemoryGB(Number(event.target.value))} /></label>
                </div>
              </>}
            </fieldset>
            <fieldset>
              <legend>02 / GPU 与 NUMA</legend>
              <div className="mode-switch"><button className={gpuMode === 'auto' ? 'active' : ''} onClick={() => { setGpuMode('auto'); setPlan(undefined) }}>自动分配</button><button className={gpuMode === 'manual' ? 'active' : ''} onClick={() => { setGpuMode('manual'); setPlan(undefined) }}>手动整卡</button></div>
              {gpuMode === 'auto' ? <label>GPU 数量<input type="number" min={0} max={topology?.gpus.length ?? 8} value={gpuCount} onChange={(event) => setGpuCount(Number(event.target.value))} /></label> :
                <div className="resource-choice-grid">{(topology?.gpus ?? []).map((gpu) => <button key={gpu.uuid} className={selectedGPUs.includes(gpu.uuid) ? 'resource-choice selected' : 'resource-choice'} onClick={() => toggleGPU(gpu.uuid)}><span>GPU {gpu.index}</span><small>NUMA {gpu.numaNode}</small>{selectedGPUs.includes(gpu.uuid) && <Check size={14} />}</button>)}</div>}
            </fieldset>
            <fieldset>
              <legend>03 / CPU</legend>
              <div className="mode-switch"><button className={cpuMode === 'auto-cores' ? 'active' : ''} onClick={() => { setCpuMode('auto-cores'); setPlan(undefined) }}>完整物理核</button><button className={cpuMode === 'manual-threads' ? 'active' : ''} onClick={() => { setCpuMode('manual-threads'); setPlan(undefined) }}>高级线程选择</button></div>
              {cpuMode === 'auto-cores' ? <label>物理核数量<input type="number" min={1} value={cpuCount} onChange={(event) => setCpuCount(Number(event.target.value))} /></label> :
                <div className="thread-choice-grid">{(topology?.cpus ?? []).map((cpu) => {
                  const allowed = cpuSelectable(cpu, allowedNuma)
                  return <button disabled={!allowed} key={cpu.id} className={selectedCPUs.includes(cpu.id) ? 'thread-choice selected' : 'thread-choice'} onClick={() => toggleCPU(cpu.id)}>{cpu.id}<small>N{cpu.numaNode}</small></button>
                })}</div>}
              {cpuMode === 'manual-threads' && allowedNuma.size > 0 && <small className="field-hint">已按所选 GPU 限制为 NUMA {[...allowedNuma].sort((a, b) => a - b).join(', ')}</small>}
            </fieldset>
          </div>
          <aside className="plan-column">
            <p className="eyebrow">EFFECTIVE PLAN</p>
            <h3>资源预检</h3>
            {!plan && <div className="plan-empty"><CircleGauge size={30} /><span>填写资源后执行预检。资源不足时任务会进入队列。</span></div>}
            {plan && <>
              <div className={plan.schedulable ? 'plan-status ok' : 'plan-status error'}>{plan.schedulable ? <Check size={17} /> : <X size={17} />}{plan.schedulable ? '资源可立即调度' : '将进入排队'}</div>
              <div className="plan-line"><span><Gauge size={15} />GPU</span><strong>{plan.gpuUuids.length ? `${plan.gpuUuids.length} 张整卡` : '无'}</strong></div>
              <div className="plan-line"><span><Cpu size={15} />CPU</span><strong>{`${plan.cpuIds.length} 线程`}</strong></div>
              <div className="plan-line"><span><LayoutGrid size={15} />NUMA</span><strong>{plan.numaNodes.join(', ') || '—'}</strong></div>
              <div className="plan-line"><span><HardDrive size={15} />内存</span><strong>{`${memoryGB} GB`}</strong></div>
              {plan.conflicts.map((conflict) => <div className="plan-conflict" key={conflict}>{conflict}</div>)}
            </>}
            {plan && !planCurrent && <div className="plan-conflict">配置已经变化，请重新执行资源预检。</div>}
            {(preview.error || create.error) && <div className="plan-conflict">请求失败：{String((preview.error ?? create.error)?.message)}</div>}
            <div className="dialog-actions">
              <button className="secondary-action" disabled={!ready || preview.isPending} onClick={() => preview.mutate()}>{preview.isPending ? '计算中…' : '资源预检'}</button>
              <button className="primary-action" disabled={!plan || !planCurrent || create.isPending} onClick={() => create.mutate()}>{create.isPending ? '正在提交…' : plan?.schedulable ? '立即启动' : '加入队列'}</button>
            </div>
          </aside>
        </div>
      </section>
    </div>
  )
}
