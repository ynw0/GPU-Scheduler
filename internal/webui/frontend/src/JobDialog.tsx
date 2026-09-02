import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, CircleGauge, Cpu, Gauge, HardDrive, LayoutGrid, X } from 'lucide-react'
import { api } from './api'
import { cpuSelectable, selectedGPUNuma } from './numa'
import { emptyImageFields, imageFieldsFromWorkload, JobImageFields, parseEnvironment, type ImageFields } from './JobImageFields'
import { existingMigProfiles, gpuDeviceLabel, idleMigProfiles, migInstances, wholeGPUs } from './gpuLabel'
import type { ImageWorkload, JobPlan, JobSpec, ResourceRequest, Topology, User } from './types'

function emptyResources(): ResourceRequest {
  return { cpuMode: 'auto-cores', cpuCount: 8, gpuKind: 'whole', gpuMode: 'auto', gpuCount: 1, memoryBytes: 64 * 1024 ** 3, numaPolicy: 'strict' }
}

function specResources(spec?: JobSpec): ResourceRequest {
  if (!spec) return emptyResources()
  if (spec.workload.kind === 'compose') {
    return spec.workload.services.find((item) => item.class === 'training')?.resources ?? spec.resources
  }
  return spec.resources
}

function canUseP0(role?: User['role']): boolean {
  return role === 'Admin' || role === 'TeamOperator'
}

function clampPriority(role: User['role'] | undefined, priority: JobSpec['priority']): JobSpec['priority'] {
  if (priority === 'P0' && !canUseP0(role)) return 'P1'
  return priority
}

