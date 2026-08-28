import type { Availability, Health, Job, Topology, User } from './types'

export class APIError extends Error {
  constructor(public status: number, detail: string) { super(detail) }
}

function csrfToken(): string {
  const item = document.cookie.split('; ').find((cookie) => cookie.startsWith('gpu_scheduler_csrf='))
  return item ? decodeURIComponent(item.split('=')[1]) : ''
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
}
