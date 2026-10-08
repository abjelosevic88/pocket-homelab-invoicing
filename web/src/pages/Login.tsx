import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Card, Field } from '../components/ui'

export default function Login({ mode }: { mode: 'login' | 'setup' }) {
  const { status, user, refresh } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [form, setForm] = useState({ email: '', password: '', name: '', company_name: '', base_currency: 'EUR' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  if (status && user) return <Navigate to={(loc.state as { from?: string })?.from || '/'} replace />
  if (status && mode === 'setup' && !status.needs_setup) return <Navigate to="/login" replace />
  if (status && mode === 'login' && status.needs_setup) return <Navigate to="/setup" replace />

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      await api.post(`${V1}/auth/${mode}`, form)
      await refresh()
      navigate('/')
    } catch (e: unknown) {
      setErr((e as Error).message)
    } finally { setBusy(false) }
  }
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setForm(f => ({ ...f, [k]: e.target.value }))

  return (
    <div className="login-wrap">
      <div className="login-card">
        <div className="row mb" style={{ justifyContent: 'center', gap: 10 }}><span className="logo" style={{ width: 36, height: 36, borderRadius: 10, background: 'var(--accent)', color: '#fff', display: 'grid', placeItems: 'center', fontSize: 18 }}>₪</span><h1>Pocket Invoicing</h1></div>
        <Card title={mode === 'setup' ? 'Welcome! Create your admin account' : 'Sign in'}>
          <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
            {mode === 'setup' && <>
              <Field label="Your name"><input value={form.name} onChange={set('name')} autoFocus /></Field>
              <Field label="Company / your business name"><input value={form.company_name} onChange={set('company_name')} placeholder="Homelab Consulting" /></Field>
              <Field label="Base currency" help="Reports are shown in this currency. You can invoice in any currency.">
                <input value={form.base_currency} onChange={set('base_currency')} maxLength={4} style={{ textTransform: 'uppercase' }} />
              </Field>
            </>}
            <Field label="Email"><input type="email" value={form.email} onChange={set('email')} required autoFocus={mode === 'login'} autoComplete="username" /></Field>
            <Field label="Password" help={mode === 'setup' ? 'At least 8 characters' : undefined}><input type="password" value={form.password} onChange={set('password')} required minLength={mode === 'setup' ? 8 : 1} autoComplete={mode === 'setup' ? 'new-password' : 'current-password'} /></Field>
            {err && <div className="callout danger">{err}</div>}
            <button className="btn primary lg" disabled={busy}>{busy ? 'Please wait…' : mode === 'setup' ? 'Create account' : 'Sign in'}</button>
          </form>
        </Card>
        {status?.version && <p className="muted small right mt">v{status.version}</p>}
      </div>
    </div>
  )
}
