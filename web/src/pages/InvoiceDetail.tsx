import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fileSize, fmtDate, fmtDateTime, money, PAYMENT_METHODS, today } from '../lib/format'
import type { Activity, Invoice, Payment } from '../lib/types'
import { Badge, Card, Confirm, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'

function PaymentForm({ inv, onDone }: { inv: Invoice; onDone: () => void }) {
  const toast = useToast()
  const { currencies } = useApp()
  const [p, setP] = useState({ invoice_id: inv.id, date: today(), amount: inv.balance, currency: inv.currency, exchange_rate: 0, method: 'bank_transfer', reference: '', notes: '' })
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.post(`${V1}/payments`, p); toast('Payment recorded', 'success'); onDone() } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit}>
      <div className="form-grid">
        <Field label="Amount received"><input type="number" step="0.01" value={p.amount} onChange={e => setP({ ...p, amount: parseFloat(e.target.value) || 0 })} autoFocus /></Field>
        <Field label="Currency" help={p.currency !== inv.currency ? `Converted to ${inv.currency} using the rate below` : undefined}><select value={p.currency} onChange={e => setP({ ...p, currency: e.target.value })}>{currencies.filter(c => c.enabled || c.code === inv.currency).map(c => <option key={c.code} value={c.code}>{c.code}</option>)}</select></Field>
        {p.currency !== inv.currency && <Field label={`Rate: 1 ${p.currency} = ? ${inv.currency}`} help="Leave 0 to use the stored rate" className="full"><input type="number" step="0.000001" value={p.exchange_rate || ''} onChange={e => setP({ ...p, exchange_rate: parseFloat(e.target.value) || 0 })} /></Field>}
        <Field label="Date"><input type="date" value={p.date} onChange={e => setP({ ...p, date: e.target.value })} /></Field>
        <Field label="Method"><select value={p.method} onChange={e => setP({ ...p, method: e.target.value })}>{PAYMENT_METHODS.map(m => <option key={m} value={m}>{m.replace('_', ' ')}</option>)}</select></Field>
        <Field label="Reference"><input value={p.reference} onChange={e => setP({ ...p, reference: e.target.value })} placeholder="Transaction id, cheque #…" /></Field>
        <Field label="Notes"><input value={p.notes} onChange={e => setP({ ...p, notes: e.target.value })} /></Field>
      </div>
      <div className="form-actions"><button className="btn primary" disabled={busy}>Record payment</button></div>
    </form>
  )
}

function SendForm({ inv, onDone }: { inv: Invoice; onDone: () => void }) {
  const toast = useToast()
  const { settings } = useApp()
  const [to, setTo] = useState('')
  const [subject, setSubject] = useState(settings?.email_subject || '')
  const [body, setBody] = useState(settings?.email_body || '')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api.post(`${V1}/invoices/${inv.id}/send`, { to, subject, body }); toast('Invoice emailed', 'success'); onDone() } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit}>
      {!settings?.smtp_host && <div className="callout warn mb">SMTP is not configured. Set it up under <Link to="/settings/email">Settings → Email</Link>, or share the public link instead.</div>}
      <div className="grid" style={{ gap: 12 }}>
        <Field label="To" help="Leave blank to use the client's email"><input value={to} onChange={e => setTo(e.target.value)} placeholder="client@example.com" /></Field>
        <Field label="Subject"><input value={subject} onChange={e => setSubject(e.target.value)} /></Field>
        <Field label="Message" help="Placeholders: {number} {client} {company} {total} {balance} {due_date} {link}"><textarea rows={8} value={body} onChange={e => setBody(e.target.value)} /></Field>
      </div>
      <div className="form-actions"><button className="btn primary" disabled={busy || !settings?.smtp_host}>Send with PDF attached</button></div>
    </form>
  )
}

