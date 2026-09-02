import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { catalogForMemory, evaluateMig } from './migLayout'
import type { GPU, MIGLayout } from './types'

export function MigSettings() {
  const queryClient = useQueryClient()
  const topology = useQuery({ queryKey: ['topology'], queryFn: api.topology })
  const layouts = useQuery({ queryKey: ['mig-layouts'], queryFn: api.migLayouts })
  const physical = (topology.data?.gpus ?? []).filter((gpu) => !gpu.parentUuid)
  const saved = new Map((layouts.data ?? []).map((item) => [item.gpuUuid, item]))

  return (
    <div className="mig-settings">
      <p className="field-hint">按卡切分。点选档位时按剩余 7g / 显存即时点亮可选项；例如点了 3 个 2g，就只剩 1g 可点。该卡有任务或容器时不能应用。应用会重置该卡，不必重启整机。</p>
      {physical.map((gpu) => (
        <MigCard key={gpu.uuid} gpu={gpu} children={(topology.data?.gpus ?? []).filter((item) => item.parentUuid === gpu.uuid)} saved={saved.get(gpu.uuid)} onApplied={() => {
          void queryClient.invalidateQueries({ queryKey: ['mig-layouts'] })
          void queryClient.invalidateQueries({ queryKey: ['topology'] })
          void queryClient.invalidateQueries({ queryKey: ['availability'] })
        }} />
      ))}
      {!physical.length && <div className="empty-row">没有可配置的 GPU</div>}
    </div>
  )
}

function MigCard({ gpu, children, saved, onApplied }: {
  gpu: GPU
  children: GPU[]
  saved?: MIGLayout
  onApplied: () => void
}) {
  const catalog = useMemo(() => catalogForMemory(gpu.memoryBytes), [gpu.memoryBytes])
  const [slices, setSlices] = useState<string[]>(saved?.profiles ?? [])
  useEffect(() => {
    setSlices(saved?.profiles ?? [])
  }, [gpu.uuid, saved?.enabled, (saved?.profiles ?? []).join(',')])
  const plan = evaluateMig(catalog, slices)
  const apply = useMutation({
    mutationFn: (enabled: boolean) => api.applyMigLayout(gpu.uuid, { enabled, profiles: enabled ? slices : [] }),
    onSuccess: onApplied,
  })
  const live = gpu.migEnabled ? (children.length ? children.map((item) => item.profile || item.uuid.slice(0, 8)).join(' + ') : 'MIG 已开，尚无实例') : '整卡'

  return (
    <div className="mig-card">
      <div className="mig-card-head">
        <strong>GPU {gpu.index}</strong>
        <span>{gpu.name}</span>
        <small>当前：{live}</small>
      </div>
      <div className="mig-remain">剩余 {plan.remainG} / 7 g · 显存 {plan.remainMemGB} / {catalog.memoryGB} GB</div>
      <div className="resource-choice-grid mig-profile-grid">
        {catalog.profiles.map((profile) => {
          const on = plan.selectable.has(profile.name)
          return (
            <button
              key={profile.name}
              type="button"
              className={on ? 'resource-choice' : 'resource-choice disabled'}
              disabled={!on}
              onClick={() => on && setSlices([...slices, profile.name])}
            >
              <span>{profile.name}</span>
              <small>{profile.g}g · {profile.memoryGB}GB · 最多 {profile.maxCount}</small>
            </button>
          )
        })}
      </div>
      <div className="mig-picked">
        {slices.length ? slices.map((name, index) => (
          <button type="button" className="mig-chip" key={`${name}-${index}`} onClick={() => setSlices(slices.filter((_, i) => i !== index))}>{name} ×</button>
        )) : <span className="muted">未选档位 = 保持或恢复整卡</span>}
      </div>
      <div className="mig-actions">
        <button type="button" className="text-button" onClick={() => setSlices(slices.slice(0, -1))} disabled={!slices.length}>撤销一片</button>
        <button type="button" className="text-button" onClick={() => setSlices([])}>清空</button>
        <button type="button" className="secondary-action" disabled={apply.isPending} onClick={() => apply.mutate(false)}>恢复整卡</button>
        <button type="button" className="primary-action" disabled={apply.isPending || !slices.length} onClick={() => apply.mutate(true)}>
          {apply.isPending ? '正在应用…' : '应用切分'}
        </button>
      </div>
      {apply.error && <div className="plan-conflict">{String(apply.error.message)}</div>}
    </div>
  )
}
