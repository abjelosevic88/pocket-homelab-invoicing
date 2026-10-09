import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { BILLING_MODES, fmtDate, hours, money } from '../lib/format'
import type { Document, Client, Invoice, PaperlessDoc, Recurring } from '../lib/types'
import { Badge, Card, Confirm, Empty, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'
import { usePaperless } from '../components/paperless'
import { ClientForm } from './Clients'

interface Detail { client: Client; invoices: Invoice[]; recurring: Recurring[]; unbilled_minutes: number; unbilled_entries: number }

export default function ClientDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { data, loading, reload } = useAsync(() => api.get<Detail>(`${V1}/clients/${id}`), [id])
  const [editing, setEditing] = useState(false)
  const [confirmDel, setConfirmDel] = useState(false)
  if (loading || !data) return <Loading />
  const c = data.client
  const del = async () => {
    const r = await api.del<{ deleted: boolean }>(`${V1}/clients/${c.id}`)
    toast(r.deleted ? 'Client deleted' : 'Client has invoices, so it was archived instead', 'success')
    navigate('/clients')
  }
  return (
    <>
      <PageHeader title={c.name} sub={<>{c.contact_name}{c.contact_name && c.email ? ' · ' : ''}{c.email}</>} actions={<>
        <button className="btn" onClick={() => setEditing(true)}>Edit</button>
        <button className="btn danger" onClick={() => setConfirmDel(true)}>{c.invoice_count ? 'Archive' : 'Delete'}</button>
        <Link className="btn" to={`/time?client_id=${c.id}`}>Time entries</Link>
        <Link className="btn primary" to={`/invoices/new?client_id=${c.id}`}>New invoice</Link>
      </>} />
      <div className="grid cols-4 mb">
        <div className="card stat"><div className="label">Outstanding</div><div className="value">{money(c.outstanding, c.currency)}</div></div>
        <div className="card stat"><div className="label">Total billed</div><div className="value">{money(c.total_billed, c.currency)}</div><div className="hint">{c.invoice_count} invoices</div></div>
        <div className="card stat"><div className="label">Unbilled time</div><div className="value">{hours(data.unbilled_minutes)}</div><div className="hint">{data.unbilled_entries ? <Link to={`/time?client_id=${c.id}&unbilled=1`}>Invoice it →</Link> : 'nothing pending'}</div></div>
        <div className="card stat"><div className="label">Billing</div><div className="value" style={{ fontSize: 18 }}>{BILLING_MODES.find(b => b.value === c.billing_mode)?.label}</div><div className="hint">{c.default_rate ? `${money(c.default_rate, c.currency)} · ` : ''}net {c.payment_terms_days} days · {c.currency}</div></div>
      </div>
      <div className="grid cols-2 mb" style={{ gridTemplateColumns: '1fr 2fr' }}>
        <Card title="Details">
          <dl className="kv">
            {c.address1 && <><dt>Address</dt><dd>{c.address1}<br />{c.address2 && <>{c.address2}<br /></>}{[c.postal_code, c.city].filter(Boolean).join(' ')}{c.state ? `, ${c.state}` : ''}<br />{c.country}</dd></>}
            {c.phone && <><dt>Phone</dt><dd>{c.phone}</dd></>}
            {c.website && <><dt>Website</dt><dd><a href={c.website} target="_blank" rel="noreferrer">{c.website}</a></dd></>}
            {c.tax_id && <><dt>Tax ID</dt><dd>{c.tax_id}</dd></>}
            {c.notes && <><dt>Notes</dt><dd style={{ whiteSpace: 'pre-wrap' }}>{c.notes}</dd></>}
          </dl>
        </Card>
        <Card title="Invoices" flush>
          {data.invoices.length === 0 ? <Empty title="No invoices yet" /> : <div className="table-wrap"><table className="table">
            <thead><tr><th>Number</th><th>Issued</th><th>Due</th><th>Status</th><th className="num">Total</th><th className="num">Balance</th></tr></thead>
            <tbody>{data.invoices.map(i => <tr key={i.id} className="clickable" onClick={() => navigate(`/invoices/${i.id}`)}>
              <td className="bold">{i.number}</td><td className="muted">{fmtDate(i.issue_date)}</td><td className="muted">{fmtDate(i.due_date)}</td><td><Badge status={i.status} /></td><td className="num">{money(i.total, i.currency)}</td><td className="num">{money(i.balance, i.currency)}</td>
            </tr>)}</tbody>
          </table></div>}
        </Card>
      </div>
      <ClientDocuments clientId={c.id} />
      <ClientPaperless client={c} onEdit={() => setEditing(true)} />
      {data.recurring?.length > 0 && <Card title="Recurring profiles" flush>
        <table className="table"><thead><tr><th>Name</th><th>Frequency</th><th>Next run</th><th>Status</th></tr></thead>
          <tbody>{data.recurring.map(r => <tr key={r.id} className="clickable" onClick={() => navigate('/recurring')}><td className="bold">{r.name}</td><td>{r.frequency}{r.interval > 1 ? ` ×${r.interval}` : ''}</td><td>{fmtDate(r.next_run)}</td><td><Badge status={r.status} /></td></tr>)}</tbody></table>
      </Card>}
      {editing && <Modal title="Edit client" size="lg" onClose={() => setEditing(false)}><ClientForm initial={c} onSaved={() => { setEditing(false); reload() }} onClose={() => setEditing(false)} /></Modal>}
      {confirmDel && <Confirm title={c.invoice_count ? 'Archive client?' : 'Delete client?'} message={c.invoice_count ? 'This client has invoices, so it will be archived (hidden) rather than deleted.' : 'This permanently deletes the client and its time entries.'} onConfirm={del} onCancel={() => setConfirmDel(false)} />}
    </>
  )
}

