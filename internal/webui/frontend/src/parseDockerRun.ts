import type { ImageInfo, Mount, PortPublish } from './types'
import type { ImageFields } from './JobImageFields'

export type DockerRunParse = {
  error?: string
  fields: Partial<ImageFields>
  memoryGB?: number
  image?: string
  applied: string[]
  ignored: string[]
  unknown: string[]
}

const WRITE_ARG: Record<string, string> = {
  '-e': 'env', '--env': 'env',
  '-v': 'volume', '--volume': 'volume',
  '-p': 'publish', '--publish': 'publish',
  '-w': 'workdir', '--workdir': 'workdir',
  '--entrypoint': 'entrypoint',
  '-u': 'user', '--user': 'user',
  '--shm-size': 'shm',
  '-m': 'memory', '--memory': 'memory',
  '--network': 'network', '--net': 'network',
}

const IGNORE_ARG = new Set([
  '--gpus', '--runtime', '--cpuset-cpus', '--cpuset-mems', '--cpus', '--cpu-shares',
  '--name', '--restart', '--label',
])

const IGNORE_BOOL = new Set([
  '--init', '--detach', '--rm', '--interactive', '--tty', '-d', '-i', '-t',
])

const SHORT_ARG = new Set(['e', 'v', 'p', 'w', 'u', 'm'])
const SHORT_BOOL = new Set(['d', 'i', 't'])

function stripContinuations(input: string): string {
  return input.replace(/\\\r?\n/g, ' ').trim()
}

function tokenize(input: string): string[] {
  const tokens: string[] = []
  let i = 0
  while (i < input.length) {
    while (i < input.length && /\s/.test(input[i])) i += 1
    if (i >= input.length) break
    let quote = ''
    let token = ''
    while (i < input.length) {
      const c = input[i]
      if (quote) {
        if (c === quote) {
          quote = ''
          i += 1
          continue
        }
        if (c === '\\' && quote === '"' && i + 1 < input.length) {
          token += input[i + 1]
          i += 2
          continue
        }
        token += c
        i += 1
        continue
      }
      if (c === '"' || c === "'") {
        quote = c
        i += 1
        continue
      }
      if (/\s/.test(c)) break
      token += c
      i += 1
    }
    if (token) tokens.push(token)
  }
  return tokens
}

function skipPrefix(tokens: string[]): string[] | undefined {
  let i = 0
  if (tokens[i] === 'sudo') {
    i += 1
    while (i < tokens.length && tokens[i].startsWith('-')) {
      if (tokens[i] === '-u' || tokens[i] === '--user' || tokens[i] === '-g' || tokens[i] === '--group') {
        i += 2
        continue
      }
      i += 1
    }
  }
  if (tokens[i] === 'nvidia-docker') i += 1
  if (tokens[i] === 'docker') i += 1
  if (tokens[i] !== 'run') return undefined
  return tokens.slice(i + 1)
}

function splitEquals(token: string): [string, string | undefined] {
  const index = token.indexOf('=')
  if (index <= 0) return [token, undefined]
  return [token.slice(0, index), token.slice(index + 1)]
}

function parseBytes(value: string): number | undefined {
  const match = /^([0-9]+(?:\.[0-9]+)?)([bkmgtpe]i?b?)?$/i.exec(value.trim())
  if (!match) return undefined
  const amount = Number(match[1])
  if (!Number.isFinite(amount)) return undefined
  const unit = (match[2] || 'b').toLowerCase()
  const letter = unit[0] as 'b' | 'k' | 'm' | 'g' | 't' | 'p' | 'e'
  const exp = { b: 0, k: 1, m: 2, g: 3, t: 4, p: 5, e: 6 }[letter]
  if (exp == null) return undefined
  return amount * 1024 ** exp
}

function parseShmMB(value: string): string | undefined {
  const bytes = parseBytes(value)
  if (bytes == null) return undefined
  return String(Math.max(0, Math.round(bytes / (1024 * 1024))))
}

function forbiddenNetwork(value: string): string | undefined {
  const name = value.trim().toLowerCase()
  if (name === 'host' || name.startsWith('container:') || name.startsWith('ns:')) {
    return `拒绝网络 ${value.trim()}`
  }
}

function parseMemoryGB(value: string): number | undefined {
  const bytes = parseBytes(value)
  if (bytes == null) return undefined
  return Math.max(1, Math.round(bytes / (1024 ** 3)))
}

