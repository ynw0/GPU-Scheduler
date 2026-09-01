import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { api } from './api'
import type { User } from './types'

export function AccountMenu({ user }: { user: User }) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [done, setDone] = useState('')
  const change = useMutation({
    mutationFn: () => api.changePassword(currentPassword, newPassword),
    onSuccess: () => {
      setDone('密码已更新')
      setCurrentPassword('')
      setNewPassword('')
    },
    onMutate: () => setDone(''),
  })
  const logout = useMutation({
    mutationFn: () => api.logout(),
    onSuccess: () => {
      queryClient.clear()
      void queryClient.invalidateQueries({ queryKey: ['me'] })
    },
  })

  return (
    <div className="account-wrap">
      <button className="avatar" title={`${user.username} · ${user.role}`} onClick={() => setOpen((value) => !value)}>
        {user.username.slice(0, 2).toUpperCase()}
      </button>
      {open && (
        <div className="account-menu">
          <strong>{user.username}</strong>
          <span>{user.role}</span>
          <button type="button" onClick={() => { setOpen(false); setPasswordOpen(true) }}>修改密码</button>
          <button type="button" onClick={() => logout.mutate()}>{logout.isPending ? '正在退出…' : '退出登录'}</button>
        </div>
      )}
      {passwordOpen && (
        <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setPasswordOpen(false) }}>
          <section className="account-dialog" role="dialog" aria-modal="true" aria-labelledby="change-password-title">
            <header className="dialog-header">
              <div>
                <p className="eyebrow">ACCOUNT</p>
                <h2 id="change-password-title">修改密码</h2>
              </div>
              <button className="icon-button" onClick={() => setPasswordOpen(false)} aria-label="关闭"><X size={18} /></button>
            </header>
            <form className="account-form" onSubmit={(event) => { event.preventDefault(); change.mutate() }}>
              <label>当前密码<input type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} /></label>
              <label>
                新密码
                <input type="password" autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} />
                <small className="field-hint">密码不少于配置下限（至少 12 位）</small>
              </label>
              {done && <div className="plan-status ok">{done}</div>}
              {change.error && <div className="plan-conflict">{String(change.error.message)}</div>}
              <button className="primary-action" type="submit" disabled={change.isPending || !currentPassword || !newPassword}>
                {change.isPending ? '正在保存…' : '保存'}
              </button>
            </form>
          </section>
        </div>
      )}
    </div>
  )
}
