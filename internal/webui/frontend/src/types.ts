export interface Health {
  status: 'ok' | 'degraded'
  version: string
  agent: 'online' | 'offline'
}

export interface User {
  id: string
  username: string
  role: 'Viewer' | 'User' | 'TeamOperator' | 'Admin' | 'Auditor'
  teamId?: string
}

export interface LogicalCPU {
  id: number
  coreId: number
  socketId: number
  numaNode: number
  siblings: number[]
  allocatable: boolean
}

export interface NUMANode {
  id: number
  memoryBytes: number
  cpuIds: number[]
}

export interface GPU {
  uuid: string
  index: number
  name: string
  pciBusId: string
  numaNode: number
  memoryBytes: number
  healthy: boolean
  utilization?: number
  memoryUsedBytes?: number
  temperatureCelsius?: number
  powerWatts?: number
  allocation?: { jobId: string; jobName: string }
}

export interface Topology {
  hostname: string
  cpus: LogicalCPU[]
  numaNodes: NUMANode[]
  gpus: GPU[]
  observedAt: string
}

export interface Availability {
  cpuIds: number[]
  gpuUuids: string[]
  memoryByNuma: Record<string, number>
}

export interface Job {
  id: string
  name: string
  state: string
  priority: 'P0' | 'P1' | 'P2' | 'P3'
  ownerId: string
  createdAt: string
  updatedAt: string
}