function parseEntrypoint(value: string): string {
  const trimmed = value.trim()
  if (trimmed.startsWith('[')) {
    try {
      const parsed = JSON.parse(trimmed) as unknown
      if (Array.isArray(parsed) && parsed.every((item) => typeof item === 'string')) {
        return parsed.join('\n')
      }
    } catch {
      return trimmed
    }
  }
  return trimmed
}

function parseVolume(value: string): Mount | { reject: string } | { unknown: string } {
  const parts = value.split(':')
  if (parts.length < 2) return { unknown: `-v ${value}` }
  const source = parts[0]
  const target = parts[1]
  const mode = parts.slice(2).join(',')
  if (!source.startsWith('/') && !source.startsWith('.')) return { unknown: `-v ${value}` }
  if (source === '/' || source === '/var/run/docker.sock') {
    return { reject: `拒绝挂载 ${source}` }
  }
  return { source, target, readOnly: /(?:^|,)ro(?:,|$)/.test(mode) }
}

function parsePublish(value: string): PortPublish | undefined {
  let spec = value
  let protocol = 'tcp'
  const proto = spec.lastIndexOf('/')
  if (proto >= 0) {
    protocol = spec.slice(proto + 1).toLowerCase() || 'tcp'
    spec = spec.slice(0, proto)
  }
  const parts = spec.split(':')
  if (parts.length === 1) {
    const port = Number(parts[0])
    if (!port) return undefined
    return { hostPort: port, containerPort: port, protocol }
  }
  if (parts.length === 2) {
    const hostPort = Number(parts[0])
    const containerPort = Number(parts[1])
    if (!hostPort || !containerPort) return undefined
    return { hostPort, containerPort, protocol }
  }
  if (parts.length >= 3) {
    const hostPort = Number(parts[parts.length - 2])
    const containerPort = Number(parts[parts.length - 1])
    if (!hostPort || !containerPort) return undefined
    return { hostPort, containerPort, protocol }
  }
  return undefined
}

function parseMount(value: string): Mount | { reject: string } | { unknown: string } {
  const fields: Record<string, string> = {}
  for (const part of value.split(',')) {
    const index = part.indexOf('=')
    if (index <= 0) continue
    fields[part.slice(0, index).trim()] = part.slice(index + 1).trim()
  }
  const type = fields.type || 'volume'
  if (type !== 'bind') return { reject: `--mount 仅支持 bind，收到 ${type}` }
  const source = fields.source || fields.src || ''
  const target = fields.target || fields.dst || fields.destination || ''
  if (!source || !target) return { unknown: `--mount ${value}` }
  if (source === '/' || source === '/var/run/docker.sock') {
    return { reject: `拒绝挂载 ${source}` }
  }
  return { source, target, readOnly: fields.readonly === 'true' || fields.readonly === '1' }
}

function isOption(token: string): boolean {
  return token.startsWith('-') && token !== '-'
}

