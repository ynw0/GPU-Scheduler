export type MigProfile = {
  name: string
  g: number
  memoryGB: number
  maxCount: number
}

export type MigCatalog = {
  family: string
  memoryGB: number
  profiles: MigProfile[]
}

const family80: MigCatalog = {
  family: '80g',
  memoryGB: 80,
  profiles: [
    { name: '7g.80gb', g: 7, memoryGB: 80, maxCount: 1 },
    { name: '4g.40gb', g: 4, memoryGB: 40, maxCount: 1 },
    { name: '3g.40gb', g: 3, memoryGB: 40, maxCount: 2 },
    { name: '2g.20gb', g: 2, memoryGB: 20, maxCount: 3 },
    { name: '1g.10gb', g: 1, memoryGB: 10, maxCount: 7 },
  ],
}

const family40: MigCatalog = {
  family: '40g',
  memoryGB: 40,
  profiles: [
    { name: '7g.40gb', g: 7, memoryGB: 40, maxCount: 1 },
    { name: '4g.20gb', g: 4, memoryGB: 20, maxCount: 1 },
    { name: '3g.20gb', g: 3, memoryGB: 20, maxCount: 2 },
    { name: '2g.10gb', g: 2, memoryGB: 10, maxCount: 3 },
    { name: '1g.5gb', g: 1, memoryGB: 5, maxCount: 7 },
  ],
}

export const MIG_TOTAL_G = 7

export function catalogForMemory(memoryBytes: number): MigCatalog {
  return memoryBytes >= 60 * 1024 * 1024 * 1024 ? family80 : family40
}

export function evaluateMig(catalog: MigCatalog, slices: string[]): {
  usedG: number
  remainG: number
  usedMemoryGB: number
  remainMemGB: number
  selectable: Set<string>
  counts: Map<string, number>
} {
  const counts = new Map<string, number>()
  let usedG = 0
  let usedMemoryGB = 0
  for (const name of slices) {
    const profile = catalog.profiles.find((item) => item.name === name)
    if (!profile) continue
    counts.set(name, (counts.get(name) ?? 0) + 1)
    usedG += profile.g
    usedMemoryGB += profile.memoryGB
  }
  const selectable = new Set<string>()
  for (const profile of catalog.profiles) {
    if (maxCountFor(catalog, slices, profile.name) > 0) selectable.add(profile.name)
  }
  return {
    usedG,
    remainG: MIG_TOTAL_G - usedG,
    usedMemoryGB,
    remainMemGB: catalog.memoryGB - usedMemoryGB,
    selectable,
    counts,
  }
}

export function maxCountFor(catalog: MigCatalog, slices: string[], profileName: string): number {
  const profile = catalog.profiles.find((item) => item.name === profileName)
  if (!profile) return 0
  const others = slices.filter((name) => name !== profileName)
  let max = 0
  for (let count = 1; count <= profile.maxCount; count += 1) {
    const trial = [...others, ...Array(count).fill(profileName)]
    if (!fits(catalog, trial)) break
    max = count
  }
  return max
}

function fits(catalog: MigCatalog, slices: string[]): boolean {
  const counts = new Map<string, number>()
  let usedG = 0
  let usedMemoryGB = 0
  for (const name of slices) {
    const profile = catalog.profiles.find((item) => item.name === name)
    if (!profile) return false
    const next = (counts.get(name) ?? 0) + 1
    if (next > profile.maxCount) return false
    counts.set(name, next)
    usedG += profile.g
    usedMemoryGB += profile.memoryGB
    if (usedG > MIG_TOTAL_G || usedMemoryGB > catalog.memoryGB) return false
  }
  return true
}

export function setProfileCount(slices: string[], profileName: string, count: number): string[] {
  const others = slices.filter((name) => name !== profileName)
  return [...others, ...Array(Math.max(0, count)).fill(profileName)]
}
