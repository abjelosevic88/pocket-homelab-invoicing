// ⌘K / "/" — jump to a client or an invoice, or run a command from the keyboard.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate, money } from '../lib/format'
import type { Client, Invoice } from '../lib/types'
import { Icon } from './icons'
import { useDebounce } from './ui'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

export function CommandPalette({ onClose, commands }: { onClose: () => void; commands: PaletteCommand[] }) {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [hi, setHi] = useState(0)
  const [clients, setClients] = useState<Client[]>([])
  const [invoices, setInvoices] = useState<Invoice[]>([])
  const list = useRef<HTMLDivElement>(null)
  const dq = useDebounce(q.trim(), 180)
  useEffect(() => { api.get<Client[]>(`${V1}/clients`).then(setClients).catch(() => {}) }, [])
  useEffect(() => {
    let live = true
    api.get<{ items: Invoice[] }>(`${V1}/invoices?status=all&limit=${dq ? 8 : 5}&q=${encodeURIComponent(dq)}`).then(r => { if (live) setInvoices(r.items || []) }).catch(() => {})
    return () => { live = false }
  }, [dq])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = []
    const n = q.trim().toLowerCase()
    const words = n.split(/\s+/).filter(Boolean)
    const hit = (text: string) => words.every(w => text.toLowerCase().includes(w))
    for (const c of clients) {
      if (!n || !hit(`${c.name} ${c.contact_name} ${c.email}`)) continue
      out.push({ key: `c${c.id}`, group: 'Clients', icon: <Icon name="briefcase" size={16} />, label: c.name, sub: [c.contact_name, c.outstanding > 0 && `${money(c.outstanding, c.currency)} outstanding`].filter(Boolean).join(' · '), run: () => navigate(`/clients/${c.id}`) })
      if (out.length >= 6) break
    }
    for (const i of invoices) {
      out.push({ key: `i${i.id}`, group: n ? 'Invoices' : 'Recent invoices', icon: <Icon name="invoice" size={16} />, label: <>{i.number} <span className={`badge ${i.status}`} style={{ marginLeft: 6 }}>{i.status}</span></>, sub: `${i.client_name} · ${fmtDate(i.issue_date)} · ${money(i.total, i.currency)}`, run: () => navigate(`/invoices/${i.id}`) })
    }
    for (const c of commands) {
      if (n && !hit(`${c.label} ${c.keywords || ''}`)) continue
      out.push({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint, run: c.run })
    }
    return out
  }, [q, clients, invoices, commands, navigate])
  useEffect(() => setHi(0), [q])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find an invoice or a client, or run a command…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {rows.length === 0 && <div className="pal-empty">Nothing matches.</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label">{r.label}</span>{r.sub && <span className="pal-sub">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