export function parseDockerRun(input: string): DockerRunParse {
  const empty = (): DockerRunParse => ({ fields: {}, applied: [], ignored: [], unknown: [] })
  const source = stripContinuations(input)
  if (!source) return { ...empty(), error: '请粘贴 docker run 命令' }
  const tokens = tokenize(source)
  const rest = skipPrefix(tokens)
  if (!rest) return { ...empty(), error: '需要以 docker run 开头' }

  const applied: string[] = []
  const ignored: string[] = []
  const unknown: string[] = []
  const rejected: string[] = []
  const env: string[] = []
  const mounts: Mount[] = []
  const ports: PortPublish[] = []
  const fields: Partial<ImageFields> = {}
  let memoryGB: number | undefined
  let image: string | undefined
  const command: string[] = []

  const noteApplied = (label: string) => {
    if (!applied.includes(label)) applied.push(label)
  }
  const noteIgnored = (flag: string) => {
    if (!ignored.includes(flag)) ignored.push(flag)
  }

  const take = (i: number, attached?: string): { value: string; next: number } | undefined => {
    if (attached != null && attached !== '') return { value: attached, next: i }
    if (i >= rest.length || isOption(rest[i])) return undefined
    return { value: rest[i], next: i + 1 }
  }

  let i = 0
  while (i < rest.length) {
    const token = rest[i]
    if (!isOption(token)) {
      image = token
      command.push(...rest.slice(i + 1))
      break
    }
    i += 1
    const [rawFlag, attached] = splitEquals(token)
    let flag = rawFlag
    let inline = attached

    if (/^-[^-]/.test(flag) && flag.length > 2) {
      const letters = flag.slice(1)
      if ([...letters].every((letter) => SHORT_BOOL.has(letter))) {
        for (const letter of letters) noteIgnored(`-${letter}`)
        continue
      }
      const first = letters[0]
      if (SHORT_ARG.has(first)) {
        flag = `-${first}`
        inline = letters.slice(1) || inline
      }
    }

    if (flag === '--privileged') {
      rejected.push('--privileged')
      continue
    }
    if (flag === '--cap-add' || flag === '--cap-drop') {
      const got = take(i, inline)
      if (got) i = got.next
      rejected.push(flag)
      continue
    }
    if (flag === '--pid' || flag === '--ipc') {
      const got = take(i, inline)
      if (!got) {
        unknown.push(flag)
        continue
      }
      i = got.next
      if (got.value === 'host') rejected.push(`${flag}=host`)
      else unknown.push(`${flag} ${got.value}`)
      continue
    }
    if (flag === '--device') {
      const got = take(i, inline)
      if (!got) {
        unknown.push(flag)
        continue
      }
      i = got.next
      if (/nvidia/i.test(got.value)) noteIgnored('--device')
      else rejected.push(`--device ${got.value}`)
      continue
    }
    if (flag === '--mount') {
      const got = take(i, inline)
      if (!got) {
        unknown.push(flag)
        continue
      }
      i = got.next
      const parsed = parseMount(got.value)
      if ('reject' in parsed) {
        rejected.push(parsed.reject)
        continue
      }
      if ('unknown' in parsed) {
        unknown.push(parsed.unknown)
        continue
      }
      mounts.push(parsed)
      noteApplied('挂载')
      continue
    }
    if (IGNORE_BOOL.has(flag)) {
      noteIgnored(flag)
      continue
    }
    if (IGNORE_ARG.has(flag)) {
      const got = take(i, inline)
      if (got) i = got.next
      noteIgnored(flag)
      continue
    }
    const write = WRITE_ARG[flag]
    if (write) {
      const got = take(i, inline)
      if (!got) {
        unknown.push(flag)
        continue
      }
      i = got.next
      if (write === 'env') {
        env.push(got.value.includes('=') ? got.value : `${got.value}=`)
        noteApplied('环境变量')
      } else if (write === 'volume') {
        const parsed = parseVolume(got.value)
        if ('reject' in parsed) rejected.push(parsed.reject)
        else if ('unknown' in parsed) unknown.push(parsed.unknown)
        else {
          mounts.push(parsed)
          noteApplied('挂载')
        }
      } else if (write === 'publish') {
        const port = parsePublish(got.value)
        if (!port) unknown.push(`-p ${got.value}`)
        else {
          ports.push(port)
          noteApplied('端口')
        }
      } else if (write === 'workdir') {
        fields.workingDirectory = got.value
        noteApplied('工作目录')
      } else if (write === 'entrypoint') {
        fields.entrypoint = parseEntrypoint(got.value)
        noteApplied('入口')
      } else if (write === 'user') {
        fields.user = got.value
        noteApplied('用户')
      } else if (write === 'shm') {
        const mb = parseShmMB(got.value)
        if (mb == null) unknown.push(`--shm-size ${got.value}`)
        else {
          fields.shmMB = mb
          noteApplied('共享内存')
        }
      } else if (write === 'memory') {
        const gb = parseMemoryGB(got.value)
        if (gb == null) unknown.push(`-m ${got.value}`)
        else {
          memoryGB = gb
          noteApplied('内存限制')
        }
      } else if (write === 'network') {
        const reason = forbiddenNetwork(got.value)
        if (reason) rejected.push(reason)
        else {
          fields.network = got.value
          noteApplied('网络')
        }
      }
      continue
    }
    unknown.push(inline != null ? `${flag}=${inline}` : flag)
  }

  if (rejected.length) {
    return { fields: {}, applied: [], ignored: [], unknown: [], error: rejected.join('；') }
  }
  if (!image) {
    return { fields: {}, applied: [], ignored, unknown, error: '未能识别镜像名' }
  }

  if (env.length) fields.environment = env.join('\n')
  if (mounts.length) fields.mounts = mounts
  if (ports.length) fields.ports = ports
  if (command.length) fields.command = command.join('\n')
  applied.unshift('镜像')

  return { fields, memoryGB, image, applied, ignored, unknown }
}

export function matchCatalogImage(parsed: string, images: ImageInfo[]): string | undefined {
  const candidates = parsed.includes(':') ? [parsed] : [parsed, `${parsed}:latest`]
  for (const item of images) {
    const tag = `${item.repository}:${item.tag}`
    if (candidates.includes(tag) || candidates.includes(item.repository)) return tag
  }
  return undefined
}
