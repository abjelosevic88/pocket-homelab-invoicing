import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { TimeEntry } from '../lib/types'

const nav = [
  { section: 'Overview' },
  { to: '/', label: 'Dashboard', ico: '◫' },
  { section: 'Billing' },
  { to: '/invoices', label: 'Invoices', ico: '▤' },
  { to: '/month-end', label: 'Month-end', ico: '◴' },
  { to: '/recurring', label: 'Recurring', ico: '↻' },
  { to: '/payments', label: 'Payments', ico: '◎' },
  { to: '/clients', label: 'Clients', ico: '◉' },
  { section: 'Work' },
  { to: '/time', label: 'Time tracking', ico: '◷' },
  { to: '/expenses', label: 'Expenses', ico: '▽' },
  { to: '/documents', label: 'Documents', ico: '▣' },
  { section: 'Insights' },
  { to: '/reports', label: 'Reports', ico: '▦' },
  { section: 'System' },
  { to: '/settings', label: 'Settings', ico: '⚙' },
]

function TimerWidget() {
  const [running, setRunning] = useState<TimeEntry | null>(null)
  const [now, setNow] = useState(Date.now())
  const navigate = useNavigate()
  useEffect(() => {
    const load = () => api.get<{ running: TimeEntry | null }>(`${V1}/time/running`).then(r => setRunning(r.running)).catch(() => {})
    load()
    const i = setInterval(load, 30000)
    const h = () => load()
    window.addEventListener('pi:timer', h)
    return () => { clearInterval(i); window.removeEventListener('pi:timer', h) }
  }, [])
  useEffect(() => { const i = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(i) }, [])
  if (!running) return null
  const secs = Math.max(0, Math.floor((now - new Date(running.started_at).getTime()) / 1000))
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60), s = secs % 60
  return (
    <button className="timer-pill" onClick={() => navigate('/time')} title={`${running.client_name} · ${running.description || running.project}`}>
      <span className="dot" /> {String(h).padStart(2, '0')}:{String(m).padStart(2, '0')}:{String(s).padStart(2, '0')} <span className="muted small">{running.client_name}</span>
    </button>
  )
}

export default function Layout() {
  const { user, settings, status, theme, toggleTheme, setUser } = useApp()
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const logout = async () => { await api.post(`${V1}/auth/logout`); setUser(null); navigate('/login') }
  return (
    <div className="app">
      <aside className={`sidebar ${open ? 'open' : ''}`} onClick={() => setOpen(false)}>
        <div className="brand" title={settings?.company_name}><span className="logo">₪</span><span className="brand-name">{settings?.brand_name || settings?.company_name || 'Pocket Invoicing'}</span></div>
        <nav>
          {nav.map((n, i) => 'section' in n && n.section
            ? <div key={i} className="section">{n.section}</div>
            : <NavLink key={n.to} to={n.to!} end={n.to === '/'} className={({ isActive }) => isActive ? 'active' : ''}><span className="ico">{n.ico}</span>{n.label}</NavLink>)}
        </nav>
        <div className="foot">
          <span title={user?.email}>{user?.name || user?.email}</span>
          <span className="row">
            <button className="btn ghost sm" onClick={toggleTheme} title="Toggle theme">{theme === 'dark' ? '☀' : '☾'}</button>
            {status?.auth_mode === 'local' && <button className="btn ghost sm" onClick={logout} title="Sign out">⎋</button>}
          </span>
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <button className="btn ghost icon menu-btn" onClick={() => setOpen(o => !o)} aria-label="Menu">☰</button>
          <TimerWidget />
          <span className="spacer" />
          <button className="btn primary sm topbar-new" onClick={() => navigate('/invoices/new')}>+ New invoice</button>
        </header>
        <main className="content"><Outlet /></main>
      </div>
    </div>
  )
}
