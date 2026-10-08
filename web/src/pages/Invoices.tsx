import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, money } from '../lib/format'
import type { Client, Invoice } from '../lib/types'
import { Badge, Card, Empty, Loading, PageHeader, useAsync, useDebounce } from '../components/ui'

const STATUSES = ['all', 'draft', 'open', 'sent', 'viewed', 'partial', 'overdue', 'paid', 'cancelled']

export default function Invoices() {
  const [sp, setSp] = useSearchParams()
  const status = sp.get('status') || 'all'
  const clientId = sp.get('client_id') || ''
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q)
  const [page, setPage] = useState(0)
  const limit = 50
  const navigate = useNavigate()
  const { settings } = useApp()
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients?archived=1`))
  const { data, loading } = useAsync(() => api.get<{ items: Invoice[]; total: number }>(`${V1}/invoices?status=${status}&client_id=${clientId}&q=${encodeURIComponent(dq)}&limit=${limit}&offset=${page * limit}`), [status, clientId, dq, page])
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n); setPage(0) }
  const base = settings?.base_currency || ''
  const totals = (data?.items || []).reduce((acc, i) => { acc[i.currency] = (acc[i.currency] || 0) + i.balance; return acc }, {} as Record<string, number>)
  return (
    <>
      <PageHeader title="Invoices" sub={data ? `${data.total} invoice${data.total === 1 ? '' : 's'}` : ''} actions={<>
        <a className="btn" href={`${V1}/reports/export.csv?type=invoices&status=${status === 'all' || status === 'open' ? '' : status}`}>Export CSV</a>
        <Link className="btn primary" to="/invoices/new">+ New invoice</Link>
      </>} />
      <div className="filters">
        <input className="search" placeholder="Search number, client, PO…" value={q} onChange={e => setQ(e.target.value)} />
        <select value={status} onChange={e => setParam('status', e.target.value === 'all' ? '' : e.target.value)}>{STATUSES.map(s => <option key={s} value={s}>{s === 'all' ? 'All statuses' : s === 'open' ? 'Open (unpaid)' : s.charAt(0).toUpperCase() + s.slice(1)}</option>)}</select>
        <select value={clientId} onChange={e => setParam('client_id', e.target.value)}><option value="">All clients</option>{clients?.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
      </div>
      <Card flush>
        {loading && !data ? <Loading /> : !data?.items.length ? <Empty title="No invoices found">{status === 'all' && !q ? <Link to="/invoices/new">Create your first invoice</Link> : 'Try a different filter.'}</Empty> : (
          <div className="table-wrap"><table className="table">
            <thead><tr><th>Number</th><th>Client</th><th>Issued</th><th>Due</th><th>Status</th><th className="num">Total</th><th className="num">Balance</th>{base && <th className="num">In {base}</th>}</tr></thead>
            <tbody>{data.items.map(i => <tr key={i.id} className="clickable" onClick={() => navigate(`/invoices/${i.id}`)}>
              <td><div className="bold">{i.number}</div>{i.po_number && <div className="muted small">PO {i.po_number}</div>}</td>
              <td>{i.client_name}</td>
              <td className="muted">{fmtDate(i.issue_date)}</td>
              <td className={i.status === 'overdue' ? 'bold' : 'muted'} style={i.status === 'overdue' ? { color: 'var(--danger)' } : {}}>{fmtDate(i.due_date)}</td>
              <td><Badge status={i.status} /></td>
              <td className="num">{money(i.total, i.currency)}</td>
              <td className="num">{i.balance > 0 && i.status !== 'draft' && i.status !== 'cancelled' ? money(i.balance, i.currency) : <span className="muted">—</span>}</td>
              {base && <td className="num muted">{i.currency === base ? '' : money(i.total * i.exchange_rate, base)}</td>}
            </tr>)}</tbody>
            {Object.keys(totals).length > 0 && <tfoot><tr><td colSpan={6}>Balance on this page</td><td className="num" colSpan={base ? 2 : 1}>{Object.entries(totals).filter(([, v]) => v > 0).map(([c, v]) => money(v, c)).join(' + ') || '—'}</td></tr></tfoot>}
          </table></div>
        )}
      </Card>
      {data && data.total > limit && <div className="row mt" style={{ justifyContent: 'center' }}>
        <button className="btn sm" disabled={page === 0} onClick={() => setPage(p => p - 1)}>← Prev</button>
        <span className="muted small">Page {page + 1} of {Math.ceil(data.total / limit)}</span>
        <button className="btn sm" disabled={(page + 1) * limit >= data.total} onClick={() => setPage(p => p + 1)}>Next →</button>
      </div>}
    </>
  )
}
