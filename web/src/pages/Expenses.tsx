import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, money, today } from '../lib/format'
import type { Client, Expense } from '../lib/types'
import { Card, Empty, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'

const CATEGORIES = ['Software', 'Hardware', 'Hosting', 'Travel', 'Office', 'Subcontractor', 'Other']

function ExpenseForm({ initial, clients, onDone }: { initial?: Expense; clients: Client[]; onDone: () => void }) {
  const toast = useToast()
  const { settings, currencies } = useApp()
  const [e, setE] = useState({ client_id: initial?.client_id ?? 0, date: initial?.date || today(), category: initial?.category || 'Software', description: initial?.description || '', amount: initial?.amount || 0, currency: initial?.currency || settings?.base_currency || 'EUR', exchange_rate: initial?.exchange_rate || 0, billable: initial?.billable ?? false })
  const [busy, setBusy] = useState(false)
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setBusy(true)
    try { const body = { ...e, client_id: e.client_id || null }; if (initial) await api.put(`${V1}/expenses/${initial.id}`, body); else await api.post(`${V1}/expenses`, body); toast('Saved', 'success'); onDone() } catch (err) { toast((err as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit}>
      <div className="form-grid">
        <Field label="Description" className="full"><input value={e.description} onChange={ev => setE({ ...e, description: ev.target.value })} autoFocus required /></Field>
        <Field label="Amount"><input type="number" step="0.01" value={e.amount} onChange={ev => setE({ ...e, amount: parseFloat(ev.target.value) || 0 })} required /></Field>
        <Field label="Currency"><select value={e.currency} onChange={ev => setE({ ...e, currency: ev.target.value })}>{currencies.filter(c => c.enabled).map(c => <option key={c.code} value={c.code}>{c.code}</option>)}</select></Field>
        <Field label="Date"><input type="date" value={e.date} onChange={ev => setE({ ...e, date: ev.target.value })} /></Field>
        <Field label="Category"><select value={e.category} onChange={ev => setE({ ...e, category: ev.target.value })}>{CATEGORIES.map(c => <option key={c}>{c}</option>)}</select></Field>
        <Field label="Client (optional)"><select value={e.client_id ?? 0} onChange={ev => setE({ ...e, client_id: Number(ev.target.value) })}><option value={0}>— none —</option>{clients.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field>
        <Field label=" "><label className="check" style={{ marginTop: 8 }}><input type="checkbox" checked={e.billable} onChange={ev => setE({ ...e, billable: ev.target.checked })} /> Re-bill to client</label></Field>
      </div>
      <div className="form-actions"><button className="btn primary" disabled={busy}>Save</button></div>
    </form>
  )
}

export default function Expenses() {
  const navigate = useNavigate()
  const { settings } = useApp()
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients`))
  const { data, loading, reload } = useAsync(() => api.get<Expense[]>(`${V1}/expenses`))
  const [editing, setEditing] = useState<Expense | 'new' | null>(null)
  const base = settings?.base_currency || ''
  const total = (data || []).reduce((a, e) => a + e.amount * e.exchange_rate, 0)
  return (
    <>
      <PageHeader title="Expenses" sub="Track costs; billable ones can be added to the next invoice." actions={<><a className="btn" href={`${V1}/reports/export.csv?type=expenses`}>Export CSV</a><button className="btn primary" onClick={() => setEditing('new')}>+ Add expense</button></>} />
      <Card flush>
        {loading ? <Loading /> : !data?.length ? <Empty title="No expenses recorded" /> : <div className="table-wrap"><table className="table">
          <thead><tr><th>Date</th><th>Description</th><th>Category</th><th>Client</th><th className="num">Amount</th><th>Status</th><th></th></tr></thead>
          <tbody>{data.map(e => <tr key={e.id}><td className="muted">{fmtDate(e.date)}</td><td>{e.description}</td><td className="muted">{e.category}</td><td>{e.client_name || <span className="muted">—</span>}</td><td className="num">{money(e.amount, e.currency)}{e.currency !== base && <div className="muted small">≈ {money(e.amount * e.exchange_rate, base)}</div>}</td>
            <td>{e.invoice_id ? <span className="badge paid" style={{ cursor: 'pointer' }} onClick={() => navigate(`/invoices/${e.invoice_id}`)}>invoiced</span> : e.billable ? <span className="badge sent">billable</span> : <span className="badge">cost</span>}</td>
            <td className="actions">{!e.invoice_id && <><button className="btn ghost sm" onClick={() => setEditing(e)}>Edit</button><button className="btn ghost sm" onClick={async () => { if (confirm('Delete?')) { await api.del(`${V1}/expenses/${e.id}`); reload() } }}>✕</button></>}</td></tr>)}</tbody>
          <tfoot><tr><td colSpan={4}>Total</td><td className="num">{money(total, base)}</td><td colSpan={2}></td></tr></tfoot>
        </table></div>}
      </Card>
      {editing && clients && <Modal title={editing === 'new' ? 'New expense' : 'Edit expense'} onClose={() => setEditing(null)}><ExpenseForm initial={editing === 'new' ? undefined : editing} clients={clients} onDone={() => { setEditing(null); reload() }} /></Modal>}
    </>
  )
}
