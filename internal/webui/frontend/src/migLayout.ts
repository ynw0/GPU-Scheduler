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
    const next = (counts.get(profile.name) ?? 0) + 1
    if (next > profile.maxCount) continue
    if (usedG + profile.g > MIG_TOTAL_G) continue
    if (usedMemoryGB + profile.memoryGB > catalog.memoryGB) continue
    selectable.add(profile.name)
  }
  return {
    usedG,
    remainG: MIG_TOTAL_G - usedG,
    usedMemoryGB,
    remainMemGB: catalog.memoryGB - usedMemoryGB,
    selectable,
  }
}
