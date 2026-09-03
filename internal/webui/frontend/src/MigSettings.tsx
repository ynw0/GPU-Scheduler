import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { catalogForMemory, evaluateMig, maxCountFor, setProfileCount } from './migLayout'
import type { GPU, MIGLayout } from './types'

function sortedProfiles(values: string[]): string[] {
  return [...values].filter(Boolean).sort()
}

function sameProfiles(live: string[], want: string[]): boolean {
  const left = sortedProfiles(live)
  const right = sortedProfiles(want)
  return left.length === right.length && left.every((name, index) => name === right[index])
}

function layoutVisible(gpu: GPU, children: GPU[], enabled: boolean, profiles: string[]): boolean {
  if (!enabled) return !gpu.migEnabled && children.length === 0
  if (!gpu.migEnabled) return false
  return sameProfiles(children.map((item) => item.profile || ''), profiles)
}

export function MigSettings() {
  const queryClient = useQueryClient()
  const topology = useQuery({ queryKey: ['topology'], queryFn: api.topology })
  const layouts = useQuery({ queryKey: ['mig-layouts'], queryFn: api.migLayouts })
  const reboot = useMutation({ mutationFn: api.rebootHost })
  const [armed, setArmed] = useState(false)
  const physical = (topology.data?.gpus ?? []).filter((gpu) => !gpu.parentUuid)
  const saved = new Map((layouts.data ?? []).map((item) => [item.gpuUuid, item]))

  return (
    <div className="mig-settings">
      <p className="field-hint">按卡切分。亮着的 1g/2g/3g/4g/7g 可直接选片数；例如 2g 选 ×3 后，只剩 1g 可再选 ×1。该卡有任务或容器时不能应用。切换 MIG 必须重启本机。</p>
      <div className="mig-actions">
        {!armed && (
          <button type="button" className="secondary-action" disabled={reboot.isPending} onClick={() => setArmed(true)}>重启本机</button>
        )}
        {armed && (
          <>
            <span className="field-hint">将重启整台机器，所有任务会中断。</span>
            <button type="button" className="text-button" onClick={() => setArmed(false)}>取消</button>
            <button type="button" className="primary-action" disabled={reboot.isPending} onClick={() => reboot.mutate()}>
              {reboot.isPending ? '正在重启…' : '确认重启'}
            </button>
          </>
        )}
      </div>
      {reboot.error && <div className="plan-conflict">{String(reboot.error.message)}</div>}
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
  const queryClient = useQueryClient()
  const catalog = useMemo(() => catalogForMemory(gpu.memoryBytes), [gpu.memoryBytes])
  const [slices, setSlices] = useState<string[]>(saved?.profiles ?? [])
  const [expect, setExpect] = useState<{ enabled: boolean; profiles: string[] }>()
  const [needsReboot, setNeedsReboot] = useState(false)
  useEffect(() => {
    setSlices(saved?.profiles ?? [])
  }, [gpu.uuid, saved?.enabled, (saved?.profiles ?? []).join(',')])
  const plan = evaluateMig(catalog, slices)
  const apply = useMutation({
    mutationFn: (enabled: boolean) => api.applyMigLayout(gpu.uuid, { enabled, profiles: enabled ? slices : [] }),
    onMutate: () => { setExpect(undefined); setNeedsReboot(false) },
    onSuccess: (layout) => {
      setExpect({ enabled: layout.enabled, profiles: layout.profiles ?? [] })
      setNeedsReboot(Boolean(layout.needsReboot))
      onApplied()
    },
  })
  const applying = apply.isPending
  const matched = expect ? layoutVisible(gpu, children, expect.enabled, expect.profiles) : false
  const waiting = Boolean(expect) && !applying && !matched && !needsReboot
  const done = Boolean(expect) && !applying && matched
  const live = gpu.migEnabled ? (children.length ? children.map((item) => item.profile || item.uuid.slice(0, 8)).join(' + ') : 'MIG 已开，尚无实例') : '整卡'

  useEffect(() => {
    if (!waiting) return
    void queryClient.invalidateQueries({ queryKey: ['topology'] })
    const started = Date.now()
    const timer = window.setInterval(() => {
      void queryClient.invalidateQueries({ queryKey: ['topology'] })
      if (Date.now() - started >= 30_000) window.clearInterval(timer)
    }, 2000)
    return () => window.clearInterval(timer)
  }, [waiting, queryClient, gpu.uuid])

  const resultLabel = expect?.enabled
    ? (expect.profiles.length ? expect.profiles.slice().sort().join(' + ') : 'MIG')
    : '整卡'

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
          const max = maxCountFor(catalog, slices, profile.name)
          const current = plan.counts.get(profile.name) ?? 0
          const on = max > 0 && !applying
          return (
            <div key={profile.name} className={on ? 'resource-choice mig-profile' : 'resource-choice mig-profile disabled'}>
              <span>{profile.g}g</span>
              <small>{profile.memoryGB}GB</small>
              <div className="mig-count">
                {Array.from({ length: max }, (_, index) => index + 1).map((count) => (
                  <button
                    key={count}
                    type="button"
                    className={current === count ? 'active' : ''}
                    disabled={!on}
                    onClick={() => setSlices(setProfileCount(slices, profile.name, current === count ? 0 : count))}
                  >
                    ×{count}
                  </button>
                ))}
              </div>
            </div>
          )
        })}
      </div>
      <div className="mig-picked">
        {slices.length
          ? catalog.profiles.filter((profile) => (plan.counts.get(profile.name) ?? 0) > 0).map((profile) => (
            <span className="mig-chip" key={profile.name}>{profile.name} ×{plan.counts.get(profile.name)}</span>
          ))
          : <span className="muted">未选档位 = 保持或恢复整卡</span>}
      </div>
      <div className="mig-actions">
        <button type="button" className="text-button" disabled={applying} onClick={() => setSlices([])}>清空</button>
        <button type="button" className="secondary-action" disabled={applying} onClick={() => apply.mutate(false)}>恢复整卡</button>
        <button type="button" className="primary-action" disabled={applying || !slices.length} onClick={() => apply.mutate(true)}>
          {applying ? '正在应用…' : '应用切分'}
        </button>
      </div>
      {applying && <div className="field-hint">正在写入 MIG 模式…</div>}
      {needsReboot && <div className="plan-status ok">切分已写入，重启后生效。点上方「重启本机」。</div>}
      {waiting && <div className="field-hint">切分已提交，等待拓扑发现实例…</div>}
      {done && <div className="plan-status ok">已完成：{resultLabel}</div>}
      {apply.error && <div className="plan-conflict">{String(apply.error.message)}</div>}
    </div>
  )
}
