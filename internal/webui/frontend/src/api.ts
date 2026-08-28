import type { Availability, Health, Job, Topology } from './types'

async function request<T>(path: string): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  })
  if (!response.ok) {
    const detail = await response.text()
    throw new Error(detail || `${response.status} ${response.statusText}`)
  }
  return response.json() as Promise<T>
}

export const api = {
  health: () => request<Health>('/health'),
  topology: () => request<Topology>('/topology'),
  availability: () => request<Availability>('/resources/availability'),
  jobs: () => request<Job[]>('/jobs'),
}

