import type { GPU, ResourceRequest, Topology } from './types'

export function gpuDeviceLabel(topology: Topology | undefined, uuid: string): string {
  const gpu = topology?.gpus.find((item) => item.uuid === uuid)
  if (!gpu) return uuid.slice(0, 8)
  if (gpu.parentUuid) {
    return `GPU ${gpu.index} · ${gpu.profile || 'MIG'} · ${uuid.slice(0, 8)}`
  }
  return `GPU ${gpu.index}`
}

export function gpuRequestLabel(resources: ResourceRequest): string {
  if (resources.gpuKind === 'mig') {
    const profile = resources.migProfile || 'MIG'
    if (resources.gpuMode === 'manual') {
      const uuid = (resources.gpuUuids ?? [])[0]
      return uuid ? `MIG ${profile} · ${uuid.slice(0, 8)}` : `MIG ${profile} ×1`
    }
    return `MIG ${profile} ×1`
  }
  if (resources.gpuMode === 'manual') return `${(resources.gpuUuids ?? []).length} 张整卡`
  return `${resources.gpuCount} 张（自动）`
}

export function wholeGPUs(gpus: GPU[]): GPU[] {
  return gpus.filter((gpu) => !gpu.parentUuid && !gpu.migEnabled)
}

export function migInstances(gpus: GPU[]): GPU[] {
  return gpus.filter((gpu) => Boolean(gpu.parentUuid))
}

export function existingMigProfiles(gpus: GPU[]): string[] {
  const names = new Set<string>()
  for (const gpu of migInstances(gpus)) {
    if (gpu.profile) names.add(gpu.profile)
  }
  return [...names].sort()
}

export function idleMigProfiles(gpus: GPU[], freeGPUUUIDs: Set<string>): string[] {
  const names = new Set<string>()
  for (const gpu of migInstances(gpus)) {
    if (gpu.profile && freeGPUUUIDs.has(gpu.uuid)) names.add(gpu.profile)
  }
  return [...names].sort()
}