export default function InvoiceDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { settings } = useApp()
  const { data: inv, loading, reload, setData } = useAsync(() => api.get<Invoice>(`${V1}/invoices/${id}`), [id])
  const { data: activity, reload: reloadAct } = useAsync(() => api.get<Activity[]>(`${V1}/activity?entity_type=invoice&entity_id=${id}&limit=20`), [id])
  const [modal, setModal] = useState<'' | 'payment' | 'send' | 'delete' | 'cancel'>('')
  if (loading || !inv) return <Loading />
  const publicUrl = `${window.location.origin}/i/${inv.public_token}`
  const setStatus = async (status: string) => {
    try { const r = await api.post<Invoice>(`${V1}/invoices/${inv.id}/status`, { status }); setData(r); reloadAct(); toast(`Marked as ${status}`, 'success') } catch (e) { toast((e as Error).message, 'error') }
  }
  const dup = async () => { const r = await api.post<Invoice>(`${V1}/invoices/${inv.id}/duplicate`); toast(`Duplicated as ${r.number}`, 'success'); navigate(`/invoices/${r.id}`) }
  const del = async () => { await api.del(`${V1}/invoices/${inv.id}`); toast('Invoice deleted', 'success'); navigate('/invoices') }
  const delPayment = async (p: Payment) => { if (!confirm('Delete this payment?')) return; await api.del(`${V1}/payments/${p.id}`); reload(); reloadAct() }
  const copyLink = () => navigator.clipboard.writeText(publicUrl).then(() => toast('Public link copied', 'success'))
  const base = settings?.base_currency
  const paidPct = inv.total > 0 ? Math.min(100, (inv.amount_paid / inv.total) * 100) : 0
  return (
    <>
      <PageHeader title={<span className="row">{inv.number} <Badge status={inv.status} /></span>} sub={<><Link to={`/clients/${inv.client_id}`}>{inv.client_name}</Link> · issued {fmtDate(inv.issue_date)} · due {fmtDate(inv.due_date)}</>} actions={<>
        <a className="btn" href={`${V1}/invoices/${inv.id}/pdf`} target="_blank" rel="noreferrer">View PDF</a>
        <a className="btn" href={`${V1}/invoices/${inv.id}/pdf?download=1`}>Download</a>
        {inv.status !== 'paid' && inv.status !== 'cancelled' && <button className="btn" onClick={() => setModal('send')}>Email</button>}
        {inv.status === 'draft' && <button className="btn primary" onClick={() => setStatus('sent')}>Mark as sent</button>}
        {['sent', 'viewed', 'partial', 'overdue'].includes(inv.status) && <button className="btn primary" onClick={() => setModal('payment')}>Record payment</button>}
      </>} />
      <div className="grid cols-2 mb" style={{ gridTemplateColumns: '2fr 1fr' }}>
        <div className="grid">
          <Card flush>
            <div className="table-wrap"><table className="table">
              <thead><tr><th>Description</th><th>Unit</th><th className="num">Qty</th><th className="num">Rate</th><th className="num">Tax</th><th className="num">Amount</th></tr></thead>
              <tbody>{inv.items?.map(it => <tr key={it.id}><td style={{ whiteSpace: 'pre-wrap' }}>{it.description}</td><td className="muted">{it.unit}</td><td className="num">{it.quantity}{it.discount ? <div className="muted small">-{it.discount}%</div> : null}</td><td className="num">{money(it.unit_price, inv.currency)}</td><td className="num muted">{it.tax_rate ? `${it.tax_rate}%` : '—'}</td><td className="num">{money(it.line_total, inv.currency)}</td></tr>)}</tbody>
            </table></div>
            <div style={{ padding: 16 }} className="row" >
              <div style={{ flex: 1 }}>
                {inv.period_start && <div className="muted small">Service period: {fmtDate(inv.period_start)} – {fmtDate(inv.period_end)}</div>}
                {inv.po_number && <div className="muted small">PO: {inv.po_number}</div>}
                {(settings?.custom_fields || []).filter(f => inv.custom_fields?.[f.key]).map(f => <div key={f.key} className="muted small">{f.label}: {inv.custom_fields[f.key]}</div>)}
                <div className="muted small">Billing mode: {inv.billing_mode}</div>
              </div>
              <div className="totals-box">
                <div className="line"><span className="muted">Subtotal</span><span>{money(inv.subtotal, inv.currency)}</span></div>
                {inv.discount_total > 0 && <div className="line"><span className="muted">Discount</span><span>-{money(inv.discount_total, inv.currency)}</span></div>}
                {inv.tax_total > 0 && <div className="line"><span className="muted">Tax</span><span>{money(inv.tax_total, inv.currency)}</span></div>}
                <div className="line grand"><span>Total</span><span>{money(inv.total, inv.currency)}</span></div>
                {inv.amount_paid > 0 && <div className="line"><span className="muted">Paid</span><span>-{money(inv.amount_paid, inv.currency)}</span></div>}
                {inv.amount_paid > 0 && <div className="line bold"><span>Balance due</span><span>{money(inv.balance, inv.currency)}</span></div>}
                {base && inv.currency !== base && <div className="line muted small"><span>≈ {base} @ {inv.exchange_rate.toFixed(4)}</span><span>{money(inv.total * inv.exchange_rate, base)}</span></div>}
              </div>
            </div>
          </Card>
          {(inv.notes || inv.terms) && <Card><div className="grid cols-2">{inv.notes && <div><div className="muted small bold">NOTES</div><p style={{ whiteSpace: 'pre-wrap' }}>{inv.notes}</p></div>}{inv.terms && <div><div className="muted small bold">TERMS</div><p style={{ whiteSpace: 'pre-wrap' }}>{inv.terms}</p></div>}</div></Card>}
          <Card title="Attachments" actions={<label className="btn sm">+ Upload<input type="file" multiple style={{ display: 'none' }} onChange={async e => { if (!e.target.files?.length) return; const fd = new FormData(); for (const f of Array.from(e.target.files)) fd.append('file', f); try { await api.post(`${V1}/invoices/${inv.id}/attachments`, fd); toast('Uploaded', 'success'); reload(); reloadAct() } catch (err) { toast((err as Error).message, 'error') } }} /></label>} flush>
            {!inv.attachments?.length ? <div className="empty muted">No files attached. Upload your own PDF (e.g. a signed or fiscalised version); emails can send it instead of the generated PDF (Settings → Invoicing).</div> : <table className="table"><tbody>{inv.attachments.map(a => <tr key={a.id}><td><a href={`${V1}/attachments/${a.id}`} target="_blank" rel="noreferrer">{a.filename}</a><div className="muted small">{fileSize(a.size)} · {fmtDateTime(a.created_at)}</div></td><td className="actions"><a className="btn ghost sm" href={`${V1}/attachments/${a.id}?download=1`}>↓</a><button className="btn ghost sm" onClick={async () => { if (!confirm(`Remove ${a.filename}?`)) return; await api.del(`${V1}/attachments/${a.id}`); reload() }}>✕</button></td></tr>)}</tbody></table>}
          </Card>
          <Card title="Payments" actions={['sent', 'viewed', 'partial', 'overdue'].includes(inv.status) ? <button className="btn sm" onClick={() => setModal('payment')}>+ Add</button> : undefined} flush>
            {inv.amount_paid > 0 && <div style={{ padding: '12px 16px 0' }}><div className="progress"><div style={{ width: `${paidPct}%` }} /></div><div className="muted small mt" style={{ marginTop: 6 }}>{paidPct.toFixed(0)}% paid</div></div>}
            {!inv.payments?.length ? <div className="empty muted">No payments recorded.</div> : <table className="table"><thead><tr><th>Date</th><th>Method</th><th>Reference</th><th className="num">Amount</th><th></th></tr></thead>
              <tbody>{inv.payments.map(p => <tr key={p.id}><td>{fmtDate(p.date)}</td><td className="muted">{p.method.replace('_', ' ')}</td><td className="muted">{p.reference}{p.notes ? ` · ${p.notes}` : ''}</td><td className="num">{money(p.amount, p.currency)}{p.currency !== inv.currency && <div className="muted small">= {money(p.applied_amount, inv.currency)}</div>}</td><td className="actions"><button className="btn ghost sm" onClick={() => delPayment(p)}>✕</button></td></tr>)}</tbody></table>}
          </Card>
        </div>
        <div className="grid">
          <Card title="Actions">
            <div className="grid" style={{ gap: 8 }}>
              {inv.status === 'draft' && <Link className="btn" to={`/invoices/${inv.id}/edit`}>Edit invoice</Link>}
              {inv.status !== 'draft' && inv.status !== 'cancelled' && <Link className="btn" to={`/invoices/${inv.id}/edit`}>Edit (keeps status)</Link>}
              <a className="btn" href={`${V1}/invoices/${inv.id}/docx`} title="Only works when the invoice (or its client) uses a Word template">Download as Word (.docx)</a>
              <button className="btn" onClick={copyLink}>Copy public link</button>
              <a className="btn" href={publicUrl} target="_blank" rel="noreferrer">Open public page</a>
              <button className="btn" onClick={dup}>Duplicate</button>
              {['sent', 'viewed', 'partial', 'overdue'].includes(inv.status) && <button className="btn" onClick={() => setStatus('paid')}>Mark fully paid</button>}
              {inv.status !== 'draft' && inv.status !== 'paid' && <button className="btn" onClick={() => setStatus('draft')}>Back to draft</button>}
              {inv.status === 'paid' && <button className="btn" onClick={() => setStatus('sent')}>Reopen (unpaid)</button>}
              {inv.status !== 'cancelled' && inv.status !== 'paid' && <button className="btn" onClick={() => setModal('cancel')}>Cancel invoice</button>}
              <button className="btn danger" onClick={() => setModal('delete')}>Delete</button>
            </div>
          </Card>
          <Card title="Timeline">
            <dl className="kv">
              <dt>Created</dt><dd>{fmtDateTime(inv.created_at)}</dd>
              {inv.sent_at && <><dt>Sent</dt><dd>{fmtDateTime(inv.sent_at)}</dd></>}
              {inv.viewed_at && <><dt>Viewed</dt><dd>{fmtDateTime(inv.viewed_at)}</dd></>}
              {inv.paid_at && <><dt>Paid</dt><dd>{fmtDateTime(inv.paid_at)}</dd></>}
              {inv.recurring_id && <><dt>Recurring</dt><dd><Link to="/recurring">profile #{inv.recurring_id}</Link></dd></>}
            </dl>
            {activity && activity.length > 0 && <><hr />{activity.map(a => <div key={a.id} className="small" style={{ marginBottom: 6 }}><span className="muted">{fmtDateTime(a.created_at)}</span> — {a.message}</div>)}</>}
          </Card>
        </div>
      </div>
      {modal === 'payment' && <Modal title={`Record payment for ${inv.number}`} onClose={() => setModal('')}><PaymentForm inv={inv} onDone={() => { setModal(''); reload(); reloadAct() }} /></Modal>}
      {modal === 'send' && <Modal title={`Email ${inv.number}`} onClose={() => setModal('')}><SendForm inv={inv} onDone={() => { setModal(''); reload(); reloadAct() }} /></Modal>}
      {modal === 'delete' && <Confirm title="Delete invoice?" message="This permanently deletes the invoice and its payments. Linked time entries become unbilled again." onConfirm={del} onCancel={() => setModal('')} />}
      {modal === 'cancel' && <Confirm title="Cancel invoice?" message="The invoice will be marked cancelled and excluded from reports." onConfirm={() => { setStatus('cancelled'); setModal('') }} onCancel={() => setModal('')} />}
    </>
  )
}
