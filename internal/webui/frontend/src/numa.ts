import type { LogicalCPU, Topology } from './types'

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

export function resourceNuma(topology: Topology | undefined, gpuUuids: string[], cpuIds: number[]): number[] {
  const nodes = selectedGPUNuma(topology, gpuUuids)
  for (const id of cpuIds) {
    const cpu = topology?.cpus.find((item) => item.id === id)
    if (cpu) nodes.add(cpu.numaNode)
  }
  return [...nodes].sort((a, b) => a - b)
}
