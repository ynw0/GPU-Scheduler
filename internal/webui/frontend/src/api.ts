import type {
  AuditEvent, Availability, ComposeSource, Health, ImageInfo, Job, JobAction, JobMetricSnapshot, JobPlan, JobSpec,
  MIGLayout, Notification, Quota, ResourcePool, Team, Template, Topology, User, HostContainer,
} from './types'

export class APIError extends Error {
  constructor(public status: number, detail: string) { super(detail) }
}

function csrfToken(): string {
  const item = document.cookie.split('; ').find((cookie) => cookie.startsWith('gpu_scheduler_csrf='))
  return item ? decodeURIComponent(item.split('=')[1]) : ''
}

async function problem(response: Response): Promise<APIError> {
  const text = await response.text()
  try {
    const body = JSON.parse(text) as { detail?: string; title?: string }
    return new APIError(response.status, body.detail || body.title || text)
  } catch {
    return new APIError(response.status, text || `${response.status} ${response.statusText}`)
  }
}

async function mutate<T>(path: string, method: string, value?: unknown, headers?: Record<string, string>): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken(), ...headers },
    body: value === undefined ? undefined : JSON.stringify(value),
  })
  if (!response.ok) throw await problem(response)
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

async function request<T>(path: string): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) throw await problem(response)
  return response.json() as Promise<T>
}

export const api = {
  me: () => request<User>('/me'),
  login: async (username: string, password: string) => {
    const response = await fetch('/api/v1/auth/login', {
      method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
    if (!response.ok) throw await problem(response)
    return response.json() as Promise<User>
  },
  logout: async () => {
    const response = await fetch('/api/v1/auth/logout', {
      method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': csrfToken() },
    })
    if (!response.ok) throw await problem(response)
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
  jobAction: (id: string, action: JobAction) => mutate<Job>(`/jobs/${id}/actions/${action}`, 'POST'),
  createComposeSource: (value: { name: string; rootPath: string; entryFile: string }) => mutate<ComposeSource>('/compose-sources', 'POST', value),
  createComposeFromPath: (value: { name: string; filePath: string }) => mutate<ComposeSource>('/compose-sources/from-path', 'POST', value),
  uploadCompose: (value: { name: string; fileName: string; content: string }) => mutate<ComposeSource>('/compose-sources/upload', 'POST', value),
  updateComposeSource: (id: string, value: { name: string; rootPath: string; entryFile: string; enabled: boolean }) =>
    mutate<void>(`/compose-sources/${id}`, 'PATCH', value),
  hostContainers: () => request<HostContainer[]>('/unmanaged-containers'),
  patchHostContainer: (id: string, priority: Job['priority']) => mutate<void>(`/unmanaged-containers/${id}`, 'PATCH', { priority }),
  stopHostContainer: (id: string) => mutate<void>(`/unmanaged-containers/${id}/stop`, 'POST'),
  pauseHostContainer: (id: string) => mutate<void>(`/unmanaged-containers/${id}/pause`, 'POST'),
  unpauseHostContainer: (id: string) => mutate<void>(`/unmanaged-containers/${id}/unpause`, 'POST'),
  changePassword: (currentPassword: string, newPassword: string) => mutate<void>('/me/password', 'POST', { currentPassword, newPassword }),
  createTeam: (name: string) => mutate<Team>('/teams', 'POST', { name }),
  createUser: (value: { username: string; password: string; role: User['role']; email?: string; teamId?: string }) => mutate<User>('/users', 'POST', value),
  updateUser: (id: string, value: { role: User['role']; teamId: string; disabled: boolean; email?: string }) => mutate<void>(`/users/${id}`, 'PATCH', value),
  resetUserPassword: (id: string, newPassword: string) => mutate<void>(`/users/${id}/password`, 'POST', { newPassword }),
  upsertQuota: (value: Quota) => mutate<Quota>('/quotas', 'POST', value),
  createResourcePool: (value: { name: string; cpuIds: number[]; gpuUuids: string[]; numaNodes: number[]; shared: boolean }) => mutate<ResourcePool>('/resource-pools', 'POST', value),
  createTemplate: (name: string, spec: JobSpec) => mutate<Template>('/templates', 'POST', { name, spec }),
  updateTemplate: (id: string, name: string, spec: JobSpec) => mutate<void>(`/templates/${id}`, 'PATCH', { name, spec }),
  platform: () => request<{ allowedMountRoots: string[] }>('/platform'),
  migLayouts: () => request<MIGLayout[]>('/mig/layouts'),
  applyMigLayout: (gpuUuid: string, value: { enabled: boolean; profiles: string[] }) =>
    mutate<MIGLayout>(`/mig/layouts/${encodeURIComponent(gpuUuid)}`, 'PUT', value),
  rebootHost: () => mutate<void>('/host/reboot', 'POST'),
  markNotificationRead: (id: string) => mutate<void>(`/notifications/${id}`, 'PATCH'),
  markAllNotificationsRead: () => mutate<void>('/notifications/read-all', 'POST'),
  clearNotifications: () => mutate<void>('/notifications', 'DELETE'),
}
