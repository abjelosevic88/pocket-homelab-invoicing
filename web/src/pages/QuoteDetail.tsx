import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, fmtDateTime, money } from '../lib/format'
import type { Activity, Client, Invoice, Quote } from '../lib/types'
import { Badge, Card, Confirm, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'
import { useCrumb } from '../components/shell'

function QuoteSendForm({ q, onDone }: { q: Quote; onDone: () => void }) {
  const toast = useToast()
  const { settings } = useApp()
  const { data: clientInfo } = useAsync(() => api.get<{ client: Client }>(`${V1}/clients/${q.client_id}`), [q.client_id])
  const client = clientInfo?.client
  const [to, setTo] = useState('')
  const [subject, setSubject] = useState(settings?.quote_email_subject || '')
  const [body, setBody] = useState(settings?.quote_email_body || '')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.post(`${V1}/quotes/${q.id}/send`, { to, subject, body }); toast('Quote emailed', 'success'); onDone() } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit}>
      {!settings?.smtp_host && <div className="callout warn mb">Email is not set up. Set it up under <Link to="/settings/email">Settings → Email</Link>, or copy the link and send it yourself.</div>}
      <div className="grid" style={{ gap: 12 }}>
        <Field label="To" help={`Leave blank to use the client's email${client?.email ? ` (${client.email})` : ''}${client?.email_cc ? `, CC ${client.email_cc}` : ''}`}><input value={to} onChange={e => setTo(e.target.value)} placeholder="client@example.com" /></Field>
        <Field label="Subject"><input value={subject} onChange={e => setSubject(e.target.value)} /></Field>
        <Field label="Message" help={<>Placeholders: {'{number} {client} {contact} {first_name} {company} {total} {valid_until} {issue_date} {link}'} · {'{link}'} is where they accept or decline. Default text: <Link to="/settings/email">Settings → Email</Link></>}><textarea rows={8} value={body} onChange={e => setBody(e.target.value)} /></Field>
      </div>
      <div className="form-actions"><button type="submit" className="btn primary" disabled={busy || !settings?.smtp_host}>{busy ? 'Sending…' : 'Send with PDF attached'}</button></div>
    </form>
  )
}

function DeclineForm({ onSubmit }: { onSubmit: (reason: string) => void }) {
  const [reason, setReason] = useState('')
  return (
    <form onSubmit={e => { e.preventDefault(); onSubmit(reason) }}>
      <Field label="Reason (optional)"><textarea rows={3} value={reason} onChange={e => setReason(e.target.value)} placeholder="e.g. went with another offer" autoFocus /></Field>
      <div className="form-actions"><button type="submit" className="btn danger">Mark declined</button></div>
    </form>
  )
}