export function NewJobDialog({ topology, freeGPUUUIDs, initialSpec, onClose }: {
  topology?: Topology
  freeGPUUUIDs?: Set<string>
  initialSpec?: JobSpec
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, refetchInterval: false })
  const images = useQuery({ queryKey: ['images'], queryFn: api.images, refetchInterval: false })
  const sources = useQuery({ queryKey: ['compose-sources'], queryFn: api.composeSources, refetchInterval: false })
  const pools = useQuery({ queryKey: ['resource-pools'], queryFn: api.resourcePools, refetchInterval: false })
  const templates = useQuery({ queryKey: ['templates'], queryFn: api.templates, refetchInterval: false })
  const platform = useQuery({ queryKey: ['platform'], queryFn: api.platform, refetchInterval: false })
  const seed = specResources(initialSpec)
  const [kind, setKind] = useState<'image' | 'compose'>(initialSpec?.workload.kind ?? 'image')
  const [name, setName] = useState(initialSpec?.name ?? '')
  const [imageFields, setImageFields] = useState<ImageFields>(
    initialSpec?.workload.kind === 'image' ? imageFieldsFromWorkload(initialSpec.workload) : emptyImageFields(),
  )
  const [composeSourceId, setComposeSourceId] = useState(initialSpec?.workload.kind === 'compose' ? initialSpec.workload.composeSourceId : '')
  const [composeInput, setComposeInput] = useState<'catalog' | 'path' | 'upload'>('catalog')
  const [composePath, setComposePath] = useState('')
  const [composeFileName, setComposeFileName] = useState('compose.yaml')
  const [composeContent, setComposeContent] = useState('')
  const [serviceName, setServiceName] = useState(initialSpec?.workload.kind === 'compose' ? (initialSpec.workload.services[0]?.name ?? 'train') : 'train')
  const [priority, setPriority] = useState<JobSpec['priority']>(clampPriority(me.data?.role, initialSpec?.priority ?? 'P2'))
  const [resourcePoolId, setResourcePoolId] = useState(initialSpec?.resourcePoolId ?? '')
  const [templateName, setTemplateName] = useState('')
  const [loadTemplateId, setLoadTemplateId] = useState('')
  const [activeTemplateId, setActiveTemplateId] = useState('')
  const [activeTemplateName, setActiveTemplateName] = useState('')
  const [stopGraceSeconds, setStopGraceSeconds] = useState(initialSpec?.stopGraceSeconds || 30)
  const [gpuKind, setGpuKind] = useState<NonNullable<ResourceRequest['gpuKind']>>(seed.gpuKind === 'mig' ? 'mig' : 'whole')
  const [gpuMode, setGpuMode] = useState<ResourceRequest['gpuMode']>(seed.gpuMode)
  const [gpuCount, setGpuCount] = useState(seed.gpuCount)
  const [migProfile, setMigProfile] = useState(seed.migProfile ?? '')
  const [selectedGPUs, setSelectedGPUs] = useState<string[]>(seed.gpuUuids ?? [])
  const [cpuMode, setCpuMode] = useState<ResourceRequest['cpuMode']>(seed.cpuMode)
  const [cpuCount, setCpuCount] = useState(seed.cpuCount)
  const [selectedCPUs, setSelectedCPUs] = useState<number[]>(seed.cpuIds ?? [])
  const [memoryGB, setMemoryGB] = useState(Math.max(1, Math.round(seed.memoryBytes / 1024 ** 3)))
  const [plan, setPlan] = useState<JobPlan>()
  const [planKey, setPlanKey] = useState('')

  const resources = (): ResourceRequest => ({
    cpuMode, cpuCount: cpuMode === 'manual-threads' ? selectedCPUs.length : cpuCount,
    cpuIds: cpuMode === 'manual-threads' ? selectedCPUs : undefined,
    gpuKind,
    gpuMode,
    gpuCount: gpuKind === 'mig' ? 1 : gpuMode === 'manual' ? selectedGPUs.length : gpuCount,
    gpuUuids: gpuMode === 'manual' ? selectedGPUs : undefined,
    ...(gpuKind === 'mig' ? { migProfile } : {}),
    memoryBytes: memoryGB * 1024 ** 3, numaPolicy: 'strict',
  })

  const imageWorkload = (): ImageWorkload => {
    const command = imageFields.command.split('\n').map((item) => item.trim()).filter(Boolean)
    const entrypoint = imageFields.entrypoint.split('\n').map((item) => item.trim()).filter(Boolean)
    const environment = parseEnvironment(imageFields.environment)
    const mounts = imageFields.mounts.filter((item) => item.source.trim() && item.target.trim()).map((item) => ({
      source: item.source.trim(), target: item.target.trim(), readOnly: Boolean(item.readOnly),
    }))
    const publishedPorts = imageFields.ports.filter((item) => item.hostPort > 0 && item.containerPort > 0)
    const shmMB = Number(imageFields.shmMB)
    return {
      kind: 'image',
      image: imageFields.image,
      ...(command.length ? { command } : {}),
      ...(entrypoint.length ? { entrypoint } : {}),
      ...(imageFields.workingDirectory.trim() ? { workingDirectory: imageFields.workingDirectory.trim() } : {}),
      ...(Object.keys(environment).length ? { environment } : {}),
      ...(mounts.length ? { mounts } : {}),
      ...(publishedPorts.length ? { publishedPorts } : {}),
      ...(shmMB > 0 ? { shmSizeBytes: shmMB * 1024 * 1024 } : {}),
      ...(imageFields.user.trim() ? { user: imageFields.user.trim() } : {}),
      ...(imageFields.network.trim() ? { network: imageFields.network.trim() } : {}),
    }
  }

  const buildSpec = (): JobSpec => ({
    name, priority: clampPriority(me.data?.role, priority),
    workload: kind === 'image' ? imageWorkload() : {
      kind: 'compose', composeSourceId,
      services: [{ name: serviceName, class: 'training', resources: resources() }],
    },
    resources: kind === 'image' ? resources() : emptyResources(),
    ...(resourcePoolId ? { resourcePoolId } : {}),
    stopGraceSeconds,
  })

  const applySpec = (spec: JobSpec) => {
    const next = specResources(spec)
    setName(spec.name)
    setPriority(clampPriority(me.data?.role, spec.priority))
    setKind(spec.workload.kind)
    setResourcePoolId(spec.resourcePoolId ?? '')
    setStopGraceSeconds(spec.stopGraceSeconds || 30)
    setGpuKind(next.gpuKind === 'mig' ? 'mig' : 'whole')
    setGpuMode(next.gpuMode)
    setGpuCount(next.gpuCount)
    setMigProfile(next.migProfile ?? '')
    setSelectedGPUs(next.gpuUuids ?? [])
    setCpuMode(next.cpuMode)
    setCpuCount(next.cpuCount)
    setSelectedCPUs(next.cpuIds ?? [])
    setMemoryGB(Math.max(1, Math.round(next.memoryBytes / 1024 ** 3)))
    if (spec.workload.kind === 'image') {
      setImageFields(imageFieldsFromWorkload(spec.workload))
    } else {
      setComposeInput('catalog')
      setComposeSourceId(spec.workload.composeSourceId)
      setServiceName(spec.workload.services[0]?.name ?? 'train')
    }
    setPlan(undefined)
  }

  const fingerprint = () => JSON.stringify({ spec: buildSpec(), composeInput, composePath, composeFileName, composeContent })

  const ensureSource = async (): Promise<string> => {
    if (kind !== 'compose') return ''
    if (composeInput === 'catalog') return composeSourceId
    if (composeSourceId) return composeSourceId
    if (composeInput === 'path') {
      const item = await api.createComposeFromPath({ name, filePath: composePath })
      setComposeSourceId(item.id)
      return item.id
    }
    const item = await api.uploadCompose({ name, fileName: composeFileName, content: composeContent })
    setComposeSourceId(item.id)
    return item.id
  }

  const preview = useMutation({
    mutationFn: async () => {
      const sourceId = await ensureSource()
      const spec = buildSpec()
      if (kind === 'compose') {
        spec.workload = { kind: 'compose', composeSourceId: sourceId, services: spec.workload.kind === 'compose' ? spec.workload.services : [{ name: serviceName, class: 'training', resources: resources() }] }
      }
      return api.planJob(spec)
    },
    onSuccess: (nextPlan) => { setPlan(nextPlan); setPlanKey(fingerprint()) },
  })
  const create = useMutation({
    mutationFn: async () => {
      const sourceId = await ensureSource()
      const spec = buildSpec()
      if (kind === 'compose') {
        spec.workload = { kind: 'compose', composeSourceId: sourceId, services: spec.workload.kind === 'compose' ? spec.workload.services : [{ name: serviceName, class: 'training', resources: resources() }] }
      }
      return api.createJob(spec)
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['jobs'] }),
        queryClient.invalidateQueries({ queryKey: ['availability'] }),
      ])
      onClose()
    },
  })
  const saveTemplate = useMutation({
    mutationFn: () => api.createTemplate(templateName.trim(), buildSpec()),
    onSuccess: (item) => {
      void queryClient.invalidateQueries({ queryKey: ['templates'] })
      setTemplateName('')
      setActiveTemplateId(item.id)
      setActiveTemplateName(item.name)
    },
  })
  const overwriteTemplate = useMutation({
    mutationFn: () => api.updateTemplate(activeTemplateId, activeTemplateName, buildSpec()),
    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['templates'] }) },
  })

  const toggleGPU = (uuid: string) => {
    setSelectedGPUs((current) => {
      if (gpuKind === 'mig') {
        const next = current.includes(uuid) ? [] : [uuid]
        const picked = (topology?.gpus ?? []).find((gpu) => gpu.uuid === (next[0] ?? ''))
        if (picked?.profile) setMigProfile(picked.profile)
        return next
      }
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
  const composeReady = composeInput === 'catalog' ? composeSourceId : composeInput === 'path' ? composePath.trim() : composeContent.trim()
  const ready = Boolean(name.trim()) && (gpuKind !== 'mig' || Boolean(migProfile)) && (kind === 'image' ? imageFields.image && memoryGB > 0 : composeReady && serviceName.trim() && memoryGB > 0)
  const planCurrent = planKey === fingerprint()
  const allowedNuma = gpuMode === 'manual' ? selectedGPUNuma(topology, selectedGPUs) : new Set<number>()
  const cards = wholeGPUs(topology?.gpus ?? [])
  const instances = migInstances(topology?.gpus ?? [])
  const freeSet = freeGPUUUIDs ?? new Set(instances.map((item) => item.uuid))
  const profiles = existingMigProfiles(topology?.gpus ?? [])
  const idleProfiles = idleMigProfiles(topology?.gpus ?? [], freeSet)

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
                <label>优先级<select value={clampPriority(me.data?.role, priority)} onChange={(event) => setPriority(event.target.value as JobSpec['priority'])}>
                  <option>P2</option><option>P3</option><option>P1</option>
                  {canUseP0(me.data?.role) && <option>P0</option>}
                </select></label>
              </div>
              <label>停止宽限（秒）<input type="number" min={1} value={stopGraceSeconds} onChange={(event) => { setStopGraceSeconds(Number(event.target.value)); setPlan(undefined) }} /></label>
              <label>资源池<select value={resourcePoolId} onChange={(event) => { setResourcePoolId(event.target.value); setPlan(undefined) }}>
                <option value="">不限制（全机可分配资源）</option>
                {(pools.data ?? []).map((pool) => (
                  <option key={pool.id} value={pool.id}>{pool.name} · {pool.gpuUuids.length} GPU · {pool.cpuIds.length} CPU</option>
                ))}
              </select></label>
              <div className="field-grid two">
                <label>从模板加载<select value={loadTemplateId} onChange={(event) => {
                  const item = (templates.data ?? []).find((template) => template.id === event.target.value)
                  setLoadTemplateId('')
                  if (item) {
                    applySpec(item.spec)
                    setActiveTemplateId(item.id)
                    setActiveTemplateName(item.name)
                  }
                }}>
                  <option value="">选择模板</option>
                  {(templates.data ?? []).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select></label>
                <label>存为模板
                  <span className="inline-save">
                    <input value={templateName} onChange={(event) => setTemplateName(event.target.value)} placeholder="模板名称" />
                    <button type="button" className="text-button" disabled={!templateName.trim() || !name.trim() || saveTemplate.isPending} onClick={() => saveTemplate.mutate()}>
                      {saveTemplate.isPending ? '保存中…' : '保存'}
                    </button>
                  </span>
                </label>
              </div>
              {activeTemplateId && (
                <button
                  type="button"
                  className="text-button"
                  disabled={overwriteTemplate.isPending || !name.trim()}
                  onClick={() => overwriteTemplate.mutate()}
                >
                  {overwriteTemplate.isPending ? '覆盖中…' : `覆盖模板「${activeTemplateName}」`}
                </button>
              )}
              {(saveTemplate.error || overwriteTemplate.error) && <div className="plan-conflict">{String((saveTemplate.error ?? overwriteTemplate.error)?.message)}</div>}
              {kind === 'image' ? (
                <JobImageFields
                  images={images.data ?? []}
                  value={imageFields}
                  memoryGB={memoryGB}
                  allowedRoots={platform.data?.allowedMountRoots}
                  onChange={(next) => { setImageFields(next); setPlan(undefined) }}
                  onMemory={(gb) => { setMemoryGB(gb); setPlan(undefined) }}
                />
              ) : <>
                <div className="mode-switch">
                  <button className={composeInput === 'catalog' ? 'active' : ''} onClick={() => { setComposeInput('catalog'); setComposeSourceId(''); setPlan(undefined) }}>已登记</button>
                  <button className={composeInput === 'path' ? 'active' : ''} onClick={() => { setComposeInput('path'); setComposeSourceId(''); setPlan(undefined) }}>本机路径</button>
                  <button className={composeInput === 'upload' ? 'active' : ''} onClick={() => { setComposeInput('upload'); setComposeSourceId(''); setPlan(undefined) }}>上传文件</button>
                </div>
                {composeInput === 'catalog' && <label>Compose 来源<select value={composeSourceId} onChange={(event) => setComposeSourceId(event.target.value)}><option value="">选择已登记来源</option>{(sources.data ?? []).filter((item) => item.enabled).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.entryFile}</option>)}</select></label>}
                {composeInput === 'path' && <label>本机 compose 路径<input value={composePath} onChange={(event) => setComposePath(event.target.value)} placeholder="/mnt/disk/train/compose.yaml" /></label>}
                {composeInput === 'upload' && <>
                  <label>文件名<input value={composeFileName} onChange={(event) => setComposeFileName(event.target.value)} /></label>
                  <label>Compose 正文<textarea value={composeContent} onChange={(event) => setComposeContent(event.target.value)} rows={8} placeholder="services:&#10;  train:&#10;    image: ..." /></label>
                </>}
                <div className="field-grid two">
                  <label>训练服务名<input value={serviceName} onChange={(event) => setServiceName(event.target.value)} /></label>
                  <label>内存限制（GB）<input type="number" min={1} value={memoryGB} onChange={(event) => setMemoryGB(Number(event.target.value))} /></label>
                </div>
              </>}
            </fieldset>
            <fieldset>
              <legend>02 / GPU 与 NUMA</legend>
              <div className="mode-switch">
                <button className={gpuKind === 'whole' ? 'active' : ''} onClick={() => { setGpuKind('whole'); setGpuMode('auto'); setSelectedGPUs([]); setMigProfile(''); setPlan(undefined) }}>整卡</button>
                <button className={gpuKind === 'mig' ? 'active' : ''} onClick={() => { setGpuKind('mig'); setGpuMode('auto'); setGpuCount(1); setSelectedGPUs([]); setPlan(undefined) }}>MIG</button>
              </div>
              {gpuKind === 'whole' ? <>
                <div className="mode-switch"><button className={gpuMode === 'auto' ? 'active' : ''} onClick={() => { setGpuMode('auto'); setPlan(undefined) }}>自动分配</button><button className={gpuMode === 'manual' ? 'active' : ''} onClick={() => { setGpuMode('manual'); setPlan(undefined) }}>手动整卡</button></div>
                {gpuMode === 'auto' ? <label>GPU 数量<input type="number" min={0} max={cards.length} value={gpuCount} onChange={(event) => setGpuCount(Number(event.target.value))} /></label> :
                  <div className="resource-choice-grid">{cards.map((gpu) => <button key={gpu.uuid} className={selectedGPUs.includes(gpu.uuid) ? 'resource-choice selected' : 'resource-choice'} onClick={() => toggleGPU(gpu.uuid)}><span>GPU {gpu.index}</span><small>NUMA {gpu.numaNode}</small>{selectedGPUs.includes(gpu.uuid) && <Check size={14} />}</button>)}</div>}
              </> : <>
                <div className="mode-switch"><button className={gpuMode === 'auto' ? 'active' : ''} onClick={() => { setGpuMode('auto'); setSelectedGPUs([]); setPlan(undefined) }}>自动订 1 片</button><button className={gpuMode === 'manual' ? 'active' : ''} onClick={() => { setGpuMode('manual'); setPlan(undefined) }}>点选实例</button></div>
                <label>档位<select value={migProfile} onChange={(event) => { setMigProfile(event.target.value); setSelectedGPUs([]); setPlan(undefined) }}>
                  <option value="">选择档位</option>
                  {profiles.map((name) => <option key={name} value={name}>{name}</option>)}
                </select></label>
                {gpuMode === 'manual' && <div className="resource-choice-grid">{instances.filter((gpu) => !migProfile || gpu.profile === migProfile).map((gpu) => {
                  const free = freeSet.has(gpu.uuid)
                  return <button key={gpu.uuid} disabled={!free} className={selectedGPUs.includes(gpu.uuid) ? 'resource-choice selected' : 'resource-choice'} onClick={() => toggleGPU(gpu.uuid)}><span>GPU {gpu.index} · {gpu.profile}</span><small>{free ? '空闲' : '占用'}</small>{selectedGPUs.includes(gpu.uuid) && <Check size={14} />}</button>
                })}</div>}
                {gpuKind === 'mig' && !idleProfiles.length && <small className="field-hint">当前没有空闲 MIG 实例，选择档位后任务将进入队列。</small>}
              </>}
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
              <div className="plan-line"><span><Gauge size={15} />GPU</span><strong>{plan.gpuUuids.length ? plan.gpuUuids.map((uuid) => gpuDeviceLabel(topology, uuid)).join('、') : '无'}</strong></div>
              <div className="plan-line"><span><Cpu size={15} />CPU</span><strong>{`${plan.cpuIds.length} 线程`}</strong></div>
              <div className="plan-line"><span><LayoutGrid size={15} />NUMA</span><strong>{plan.numaNodes.join(', ') || '—'}</strong></div>
              <div className="plan-line"><span><HardDrive size={15} />内存</span><strong>{`${memoryGB} GB`}</strong></div>
              {plan.conflicts.map((conflict) => <div className="plan-conflict" key={conflict}>{conflict}</div>)}
              {(plan.warnings ?? []).map((warning) => <small className="field-hint" key={warning}>{warning}</small>)}
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
