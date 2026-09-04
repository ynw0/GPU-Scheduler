import type { LogicalCPU, Topology } from './types'

export const busyCPUPercent = 50
export const warmCPUPercent = 15

export function selectedGPUNuma(topology: Topology | undefined, gpuUuids: string[]): Set<number> {
  const nodes = new Set<number>()
  for (const gpu of topology?.gpus ?? []) {
    if (gpuUuids.includes(gpu.uuid)) nodes.add(gpu.numaNode)
  }
  return nodes
}

export function cpuSelectable(cpu: LogicalCPU, numa: Set<number>): boolean {
  if (!cpu.allocatable) return false
  if (numa.size === 0) return true
  return numa.has(cpu.numaNode)
}

export function cpuPercentOf(byId: Record<string, number> | undefined, id: number): number {
  if (!byId) return 0
  return byId[id] ?? byId[String(id)] ?? 0
}

export function threadTone(allocatable: boolean, dockerFree: boolean, percent: number): 'unavailable' | 'free' | 'allocated' | 'hot' {
  if (!allocatable) return 'unavailable'
  if (percent >= busyCPUPercent) return 'hot'
  if (!dockerFree || percent >= warmCPUPercent) return 'allocated'
  return 'free'
}

export function resourceNuma(topology: Topology | undefined, gpuUuids: string[], cpuIds: number[]): number[] {
  const nodes = selectedGPUNuma(topology, gpuUuids)
  for (const id of cpuIds) {
    const cpu = topology?.cpus.find((item) => item.id === id)
    if (cpu) nodes.add(cpu.numaNode)
  }
  return [...nodes].sort((a, b) => a - b)
}