export default function QuoteDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { data: q, loading, reload, setData } = useAsync(() => api.get<Quote>(`${V1}/quotes/${id}`), [id])
  const { data: activity, reload: reloadAct } = useAsync(() => api.get<Activity[]>(`${V1}/activity?entity_type=quote&entity_id=${id}&limit=30`), [id])
  const [modal, setModal] = useState<'' | 'send' | 'decline' | 'delete' | 'accept'>('')
  const [busy, setBusy] = useState(false)
  useCrumb(q?.number)
  if (loading || !q) return <Loading />
  const publicUrl = `${window.location.origin}/q/${q.public_token}`
  const open = q.status === 'sent' || q.status === 'viewed'
  const done = q.status === 'accepted' || q.status === 'invoiced'
  const run = async (fn: () => Promise<void>) => { setBusy(true); try { await fn() } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) } }
  const setStatus = (status: string, reason = '') => run(async () => { const r = await api.post<Quote>(`${V1}/quotes/${q.id}/status`, { status, reason }); setData(r); reloadAct(); setModal(''); toast(status === 'sent' ? 'Marked as sent' : status === 'draft' ? 'Back to draft' : 'Marked as declined', 'success') })
  const convert = () => run(async () => { const r = await api.post<{ quote: Quote; invoice: Invoice }>(`${V1}/quotes/${q.id}/convert`); setModal(''); toast(`Draft invoice ${r.invoice.number} made — check it and send it`, 'success'); navigate(`/invoices/${r.invoice.id}`) })
  const dup = () => run(async () => { const r = await api.post<Quote>(`${V1}/quotes/${q.id}/duplicate`); toast(`Duplicated as ${r.number}`, 'success'); navigate(`/quotes/${r.id}`) })
  const del = () => run(async () => { await api.del(`${V1}/quotes/${q.id}`); toast('Quote deleted', 'success'); navigate('/quotes') })
  const copyLink = () => navigator.clipboard.writeText(publicUrl).then(() => toast('Link copied', 'success'))
  return (
    <>
      <PageHeader title={<span className="row">{q.number} <Badge status={q.status} />{q.revision > 1 && <span className="muted small" style={{ fontWeight: 400 }}>rev {q.revision}</span>}</span>} sub={<><Link to={`/clients/${q.client_id}`}>{q.client_name}</Link> · {fmtDate(q.issue_date)} · valid until {fmtDate(q.valid_until)}</>} actions={<>
        <a className="btn" href={`${V1}/quotes/${q.id}/pdf`} target="_blank" rel="noreferrer">View PDF</a>
        <a className="btn" href={`${V1}/quotes/${q.id}/pdf?download=1`}>Download</a>
        {(q.status === 'draft' || open) && <button type="button" className="btn" onClick={() => setModal('send')}>Email</button>}
        {q.status === 'draft' && <button type="button" className="btn" disabled={busy} onClick={() => setStatus('sent')} title="You sent it another way: the link starts working">Mark as sent</button>}
        {q.status === 'invoiced' && q.invoice_id && <Link className="btn primary" to={`/invoices/${q.invoice_id}`}>Open invoice {q.invoice_number}</Link>}
        {q.status === 'accepted' && <button type="button" className="btn primary" disabled={busy} onClick={convert}>Make draft invoice</button>}
        {!done && q.status !== 'draft' && <button type="button" className="btn primary" disabled={busy} onClick={() => setModal('accept')}>Accepted? Make invoice</button>}
      </>} />

      {q.status === 'draft' && <div className="callout info mb">This is a draft: the client's link doesn't work yet. Email it, or mark it as sent and share the link yourself.</div>}
      {open && <div className="callout info mb">Waiting for {q.client_name}. They accept or decline at <a href={publicUrl} target="_blank" rel="noreferrer">their link</a>; accepting starts a draft invoice here for you to check and send.</div>}
      {done && <div className="callout ok mb">Accepted by <b>{q.accepted_name}</b> on {fmtDateTime(q.accepted_at || '')}{q.accepted_ip ? <span className="muted small"> · from {q.accepted_ip}</span> : null}.{q.status === 'invoiced' && q.invoice_id ? <> Draft invoice <Link to={`/invoices/${q.invoice_id}`}>{q.invoice_number}</Link> was made from it.</> : ' No invoice yet.'}</div>}
      {q.status === 'declined' && <div className="callout warn mb">Declined on {fmtDateTime(q.declined_at || '')}{q.decline_reason ? <>: “{q.decline_reason}”</> : '.'} Edit it to make a new revision you can send again.</div>}
      {q.status === 'expired' && <div className="callout warn mb">Expired on {fmtDate(q.valid_until)} without an answer. Edit it (e.g. a new valid-until date) to send a new revision.</div>}

      <div className="grid split-2-1 mb">
        <div className="grid">
          <Card flush>
            <div className="table-wrap"><table className="table">
              <thead><tr><th>Description</th><th>Unit</th><th className="num">Qty</th><th className="num">Rate</th><th className="num">Tax</th><th className="num">Amount</th></tr></thead>
              <tbody>{q.items?.map(it => <tr key={it.id}><td style={{ whiteSpace: 'pre-wrap' }}>{it.description}</td><td className="muted">{it.unit}</td><td className="num">{it.quantity}{it.discount ? <div className="muted small">-{it.discount}%</div> : null}</td><td className="num">{money(it.unit_price, q.currency)}</td><td className="num muted">{it.tax_rate ? `${it.tax_rate}%` : '—'}</td><td className="num">{money(it.line_total || 0, q.currency)}</td></tr>)}</tbody>
            </table></div>
            <div style={{ padding: 16 }} className="row">
              <div style={{ flex: 1 }}>
                {q.po_number && <div className="muted small">PO: {q.po_number}</div>}
                <div className="muted small">Billing mode: {q.billing_mode}</div>
              </div>
              <div className="totals-box">
                <div className="line"><span className="muted">Subtotal</span><span>{money(q.subtotal, q.currency)}</span></div>
                {q.discount_total > 0 && <div className="line"><span className="muted">Discount</span><span>-{money(q.discount_total, q.currency)}</span></div>}
                {q.tax_total > 0 && <div className="line"><span className="muted">Tax</span><span>{money(q.tax_total, q.currency)}</span></div>}
                <div className="line grand"><span>Total</span><span>{money(q.total, q.currency)}</span></div>
              </div>
            </div>
          </Card>
          {(q.notes || q.terms) && <Card><div className="grid cols-2">{q.notes && <div><div className="muted small bold">NOTES</div><p style={{ whiteSpace: 'pre-wrap' }}>{q.notes}</p></div>}{q.terms && <div><div className="muted small bold">TERMS</div><p style={{ whiteSpace: 'pre-wrap' }}>{q.terms.replace('{valid_until}', fmtDate(q.valid_until))}</p></div>}</div></Card>}
        </div>
        <div className="grid">
          <Card title="Actions">
            <div className="grid" style={{ gap: 8 }}>
              {!done && <Link className="btn" to={`/quotes/${q.id}/edit`}>{q.status === 'draft' ? 'Edit quote' : 'Edit (new revision)'}</Link>}
              {q.status !== 'draft' && <button type="button" className="btn" onClick={copyLink}>Copy client link</button>}
              {q.status !== 'draft' && <a className="btn" href={publicUrl} target="_blank" rel="noreferrer">Open client page</a>}
              <button type="button" className="btn" disabled={busy} onClick={dup}>Duplicate</button>
              {(open || q.status === 'expired') && <button type="button" className="btn" onClick={() => setModal('decline')}>Mark declined</button>}
              {!done && q.status !== 'draft' && <button type="button" className="btn" disabled={busy} onClick={() => setStatus('draft')}>Back to draft</button>}
              <button type="button" className="btn danger" onClick={() => setModal('delete')}>Delete</button>
            </div>
          </Card>
          <Card title="Timeline">
            <dl className="kv">
              <dt>Created</dt><dd>{fmtDateTime(q.created_at)}</dd>
              {q.sent_at && <><dt>Sent</dt><dd>{fmtDateTime(q.sent_at)}</dd></>}
              {q.viewed_at && <><dt>Viewed</dt><dd>{fmtDateTime(q.viewed_at)}</dd></>}
              {q.accepted_at && <><dt>Accepted</dt><dd>{fmtDateTime(q.accepted_at)}</dd></>}
              {q.declined_at && <><dt>Declined</dt><dd>{fmtDateTime(q.declined_at)}</dd></>}
              {q.invoice_id && <><dt>Invoice</dt><dd><Link to={`/invoices/${q.invoice_id}`}>{q.invoice_number}</Link></dd></>}
            </dl>
            {activity && activity.length > 0 && <><hr />{activity.map(a => <div key={a.id} className="small" style={{ marginBottom: 6 }}><span className="muted">{fmtDateTime(a.created_at)}</span> — {a.message}</div>)}</>}
          </Card>
        </div>
      </div>
      {modal === 'send' && <Modal title={`Email ${q.number}`} onClose={() => setModal('')}><QuoteSendForm q={q} onDone={() => { setModal(''); reload(); reloadAct() }} /></Modal>}
      {modal === 'decline' && <Modal title={`Mark ${q.number} declined`} onClose={() => setModal('')}><DeclineForm onSubmit={r => setStatus('declined', r)} /></Modal>}
      {modal === 'accept' && <Confirm title="Mark accepted and make the invoice?" danger={false} confirmLabel="Accept & make draft invoice" message={<>Use this when {q.client_name} said yes another way (phone, email). The quote is marked accepted by you and a <b>draft</b> invoice is made with its lines. Nothing is sent.</>} onConfirm={convert} onCancel={() => setModal('')} />}
      {modal === 'delete' && <Confirm title="Delete quote?" message={q.invoice_id ? 'The quote is deleted; the invoice made from it stays.' : "This permanently deletes the quote. The client's link stops working."} onConfirm={del} onCancel={() => setModal('')} />}
    </>
  )
}
