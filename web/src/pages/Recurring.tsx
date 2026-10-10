import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { BILLING_MODES, fmtDate, money, today, UNITS, unitForBilling } from '../lib/format'
import type { Client, InvoiceTemplate, Recurring, RecurringItem, TaxRate } from '../lib/types'
import { Badge, Card, Confirm, Empty, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'
import { computeTotals } from './InvoiceEditor'

const FREQ = [['weekly', 'Weekly'], ['biweekly', 'Every 2 weeks'], ['monthly', 'Monthly'], ['quarterly', 'Quarterly'], ['yearly', 'Yearly'], ['daily', 'Daily']]

function RecurringForm({ initial, clients, onSaved, onClose }: { initial?: Recurring; clients: Client[]; onSaved: () => void; onClose: () => void }) {
  const { settings, currencies } = useApp()
  const toast = useToast()
  const { data: taxRates } = useAsync(() => api.get<TaxRate[]>(`${V1}/tax-rates`))
  const { data: templates } = useAsync(() => api.get<InvoiceTemplate[]>(`${V1}/templates`))
  const [r, setR] = useState<Partial<Recurring>>(initial ?? { name: '', client_id: clients[0]?.id || 0, status: 'active', frequency: 'monthly', interval: 1, start_date: today(), end_date: null, next_run: today(), max_occurrences: 0, due_days: settings?.default_due_days || 14, currency: clients[0]?.currency || settings?.base_currency, billing_mode: 'monthly', items: [{ description: 'Monthly retainer – {month}', unit: 'month', quantity: 1, unit_price: clients[0]?.default_rate || settings?.default_monthly_rate || 0, tax_rate: settings?.default_tax_rate || 0, discount: 0 }], discount_type: 'none', discount_value: 0, notes: settings?.default_notes || '', terms: (settings?.default_terms || '').replace('{due_days}', String(settings?.default_due_days || 14)), auto_send: false, template_id: null, quantity_mode: 'fixed', period_mode: 'forward' })
  const [busy, setBusy] = useState(false)
  const set = (k: keyof Recurring, v: unknown) => setR(x => ({ ...x, [k]: v }))
  const items = r.items || []
  const setItem = (i: number, p: Partial<RecurringItem>) => set('items', items.map((it, idx) => idx === i ? { ...it, ...p } : it))
  const totals = computeTotals(items, r.discount_type || 'none', r.discount_value || 0)
  const onClient = (cid: number) => { const c = clients.find(x => x.id === cid); setR(x => ({ ...x, client_id: cid, currency: c?.currency || x.currency, due_days: c?.payment_terms_days || x.due_days, billing_mode: c?.billing_mode || x.billing_mode })) }
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { ...r, end_date: r.end_date || null, template_id: r.template_id || null }
      if (initial) await api.put(`${V1}/recurring/${initial.id}`, body); else await api.post(`${V1}/recurring`, body)
      toast('Recurring profile saved', 'success'); onSaved()
    } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit} noValidate>
      <div className="form-grid cols-3">
        <Field label="Client"><select value={r.client_id} onChange={e => onClient(Number(e.target.value))}>{clients.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field>
        <Field label="Profile name" className="full" help="Internal only"><input value={r.name} onChange={e => set('name', e.target.value)} placeholder="e.g. Acme hosting retainer" /></Field>
        <Field label="Frequency"><select value={r.frequency} onChange={e => set('frequency', e.target.value)}>{FREQ.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></Field>
        <Field label="Every N periods"><input type="number" min={1} value={r.interval} onChange={e => set('interval', Number(e.target.value) || 1)} /></Field>
        <Field label="Status"><select value={r.status} onChange={e => set('status', e.target.value)}><option value="active">Active</option><option value="paused">Paused</option><option value="completed">Completed</option></select></Field>
        <Field label="Start date"><input type="date" value={r.start_date} onChange={e => { set('start_date', e.target.value); if (!initial) set('next_run', e.target.value) }} /></Field>
        <Field label="Next run" help="Invoice is generated on this date"><input type="date" value={r.next_run} onChange={e => set('next_run', e.target.value)} /></Field>
        <Field label="End date (optional)"><input type="date" value={r.end_date || ''} onChange={e => set('end_date', e.target.value || null)} /></Field>
        <Field label="Max occurrences" help="0 = unlimited"><input type="number" min={0} value={r.max_occurrences} onChange={e => set('max_occurrences', Number(e.target.value) || 0)} /></Field>
        <Field label="Due after (days)"><input type="number" min={0} value={r.due_days} onChange={e => set('due_days', Number(e.target.value) || 0)} /></Field>
        <Field label="Currency"><select value={r.currency} onChange={e => set('currency', e.target.value)}>{currencies.filter(c => c.enabled).map(c => <option key={c.code} value={c.code}>{c.code}</option>)}</select></Field>
        <Field label="Billing mode"><select value={r.billing_mode} onChange={e => set('billing_mode', e.target.value)}>{BILLING_MODES.map(b => <option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
        <Field label="Template"><select value={r.template_id || ''} onChange={e => set('template_id', Number(e.target.value) || null)}><option value="">Default</option>{templates?.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</select></Field>
        <Field label="Service period" help={r.period_mode === 'arrears' ? 'Run on Nov 1 bills October (previous period)' : 'Run on Nov 1 bills November (period starts on the run date)'}><select value={r.period_mode || 'forward'} onChange={e => set('period_mode', e.target.value)}><option value="forward">Period starts on run date</option><option value="arrears">Previous period (in arrears)</option></select></Field>
        <Field label="Quantity" help={r.quantity_mode === 'working_days' ? <>Day lines get the working days of the period, hour lines working days × {settings?.hours_per_day || 8} h (<Link to="/settings/invoicing">calendar</Link>)</> : 'Quantities are taken as entered below'}><select value={r.quantity_mode || 'fixed'} onChange={e => set('quantity_mode', e.target.value)}><option value="fixed">Fixed (as entered)</option><option value="working_days">Working days of the period</option></select></Field>
        <Field label="Auto-send" help="Email the invoice automatically when generated"><label className="check" style={{ marginTop: 8 }}><input type="checkbox" checked={!!r.auto_send} onChange={e => set('auto_send', e.target.checked)} /> Send by email on generation</label></Field>
      </div>
      <h3 className="mt mb">Line items <span className="muted small" style={{ fontWeight: 400 }}>— placeholders: {'{month} {year} {period}'}</span></h3>
      <div className="table-wrap"><table className="table items-table">
        <thead><tr><th>Description</th><th style={{ width: 90 }}>Unit</th><th style={{ width: 80 }}>Qty</th><th style={{ width: 110 }}>Rate</th><th style={{ width: 110 }}>Tax</th><th className="num">Amount</th><th></th></tr></thead>
        <tbody>{items.map((it, i) => <tr key={i}>
          <td><input value={it.description} onChange={e => setItem(i, { description: e.target.value })} /></td>
          <td><select value={it.unit} onChange={e => setItem(i, { unit: e.target.value })}>{UNITS.map(u => <option key={u.value} value={u.value}>{u.label}</option>)}</select></td>
          <td><input type="number" step="1" value={it.quantity} onChange={e => setItem(i, { quantity: parseFloat(e.target.value) || 0 })} /></td>
          <td><input type="number" step="0.01" value={it.unit_price} onChange={e => setItem(i, { unit_price: parseFloat(e.target.value) || 0 })} /></td>
          <td><select value={it.tax_rate} onChange={e => setItem(i, { tax_rate: parseFloat(e.target.value) })}>{!taxRates?.some(t => t.rate === it.tax_rate) && <option value={it.tax_rate}>{it.tax_rate}%</option>}{taxRates?.map(t => <option key={t.id} value={t.rate}>{t.name}</option>)}{!taxRates?.some(t => t.rate === 0) && <option value={0}>No tax</option>}</select></td>
          <td className="num">{money(totals.lines[i], r.currency!)}</td>
          <td><button type="button" className="btn ghost sm" onClick={() => set('items', items.filter((_, j) => j !== i))}>✕</button></td>
        </tr>)}</tbody>
      </table></div>
      <div className="row between mt">
        <button type="button" className="btn" onClick={() => set('items', [...items, { description: '', unit: unitForBilling(r.billing_mode || 'monthly'), quantity: 1, unit_price: 0, tax_rate: settings?.default_tax_rate || 0, discount: 0 }])}>+ Add line</button>
        <div className="bold">Total per invoice: {money(totals.total, r.currency!)}</div>
      </div>
      <div className="form-grid mt">
        <Field label="Notes"><textarea value={r.notes} onChange={e => set('notes', e.target.value)} /></Field>
        <Field label="Terms"><textarea value={r.terms} onChange={e => set('terms', e.target.value)} /></Field>
      </div>
      <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{initial ? 'Save' : 'Create profile'}</button></div>
    </form>
  )
}

export default function RecurringPage() {
  const toast = useToast()
  const navigate = useNavigate()
  const { data, loading, reload } = useAsync(() => api.get<Recurring[]>(`${V1}/recurring`))
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients`))
  const [editing, setEditing] = useState<Recurring | 'new' | null>(null)
  const [del, setDel] = useState<Recurring | null>(null)
  const run = async (r: Recurring) => { try { const inv = await api.post<{ id: number; number: string }>(`${V1}/recurring/${r.id}/run`); toast(`Generated ${inv.number}`, 'success'); reload(); navigate(`/invoices/${inv.id}`) } catch (e) { toast((e as Error).message, 'error') } }
  const toggle = async (r: Recurring) => { await api.put(`${V1}/recurring/${r.id}`, { ...r, status: r.status === 'active' ? 'paused' : 'active' }); reload() }
  return (
    <>
      <PageHeader title="Recurring invoices" sub="Retainers and subscriptions, generated automatically on schedule." actions={<button className="btn primary" disabled={!clients?.length} onClick={() => setEditing('new')}>+ New profile</button>} />
      <Card flush>
        {loading ? <Loading /> : !data?.length ? <Empty title="No recurring profiles">Monthly retainer? Set it up once and let the scheduler do the rest.</Empty> : <div className="table-wrap"><table className="table">
          <thead><tr><th>Name</th><th>Client</th><th>Schedule</th><th>Next run</th><th>Last run</th><th className="num">Amount</th><th>Status</th><th></th></tr></thead>
          <tbody>{data.map(r => { const t = computeTotals(r.items, r.discount_type, r.discount_value); return <tr key={r.id}>
            <td className="bold">{r.name}{r.auto_send && <span className="badge accent" style={{ marginLeft: 8 }}>auto-send</span>}</td>
            <td>{r.client_name}</td>
            <td>{FREQ.find(f => f[0] === r.frequency)?.[1]}{r.interval > 1 ? ` ×${r.interval}` : ''}{r.period_mode === 'arrears' && <span className="muted"> · in arrears</span>}{r.quantity_mode === 'working_days' && <span className="muted"> · working days</span>}<div className="muted small">{r.occurrences} generated{r.max_occurrences ? ` of ${r.max_occurrences}` : ''}{r.end_date ? ` · ends ${fmtDate(r.end_date)}` : ''}</div></td>
            <td>{fmtDate(r.next_run)}</td><td className="muted">{fmtDate(r.last_run)}</td>
            <td className="num">{money(t.total, r.currency)}</td>
            <td><Badge status={r.status} /></td>
            <td className="actions"><button className="btn sm" onClick={() => run(r)}>Run now</button> <button className="btn sm" onClick={() => toggle(r)}>{r.status === 'active' ? 'Pause' : 'Resume'}</button> <button className="btn sm" onClick={() => setEditing(r)}>Edit</button> <button className="btn ghost sm" onClick={() => setDel(r)}>✕</button></td>
          </tr> })}</tbody>
        </table></div>}
      </Card>
      {editing && clients && <Modal title={editing === 'new' ? 'New recurring profile' : `Edit ${editing.name}`} size="xl" onClose={() => setEditing(null)}><RecurringForm initial={editing === 'new' ? undefined : editing} clients={clients} onSaved={() => { setEditing(null); reload() }} onClose={() => setEditing(null)} /></Modal>}
      {del && <Confirm title="Delete profile?" message="Already generated invoices are kept." onConfirm={async () => { await api.del(`${V1}/recurring/${del.id}`); setDel(null); reload() }} onCancel={() => setDel(null)} />}
    </>
  )
}
