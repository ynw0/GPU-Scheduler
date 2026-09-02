import { useState } from 'react'
import type { ImageInfo, ImageWorkload, Mount, PortPublish } from './types'
import { matchCatalogImage, parseDockerRun } from './parseDockerRun'

function formatBytes(value = 0): string {
  if (!value) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(value >= 1024 ** 4 ? 0 : 1)} GB`
}

export type ImageFields = {
  image: string
  command: string
  entrypoint: string
  workingDirectory: string
  environment: string
  mounts: Mount[]
  ports: PortPublish[]
  shmMB: string
  user: string
  network: string
}

export const emptyImageFields = (): ImageFields => ({
  image: '', command: '', entrypoint: '', workingDirectory: '', environment: '',
  mounts: [], ports: [], shmMB: '', user: '', network: '',
})

function lines(value: string): string[] {
  return value.split('\n').map((item) => item.trim()).filter(Boolean)
}

export function parseEnvironment(value: string): Record<string, string> {
  const result: Record<string, string> = {}
  for (const line of lines(value)) {
    const index = line.indexOf('=')
    if (index <= 0) continue
    result[line.slice(0, index).trim()] = line.slice(index + 1)
  }
  return result
}

export function imageFieldsFromWorkload(workload: ImageWorkload): ImageFields {
  return {
    image: workload.image,
    command: (workload.command ?? []).join('\n'),
    entrypoint: (workload.entrypoint ?? []).join('\n'),
    workingDirectory: workload.workingDirectory ?? '',
    environment: Object.entries(workload.environment ?? {}).map(([key, value]) => `${key}=${value}`).join('\n'),
    mounts: workload.mounts ?? [],
    ports: workload.publishedPorts ?? [],
    shmMB: workload.shmSizeBytes ? String(Math.round(workload.shmSizeBytes / 1024 / 1024)) : '',
    user: workload.user ?? '',
    network: workload.network ?? '',
  }
}

export function JobImageFields({
  images, value, memoryGB, allowedRoots = [], onChange, onMemory,
}: {
  images: ImageInfo[]
  value: ImageFields
  memoryGB: number
  allowedRoots?: string[]
  onChange: (next: ImageFields) => void
  onMemory: (gb: number) => void
}) {
  const set = (patch: Partial<ImageFields>) => onChange({ ...value, ...patch })
  const setMount = (index: number, patch: Partial<Mount>) => {
    set({ mounts: value.mounts.map((item, i) => i === index ? { ...item, ...patch } : item) })
  }
  const setPort = (index: number, patch: Partial<PortPublish>) => {
    set({ ports: value.ports.map((item, i) => i === index ? { ...item, ...patch } : item) })
  }
  const [dockerRun, setDockerRun] = useState('')
  const [parseError, setParseError] = useState('')
  const [parseNotes, setParseNotes] = useState<string[]>([])

  const fillFromDockerRun = () => {
    const result = parseDockerRun(dockerRun)
    if (result.error) {
      setParseError(result.error)
      setParseNotes([])
      return
    }
    const next = { ...value, ...result.fields }
    const notes: string[] = []
    let filledImage = false
    if (result.image) {
      const catalog = matchCatalogImage(result.image, images)
      if (catalog) {
        next.image = catalog
        filledImage = true
      } else notes.push(`识别到镜像 ${result.image}，本机列表没有`)
    }
    onChange(next)
    if (result.memoryGB) onMemory(result.memoryGB)
    const applied = filledImage ? result.applied : result.applied.filter((item) => item !== '镜像')
    if (applied.length) notes.push(`已填写：${applied.join('、')}`)
    if (result.ignored.length) notes.push(`已忽略（以本页 CPU/GPU 设置为准）：${result.ignored.join('、')}`)
    if (result.unknown.length) notes.push(`未识别、未采用：${result.unknown.join('、')}`)
    setParseError('')
    setParseNotes(notes)
  }

  return (
    <>
      <label>
        从 docker run 填入
        <small>只用来识别填写下面的字段，不会执行这条命令。CPU / GPU 始终以本页资源设置为准。</small>
        <textarea value={dockerRun} onChange={(event) => setDockerRun(event.target.value)} rows={5} placeholder="docker run --rm -e FOO=1 -v /mnt/disk/exp:/data image:tag python train.py" />
      </label>
      <button type="button" className="secondary-action" onClick={fillFromDockerRun}>识别填写</button>
      {parseError && <div className="plan-conflict">{parseError}</div>}
      {parseNotes.map((note) => <small className="field-hint" key={note}>{note}</small>)}
      <label>本机镜像<select value={value.image} onChange={(event) => set({ image: event.target.value })}>
        <option value="">选择 Docker Image</option>
        {images.map((item) => (
          <option key={`${item.id}-${item.tag}`} value={`${item.repository}:${item.tag}`}>
            {item.repository}:{item.tag} · {formatBytes(item.sizeBytes)}
          </option>
        ))}
      </select></label>
      <div className="field-grid two">
        <label>工作目录<input value={value.workingDirectory} onChange={(event) => set({ workingDirectory: event.target.value })} placeholder="留空则使用镜像 WORKDIR" /></label>
        <label>内存限制（GB）<input type="number" min={1} value={memoryGB} onChange={(event) => onMemory(Number(event.target.value))} /></label>
      </div>
      <label>
        启动命令
        <small>每行一个参数，与 docker run 镜像 参数1 参数2 相同；不要写成一整句 shell。留空则使用镜像自带命令。</small>
        <textarea value={value.command} onChange={(event) => set({ command: event.target.value })} rows={4} placeholder={'python\ntrain.py'} />
      </label>
      <label>
        入口（可选）
        <small>覆盖镜像 ENTRYPOINT，同样每行一个参数。留空则使用镜像入口。</small>
        <textarea value={value.entrypoint} onChange={(event) => set({ entrypoint: event.target.value })} rows={2} />
      </label>
      <label>
        环境变量
        <small>每行 KEY=VALUE，对应 docker run -e。</small>
        <textarea value={value.environment} onChange={(event) => set({ environment: event.target.value })} rows={3} placeholder="CUDA_MODULE_LOADING=LAZY" />
      </label>
      <div className="repeat-field">
        <div className="repeat-head">
          <span>数据盘挂载</span>
          <button type="button" className="text-button" onClick={() => set({ mounts: [...value.mounts, { source: '', target: '', readOnly: false }] })}>添加</button>
        </div>
        <small>宿主机路径必须在允许根下{allowedRoots.length ? `：${allowedRoots.join('、')}` : ''}，对应 docker run -v。</small>
        {value.mounts.map((item, index) => (
          <div className="kv-row mount-row" key={index}>
            <input value={item.source} onChange={(event) => setMount(index, { source: event.target.value })} placeholder="宿主机路径 /mnt/disk/exp" />
            <input value={item.target} onChange={(event) => setMount(index, { target: event.target.value })} placeholder="容器路径 /data" />
            <label className="checkbox-row"><input type="checkbox" checked={Boolean(item.readOnly)} onChange={(event) => setMount(index, { readOnly: event.target.checked })} />只读</label>
            <button type="button" className="text-button" onClick={() => set({ mounts: value.mounts.filter((_, i) => i !== index) })}>删除</button>
          </div>
        ))}
      </div>
      <div className="repeat-field">
        <div className="repeat-head">
          <span>端口发布</span>
          <button type="button" className="text-button" onClick={() => set({ ports: [...value.ports, { hostPort: 0, containerPort: 0 }] })}>添加</button>
        </div>
        <small>对应 docker run -p，不发布则留空。</small>
        {value.ports.map((item, index) => (
          <div className="kv-row port-row" key={index}>
            <input type="number" min={1} value={item.hostPort || ''} onChange={(event) => setPort(index, { hostPort: Number(event.target.value) })} placeholder="宿主机端口" />
            <input type="number" min={1} value={item.containerPort || ''} onChange={(event) => setPort(index, { containerPort: Number(event.target.value) })} placeholder="容器端口" />
            <select value={item.protocol || 'tcp'} onChange={(event) => setPort(index, { protocol: event.target.value })}>
              <option value="tcp">tcp</option>
              <option value="udp">udp</option>
            </select>
            <button type="button" className="text-button" onClick={() => set({ ports: value.ports.filter((_, i) => i !== index) })}>删除</button>
          </div>
        ))}
      </div>
      <div className="field-grid two">
        <label>共享内存（MB）<input type="number" min={0} value={value.shmMB} onChange={(event) => set({ shmMB: event.target.value })} placeholder="留空则使用 Docker 默认" /></label>
        <label>用户<input value={value.user} onChange={(event) => set({ user: event.target.value })} placeholder="留空则使用镜像默认用户" /></label>
      </div>
      <label>
        Docker 网络
        <small>对应 docker run --network，自定义网络可填。留空则使用默认。禁止 host、container、ns。</small>
        <input value={value.network} onChange={(event) => set({ network: event.target.value })} placeholder="例如 lab-net" />
      </label>
    </>
  )
}
