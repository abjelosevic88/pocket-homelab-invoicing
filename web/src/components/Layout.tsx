// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { TimeEntry } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { ErrorBoundary } from './ErrorBoundary'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, Modal, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery } from './ui'
import { ClientForm } from '../pages/Clients'
import { announceChange, type CreateKind, elapsed, useRunningTimer } from './shell'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pi-sidebar'

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [newClient, setNewClient] = useState(false)
  const running = useRunningTimer()

  const create = (kind: CreateKind) => {
    closeDrawer()
    if (kind === 'invoice') navigate('/invoices/new')
    else setNewClient(true)
  }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pi:shortcuts', open)
    return () => window.removeEventListener('pi:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t?.closest?.('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': e.preventDefault(); createRef.current('invoice'); break
        case 'a': e.preventDefault(); createRef.current('client'); break
        case 'd': go('/'); break
        case 'i': go('/invoices'); break
        case 'c': go('/clients'); break
        case 'p': go('/payments'); break
        case 'm': go('/month-end'); break
        case 't': go('/time'); break
        case 'e': go('/expenses'); break
        case 'r': go('/reports'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'new-invoice', label: 'New invoice', icon: 'plus', hint: 'N', keywords: 'create bill', run: () => create('invoice') },
    { id: 'new-client', label: 'New client', icon: 'briefcase', hint: 'A', keywords: 'add customer', run: () => create('client') },
    { id: 'dashboard', label: 'Dashboard', icon: 'home', hint: 'D', run: () => navigate('/') },
    { id: 'invoices', label: 'Invoices', icon: 'invoice', hint: 'I', run: () => navigate('/invoices') },
    { id: 'overdue', label: 'Overdue invoices', icon: 'alert', keywords: 'late unpaid', run: () => navigate('/invoices?status=overdue') },
    { id: 'drafts', label: 'Draft invoices', icon: 'edit', keywords: 'unsent', run: () => navigate('/invoices?status=draft') },
    { id: 'month-end', label: 'Month-end', icon: 'calendar-check', hint: 'M', keywords: 'wizard monthly', run: () => navigate('/month-end') },
    { id: 'recurring', label: 'Recurring', icon: 'repeat', keywords: 'retainer subscription', run: () => navigate('/recurring') },
    { id: 'payments', label: 'Payments', icon: 'banknote', hint: 'P', keywords: 'received', run: () => navigate('/payments') },
    { id: 'clients', label: 'Clients', icon: 'users', hint: 'C', keywords: 'customers', run: () => navigate('/clients') },
    { id: 'time', label: 'Time tracking', icon: 'timer', hint: 'T', keywords: 'timer hours', run: () => navigate('/time') },
    { id: 'expenses', label: 'Expenses', icon: 'receipt', hint: 'E', keywords: 'costs', run: () => navigate('/expenses') },
    { id: 'documents', label: 'Documents', icon: 'file', keywords: 'contracts files', run: () => navigate('/documents') },
    { id: 'reports', label: 'Reports', icon: 'chart', hint: 'R', keywords: 'revenue tax aging', run: () => navigate('/reports') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'templates', label: 'Invoice templates', icon: 'palette', keywords: 'pdf word design labels', run: () => navigate('/settings/templates') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
      {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
      <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
        <Sidebar onCreate={create} onNavigate={closeDrawer} running={running} />
      </div>
      <div className="main">
        <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} running={running} />
        <main className="content"><div className="content-inner"><ErrorBoundary key={loc.pathname} where="page"><Outlet /></ErrorBoundary></div></main>
      </div>
      {mobile && !loc.pathname.startsWith('/invoices/') && <button type="button" className="fab" aria-label="New invoice" title="New invoice" onClick={() => create('invoice')}><Icon name="plus" size={24} strokeWidth={2} /></button>}
      {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      {newClient && <Modal title="New client" size="lg" onClose={() => setNewClient(false)}><ClientForm onSaved={c => { setNewClient(false); announceChange(); navigate(`/clients/${c.id}`) }} onClose={() => setNewClient(false)} /></Modal>}
    </div>
  )
}

const SECTION: Record<string, string> = { '': 'Dashboard', invoices: 'Invoices', 'month-end': 'Month-end', recurring: 'Recurring', payments: 'Payments', clients: 'Clients', time: 'Time tracking', expenses: 'Expenses', documents: 'Documents', reports: 'Reports' }

function TimerPill({ running }: { running: TimeEntry }) {
  const navigate = useNavigate()
  const [now, setNow] = useState(Date.now())
  useEffect(() => { const i = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(i) }, [])
  return (
    <button type="button" className="timer-pill" onClick={() => navigate('/time')} title={`Timer running: ${running.client_name} · ${running.description || running.project}`}>
      <span className="dot" />{elapsed(running.started_at, now)}<span className="muted">{running.client_name}</span>
    </button>
  )
}

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, running }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; running: TimeEntry | null }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [crumb, setCrumb] = useState('')
  useEffect(() => {
    const h = (e: Event) => setCrumb((e as CustomEvent<string>).detail)
    window.addEventListener('pi:crumb', h)
    return () => window.removeEventListener('pi:crumb', h)
  }, [])
  useEffect(() => setCrumb(''), [loc.pathname])
  const parts = loc.pathname.split('/').filter(Boolean)
  const top = parts[0] || ''
  const section = SECTION[top] || ''
  // a second level: an invoice, the new-invoice editor, a client
  const sub = top === 'invoices' && parts[1] === 'new' ? 'New invoice' : (top === 'invoices' || top === 'clients') && parts[1] ? (crumb || '…') + (parts[2] === 'edit' ? ' · edit' : '') : ''
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/users') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pi:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {sub ? <><button type="button" className="crumb" onClick={() => navigate(`/${top}`)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{sub}</span></> : section}
      </h1>
      <span className="spacer" />
      {running && <TimerPill running={running} />}
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
