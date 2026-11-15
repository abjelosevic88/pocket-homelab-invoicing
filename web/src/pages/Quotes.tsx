import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate, money } from '../lib/format'
import type { Client, Quote } from '../lib/types'
import { Badge, Card, Empty, Loading, PageHeader, useAsync, useDebounce } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'

const STATUSES: { value: string; label: string; hint?: string }[] = [
  { value: 'all', label: 'All statuses' },
  { value: 'draft', label: 'Draft' },
  { value: 'open', label: 'Open', hint: 'waiting for an answer' },
  { value: 'accepted', label: 'Accepted' },
  { value: 'invoiced', label: 'Invoiced' },
  { value: 'declined', label: 'Declined' },
  { value: 'expired', label: 'Expired' },
]

export default function Quotes() {
  const [sp, setSp] = useSearchParams()
  const status = sp.get('status') || 'all'
  const clientId = sp.get('client_id') || ''
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q)
  const [page, setPage] = useState(0)
  const limit = 50
  const navigate = useNavigate()
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients?archived=1`))
  const { data, loading } = useAsync(() => api.get<{ items: Quote[]; total: number }>(`${V1}/quotes?status=${status}&client_id=${clientId}&q=${encodeURIComponent(dq)}&limit=${limit}&offset=${page * limit}`), [status, clientId, dq, page])
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n); setPage(0) }
  const newLink = clientId ? `/quotes/new?client_id=${clientId}` : '/quotes/new'
  return (
    <>
      <PageHeader title="Quotes" sub={data ? `${data.total} quote${data.total === 1 ? '' : 's'} · the client accepts from the link, which starts a draft invoice` : ''} actions={
        <Link className="btn primary" to={newLink}><Icon name="plus" size={15} />New quote</Link>
      } />
      <div className="filters">
        <input className="search" placeholder="Search number, client, PO…" value={q} onChange={e => setQ(e.target.value)} />
        <Select value={status} onChange={v => setParam('status', v === 'all' ? '' : v)} ariaLabel="Status" placeholder="All statuses" options={STATUSES} />
        <Select value={clientId} onChange={v => setParam('client_id', v)} ariaLabel="Client" placeholder="All clients" options={[{ value: '', label: 'All clients' }, ...(clients || []).map(c => ({ value: String(c.id), label: c.name }))]} />
      </div>
      <Card flush>
        {loading && !data ? <Loading /> : !data?.items.length ? <Empty title="No quotes found">{status === 'all' && !q ? <Link to={newLink}>Write your first quote</Link> : 'Try a different filter.'}</Empty> : (
          <div className="table-wrap"><table className="table">
            <thead><tr><th>Number</th><th>Client</th><th>Date</th><th>Valid until</th><th>Status</th><th className="num">Total</th></tr></thead>
            <tbody>{data.items.map(x => <tr key={x.id} className="clickable" onClick={() => navigate(`/quotes/${x.id}`)}>
              <td><div className="bold">{x.number}{x.revision > 1 && <span className="muted small"> · rev {x.revision}</span>}</div>{x.po_number && <div className="muted small">PO {x.po_number}</div>}</td>
              <td>{x.client_name}</td>
              <td className="muted">{fmtDate(x.issue_date)}</td>
              <td className="muted">{fmtDate(x.valid_until)}</td>
              <td><Badge status={x.status} />{x.invoice_number && <div className="muted small">→ {x.invoice_number}</div>}</td>
              <td className="num">{money(x.total, x.currency)}</td>
            </tr>)}</tbody>
          </table></div>
        )}
      </Card>
      {data && data.total > limit && <div className="row mt" style={{ justifyContent: 'center' }}>
        <button type="button" className="btn sm" disabled={page === 0} onClick={() => setPage(p => p - 1)}>← Prev</button>
        <span className="muted small">Page {page + 1} of {Math.ceil(data.total / limit)}</span>
        <button type="button" className="btn sm" disabled={(page + 1) * limit >= data.total} onClick={() => setPage(p => p + 1)}>Next →</button>
      </div>}
    </>
  )
}
