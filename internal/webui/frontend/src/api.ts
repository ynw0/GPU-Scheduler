import type {
  AuditEvent, Availability, ComposeSource, Health, ImageInfo, Job, JobMetricSnapshot, JobPlan, JobSpec,
  Notification, Quota, ResourcePool, Team, Template, Topology, User,
} from './types'

export class APIError extends Error {
  constructor(public status: number, detail: string) { super(detail) }
}

function csrfToken(): string {
  const item = document.cookie.split('; ').find((cookie) => cookie.startsWith('gpu_scheduler_csrf='))
  return item ? decodeURIComponent(item.split('=')[1]) : ''
}

async function mutate<T>(path: string, method: string, value?: unknown, headers?: Record<string, string>): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken(), ...headers },
    body: value === undefined ? undefined : JSON.stringify(value),
  })
  if (!response.ok) throw new APIError(response.status, await response.text())
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

async function request<T>(path: string): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) {
    const detail = await response.text()
    throw new APIError(response.status, detail || `${response.status} ${response.statusText}`)
  }
  return response.json() as Promise<T>
}

export const api = {
  me: () => request<User>('/me'),
  login: async (username: string, password: string) => {
    const response = await fetch('/api/v1/auth/login', {
      method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
    if (!response.ok) throw new APIError(response.status, await response.text())
    return response.json() as Promise<User>
  },
  logout: async () => {
    const response = await fetch('/api/v1/auth/logout', {
      method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrfToken() },
    })
    if (!response.ok) throw new APIError(response.status, await response.text())
  },
  health: () => request<Health>('/health'),
  topology: () => request<Topology>('/topology'),
  availability: () => request<Availability>('/resources/availability'),
  jobs: () => request<Job[]>('/jobs'),
  job: (id: string) => request<Job>(`/jobs/${id}`),
  images: () => request<ImageInfo[]>('/images'),
  composeSources: () => request<ComposeSource[]>('/compose-sources'),
  templates: () => request<Template[]>('/templates'),
  teams: () => request<Team[]>('/teams'),
  users: () => request<User[]>('/users'),
  quotas: () => request<Quota[]>('/quotas'),
  resourcePools: () => request<ResourcePool[]>('/resource-pools'),
  jobMetrics: (id: string) => request<JobMetricSnapshot[]>(`/jobs/${id}/metrics`),
  auditEvents: () => request<AuditEvent[]>('/audit-events'),
  notifications: () => request<Notification[]>('/notifications'),
  planJob: (spec: JobSpec) => mutate<JobPlan>('/job-plans', 'POST', spec),
  createJob: (spec: JobSpec) => mutate<Job>('/jobs', 'POST', spec, { 'Idempotency-Key': crypto.randomUUID() }),
  jobAction: (id: string, action: 'start' | 'stop' | 'cancel' | 'retry') => mutate<Job>(`/jobs/${id}/actions/${action}`, 'POST'),
  createComposeSource: (value: { name: string; rootPath: string; entryFile: string }) => mutate<ComposeSource>('/compose-sources', 'POST', value),
  createTeam: (name: string) => mutate<Team>('/teams', 'POST', { name }),
  createUser: (value: { username: string; password: string; role: User['role']; email?: string; teamId?: string }) => mutate<User>('/users', 'POST', value),
  upsertQuota: (value: Quota) => mutate<Quota>('/quotas', 'POST', value),
  createResourcePool: (value: { name: string; cpuIds: number[]; gpuUuids: string[]; numaNodes: number[]; shared: boolean }) => mutate<ResourcePool>('/resource-pools', 'POST', value),
  createTemplate: (name: string, spec: JobSpec) => mutate<Template>('/templates', 'POST', { name, spec }),
  markNotificationRead: (id: string) => mutate<void>(`/notifications/${id}`, 'PATCH'),
}
