// Left column: create button, sections, the running timer, clients, settings — the Pocket Calendar sidebar.
import { type CSSProperties, useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { money } from '../lib/format'
import type { Client, Invoice, TimeEntry } from '../lib/types'
import { Icon } from './icons'
import { type CreateKind, elapsed, useOnChange } from './shell'
import { type Anchor, type MenuItem, Menu, rectOf } from './ui'

type NavItem = { to: string; label: string; icon: string; end?: boolean; badge?: number; badgeTitle?: string; live?: boolean }

export function Sidebar({ onCreate, onNavigate, running }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; running: TimeEntry | null }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [overdue, setOverdue] = useState(0)
  const [clients, setClients] = useState<Client[] | null>(null)
  const load = () => {
    api.get<{ items: Invoice[]; total: number }>(`${V1}/invoices?status=overdue&limit=1`).then(r => setOverdue(r.total || 0)).catch(() => {})
    api.get<Client[]>(`${V1}/clients`).then(setClients).catch(() => setClients([]))
  }
  // counts follow what you do: re-read them when you move between pages
  const { pathname } = useLocation()
  useEffect(load, [pathname])
  useOnChange(load)

  const brand = settings?.brand_name?.trim()
  const createItems: MenuItem[] = [
    { label: 'Invoice', icon: 'invoice', hint: 'N', onSelect: () => onCreate('invoice') },
    { label: 'Client', icon: 'briefcase', hint: 'A', onSelect: () => onCreate('client') },
    'sep',
    { label: 'Track time', icon: 'timer', hint: 'T', onSelect: () => { onNavigate(); navigate('/time') } },
    { label: 'Month-end invoices', icon: 'calendar-check', hint: 'M', onSelect: () => { onNavigate(); navigate('/month-end') } },
  ]
  const main: NavItem[] = [
    { to: '/', label: 'Dashboard', icon: 'home', end: true },
    { to: '/invoices', label: 'Invoices', icon: 'invoice', badge: overdue, badgeTitle: `${overdue} overdue` },
    { to: '/clients', label: 'Clients', icon: 'users' },
    { to: '/payments', label: 'Payments', icon: 'banknote' },
  ]
  const billing: NavItem[] = [
    { to: '/month-end', label: 'Month-end', icon: 'calendar-check' },
    { to: '/recurring', label: 'Recurring', icon: 'repeat' },
    { to: '/reports', label: 'Reports', icon: 'chart' },
  ]
  const work: NavItem[] = [
    { to: '/time', label: 'Time tracking', icon: 'timer', live: !!running },
    { to: '/expenses', label: 'Expenses', icon: 'receipt' },
    { to: '/documents', label: 'Documents', icon: 'file' },
  ]
  // clients that owe money first, then the rest by name
  const shown = (clients || []).slice().sort((a, b) => (b.outstanding > 0 ? 1 : 0) - (a.outstanding > 0 ? 1 : 0) || a.name.localeCompare(b.name)).slice(0, 8)
  return (
    <aside className="sidebar" aria-label="Invoicing">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title" title={brand || 'Pocket Invoicing'}>{brand && brand !== 'Pocket Invoicing' ? brand : <>Pocket <b>Invoicing</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems} onClose={() => setMenu(null)} width={248} />}
      {running && <TimerCard running={running} onNavigate={onNavigate} />}

      <Nav items={main} onNavigate={onNavigate} />
      <NavGroup title="Billing" items={billing} onNavigate={onNavigate} />
      <NavGroup title="Work" items={work} onNavigate={onNavigate} />

      <ClientGroup list={shown} total={clients?.length || 0} loading={!clients} onNavigate={onNavigate} add={() => onCreate('client')} />

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pi:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Nav({ items, onNavigate }: { items: NavItem[]; onNavigate: () => void }) {
  return (
    <nav className="sb-nav" aria-label="Sections">
      {items.map(n => (
        <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
          <Icon name={n.icon} size={16} />{n.label}
          {!!n.badge && <span className="badge danger" title={n.badgeTitle}>{n.badge}</span>}
          {n.live && <span className="sb-live" title="Timer running" />}
        </NavLink>
      ))}
    </nav>
  )
}

function NavGroup({ title, items, onNavigate }: { title: string; items: NavItem[]; onNavigate: () => void }) {
  return (
    <section className="sb-sec">
      <h4 className="sb-h"><span className="sb-h-btn" style={{ cursor: 'default' }}>{title}</span></h4>
      <Nav items={items} onNavigate={onNavigate} />
    </section>
  )
}

function TimerCard({ running, onNavigate }: { running: TimeEntry; onNavigate: () => void }) {
  const navigate = useNavigate()
  const [now, setNow] = useState(Date.now())
  useEffect(() => { const i = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(i) }, [])
  return (
    <button type="button" className="sb-timer" onClick={() => { onNavigate(); navigate('/time') }} title="Open time tracking">
      <Icon name="timer" size={17} />
      <span className="grow"><span className="sb-timer-t">{elapsed(running.started_at, now)}</span><span className="sb-timer-s">{[running.client_name, running.description || running.project].filter(Boolean).join(' · ')}</span></span>
    </button>
  )
}

function ClientGroup({ list, total, loading, onNavigate, add }: { list: Client[]; total: number; loading: boolean; onNavigate: () => void; add: () => void }) {
  const [open, setOpen] = useState(() => localStorage.getItem('pi-sb-clients') !== '0')
  const toggle = () => setOpen(o => { localStorage.setItem('pi-sb-clients', o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />Clients</button>
        <button type="button" className="btn ghost icon-only xs" aria-label="New client" title="New client (a)" onClick={add}><Icon name="plus" size={14} /></button>
      </h4>
      {open && (loading ? null : list.length === 0
        ? <p className="sb-empty">No clients yet. <button type="button" className="link-btn" onClick={add}>Add one</button></p>
        : <>
          <ul className="cal-list">{list.map(c => <ClientRow key={c.id} c={c} onNavigate={onNavigate} />)}</ul>
          {total > list.length && <Link to="/clients" className="sb-more" onClick={onNavigate}><Icon name="more" size={14} />All {total} clients</Link>}
        </>)}
    </section>
  )
}

const hue = (s: string) => [...s].reduce((a, c) => (a * 31 + c.charCodeAt(0)) % 360, 11)
const initials = (s: string) => s.split(/[\s.&,-]+/).filter(Boolean).slice(0, 2).map(w => w[0]!.toUpperCase()).join('')

function ClientRow({ c, onNavigate }: { c: Client; onNavigate: () => void }) {
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const go = (to: string) => { onNavigate(); navigate(to) }
  const items: MenuItem[] = [
    { label: 'Open', icon: 'briefcase', onSelect: () => go(`/clients/${c.id}`) },
    { label: 'New invoice', icon: 'plus', onSelect: () => go(`/invoices/new?client_id=${c.id}`) },
    'sep',
    { label: 'Invoices', icon: 'invoice', onSelect: () => go(`/invoices?client_id=${c.id}`) },
    { label: 'Time entries', icon: 'timer', onSelect: () => go(`/time?client_id=${c.id}`) },
    { label: 'Documents', icon: 'file', onSelect: () => go(`/documents?client_id=${c.id}`) },
  ]
  return (
    <li className="cal-row cl-row">
      <NavLink to={`/clients/${c.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={c.outstanding > 0 ? `${c.name} · ${money(c.outstanding, c.currency)} outstanding` : c.name}>
        <span className="cl-ico" style={{ '--c': `hsl(${hue(c.name)} 52% 50%)` } as CSSProperties}>{initials(c.name)}</span>
        <span className="cal-name ellipsis">{c.name}</span>
        {c.outstanding > 0 && <span className="cl-meta owed">{money(c.outstanding, c.currency)}</span>}
      </NavLink>
      <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${c.name}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={210} />}
    </li>
  )
}
