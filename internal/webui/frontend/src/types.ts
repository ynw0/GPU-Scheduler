export interface Health {
  status: 'ok' | 'degraded'
  version: string
  agent: 'online' | 'offline'
  warnings: string[]
}

export interface User {
  id: string
  username: string
  role: 'Viewer' | 'User' | 'TeamOperator' | 'Admin' | 'Auditor'
  teamId?: string
  email?: string
  disabled?: boolean
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
  spec?: JobSpec
  allocations?: Allocation[]
  attempt?: JobAttempt
  heartbeat?: {
    attemptId?: string
    serviceName?: string
    sequence?: number
    phase: string
    epoch?: number
    step?: number
    progress?: number
    reportedAt: string
    ageSeconds: number
  }
  runtime?: {
    observedAt: string
    dockerHealth: string
    cpuActivity: boolean
    gpuActivity: boolean
    oomUnder: boolean
  }
  lastError?: string
}

export interface Allocation {
  id: string
  jobId: string
  serviceName?: string
  resourceType: string
  resourceId: string
  amountBytes?: number
  state: string
}

export interface JobAttempt {
  id: string
  state: string
  kind?: string
  containerIds?: Record<string, string>
}

export interface ImageInfo {
  id: string
  repository: string
  tag: string
  sizeBytes: number
  createdAt: string
}

export interface ResourceRequest {
  cpuMode: 'auto-cores' | 'manual-threads'
  cpuCount: number
  cpuIds?: number[]
  gpuMode: 'auto' | 'manual'
  gpuCount: number
  gpuUuids?: string[]
  memoryBytes: number
  numaPolicy: 'strict'
}

export interface JobSpec {
  name: string
  priority: 'P0' | 'P1' | 'P2' | 'P3'
  workload: ImageWorkload | ComposeWorkload
  resources: ResourceRequest
  resourcePoolId?: string
  stopGraceSeconds: number
}

export interface Mount {
  source: string
  target: string
  readOnly?: boolean
}

export interface PortPublish {
  hostPort: number
  containerPort: number
  protocol?: string
}

export interface ImageWorkload {
  kind: 'image'
  image: string
  command?: string[]
  entrypoint?: string[]
  workingDirectory?: string
  environment?: Record<string, string>
  mounts?: Mount[]
  user?: string
  shmSizeBytes?: number
  publishedPorts?: PortPublish[]
}

export interface ComposeWorkload {
  kind: 'compose'
  composeSourceId: string
  services: Array<{ name: string; class: 'training' | 'auxiliary'; resources: ResourceRequest }>
}

export interface JobPlan {
  schedulable: boolean
  cpuIds: number[]
  gpuUuids: string[]
  numaNodes: number[]
  conflicts: string[]
  warnings: string[]
}

export interface ComposeSource {
  id: string
  name: string
  rootPath: string
  entryFile: string
  enabled: boolean
}

export interface Team {
  id: string
  name: string
  createdAt: string
}

export interface Quota {
  id: string
  scopeType: 'team' | 'user'
  scopeId: string
  gpuLimit: number
  cpuThreadLimit: number
  memoryBytesLimit: number
}

export interface Template {
  id: string
  name: string
  ownerId: string
  spec: JobSpec
  updatedAt: string
}

export interface Notification {
  id: string
  severity: string
  title: string
  body: string
  readAt?: string
  createdAt: string
}

export interface ResourcePool {
  id: string
  name: string
  cpuIds: number[]
  gpuUuids: string[]
  numaNodes: number[]
  shared: boolean
  createdAt: string
}

export interface JobMetricSnapshot {
  sampledAt: string
  cpuPercentById?: Record<string, number>
  memoryUsedBytes: number
  memoryLimitBytes: number
  memoryFailCount: number
  oomUnder: boolean
  blockIoReadBytes: number
  blockIoWriteBytes: number
  gpus?: Array<{
    uuid: string
    utilizationPercent: number
    memoryUsedBytes: number
    memoryTotalBytes: number
    temperatureCelsius: number
    powerWatts: number
  }>
}

export interface HostContainer {
  containerId: string
  name: string
  image: string
  state: string
  cpuIds?: number[]
  gpuUuids?: string[]
  cpuUnbounded: boolean
  gpuUnbounded: boolean
  training: boolean
  trainingGpuUuids: string[]
  memoryUsedBytes?: number
  memoryLimitBytes?: number
  gpus?: JobMetricSnapshot['gpus']
  dockerHealth?: string
  cpuActivity?: boolean
  gpuActivity?: boolean
  cpuPercent?: number
  oomUnder?: boolean
  managedJobId?: string
  jobName?: string
  priority: Job['priority']
  priorityLocked: boolean
  lastTrainingAt?: string
  firstSeenAt: string
  silentSeconds: number
}

export interface AuditEvent {
  id: string
  actorUserId?: string
  action: string
  targetType: string
  targetId: string
  detail?: string
  createdAt: string
}