function ClientDocuments({ clientId }: { clientId: number }) {
  const { data } = useAsync(() => api.get<Document[]>(`${V1}/documents?client_id=${clientId}`), [clientId])
  if (!data?.length) return null
  return <Card title="Documents" actions={<Link className="btn sm" to="/documents">All documents</Link>} flush className="mb">
    <table className="table"><thead><tr><th>Title</th><th>Category</th><th>Date</th><th>Expires</th></tr></thead>
      <tbody>{data.map(d => <tr key={d.id}><td><a className="bold" href={`${V1}/documents/${d.id}/file`} target="_blank" rel="noreferrer">{d.title}</a><div className="muted small">{d.filename}</div></td><td>{d.category && <span className="badge">{d.category}</span>}</td><td className="muted">{fmtDate(d.doc_date)}</td><td className="muted">{fmtDate(d.expires_at)}</td></tr>)}</tbody></table>
  </Card>
}

function ClientPaperless({ client, onEdit }: { client: Client; onEdit: () => void }) {
  const pl = usePaperless()
  const cid = client.paperless_correspondent_id
  const { data, loading, error } = useAsync(() => pl?.configured && cid ? api.get<{ count: number; results: PaperlessDoc[] }>(`${V1}/paperless/documents?correspondent__id=${cid}&page_size=15`) : Promise.resolve(null), [pl?.configured, cid])
  if (!pl?.configured) return null
  if (!cid) return <div className="callout mb">Paperless-ngx is connected but this client is not linked to a correspondent yet. <button className="link-btn" onClick={onEdit}>Edit the client</button> and pick one (or create it) to see their Paperless documents here.</div>
  const plUrl = `${pl.url}/documents?correspondent__id=${cid}`
  return <Card title={<>Paperless · {client.paperless_correspondent || `correspondent #${cid}`}{data ? <span className="muted small"> · {data.count} document{data.count === 1 ? '' : 's'}</span> : null}</>} actions={<a className="btn sm" href={plUrl} target="_blank" rel="noreferrer">Open in Paperless ↗</a>} flush className="mb">
    {error ? <div className="empty muted">{String(error)}</div> : loading && !data ? <Loading /> : !data?.results.length ? <div className="empty muted">No documents for this correspondent in Paperless yet.</div> :
      <table className="table"><thead><tr><th></th><th>Title</th><th>Type</th><th>Tags</th><th>Created</th><th></th></tr></thead>
        <tbody>{data.results.map(d => <tr key={d.id}>
          <td style={{ width: 48 }}><a href={`${V1}/paperless/documents/${d.id}/file`} target="_blank" rel="noreferrer"><img src={`${V1}/paperless/documents/${d.id}/thumb`} alt="" style={{ width: 36, height: 46, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--surface-2)' }} loading="lazy" /></a></td>
          <td><a className="bold" href={`${V1}/paperless/documents/${d.id}/file`} target="_blank" rel="noreferrer">{d.title}</a><div className="muted small">#{d.id}{d.original_file_name ? ` · ${d.original_file_name}` : ''}</div></td>
          <td className="muted">{d.document_type}</td>
          <td>{d.tags.map(t => <span key={t} className="badge" style={{ marginRight: 4 }}>{t}</span>)}</td>
          <td className="muted">{fmtDate(d.created)}</td>
          <td className="actions"><a className="btn ghost sm" href={d.url} target="_blank" rel="noreferrer" title="Open in Paperless">↗</a><a className="btn ghost sm" href={`${V1}/paperless/documents/${d.id}/file?download=1`} title="Download">↓</a></td>
        </tr>)}</tbody></table>}
    {data && data.count > data.results.length && <div className="muted small" style={{ padding: '8px 14px' }}>Showing {data.results.length} of {data.count}. <a href={plUrl} target="_blank" rel="noreferrer">See all in Paperless</a>.</div>}
  </Card>
}
