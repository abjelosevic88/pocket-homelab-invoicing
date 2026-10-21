import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Field } from '../components/ui'
import { Icon } from '../components/icons'

export default function Login({ mode }: { mode: 'login' | 'setup' }) {
  const { status, user, refresh } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [form, setForm] = useState({ email: '', password: '', name: '', company_name: '', base_currency: 'EUR' })
  const [err, setErr] = useState(() => new URLSearchParams(window.location.search).get('error') || '')
  const [busy, setBusy] = useState(false)

  const from = (loc.state as { from?: string } | null)?.from || '/'
  if (status && user) return <Navigate to={from} replace />
  if (status && mode === 'setup' && !status.needs_setup) return <Navigate to="/login" replace />
  if (status && mode === 'login' && status.needs_setup) return <Navigate to="/setup" replace />

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setErr('')
    try {
      await api.post(`${V1}/auth/${mode}`, form)
      await refresh()
      navigate(from, { replace: true })
    } catch (e: unknown) {
      setErr((e as Error).message)
    } finally { setBusy(false) }
  }
  const sso = mode === 'login' && !!status?.oidc
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm(f => ({ ...f, [k]: e.target.value }))

  return (
    <div className="login-wrap">
      <div className="login-card">
        <div className="login-brand">
          <img src="/favicon.svg" alt="" width={44} height={44} />
          <h1>Pocket Invoicing</h1>
          <p className="login-tagline">{mode === 'setup' ? 'Welcome! Create your admin account to get started.' : 'Clients, invoices, time and payments — billed in any currency.'}</p>
        </div>
        {sso && <>
          <a className="btn primary lg block" href={`${V1}/auth/oidc/start?next=${encodeURIComponent(from)}`}>
            <Icon name="key" size={16} />Sign in with {status?.oidc_name || 'Pocket Core'}
          </a>
          <div className="login-or"><span>or with a password</span></div>
        </>}
        <form onSubmit={submit} className="stack">
          {mode === 'setup' && <>
            <Field label="Your name"><input value={form.name} onChange={set('name')} autoFocus autoComplete="name" /></Field>
            <Field label="Company / your business name"><input value={form.company_name} onChange={set('company_name')} placeholder="Homelab Consulting" /></Field>
            <Field label="Base currency" help="Reports are shown in this currency. You can invoice in any currency.">
              <input value={form.base_currency} onChange={set('base_currency')} maxLength={4} style={{ textTransform: 'uppercase' }} />
            </Field>
          </>}
          <Field label="Email"><input type="email" value={form.email} onChange={set('email')} required autoFocus={mode === 'login' && !sso} autoComplete="username" /></Field>
          <Field label="Password" help={mode === 'setup' ? 'At least 8 characters' : undefined}>
            <input type="password" value={form.password} onChange={set('password')} required minLength={mode === 'setup' ? 8 : 1} autoComplete={mode === 'setup' ? 'new-password' : 'current-password'} />
          </Field>
          {err && <div className="callout danger"><Icon name="alert" size={15} /><span>{err}</span></div>}
          <button type="submit" className={`btn lg block ${sso ? '' : 'primary'}`} disabled={busy}>{busy ? 'Please wait…' : mode === 'setup' ? 'Create account' : 'Sign in'}</button>
        </form>
        {mode === 'setup' && <p className="login-note">You become the admin. Add people under Settings → Users; each gets their own clients, invoices and numbering.</p>}
      </div>
      {status?.version && <p className="login-version">Pocket Invoicing {/^\d/.test(status.version) ? 'v' + status.version : status.version}</p>}
    </div>
  )
}
