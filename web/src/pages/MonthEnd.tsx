import React, { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, money, today } from '../lib/format'
import type { Invoice, InvoiceItem, MonthEndPlan, MonthEndRow } from '../lib/types'
import { Badge, Card, Empty, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'
import { computeTotals } from './InvoiceEditor'
import { SendForm } from './InvoiceDetail'

/** Rows whose quantity is a number of days (or hours derived from days) get a calendar. */
function usesCalendar(r: MonthEndRow): boolean {
  return r.quantity_source !== 'time_entries' && r.items.some(it => it.unit === 'day' || (it.unit === 'hour' && r.quantity_source === 'working_days'))
}

function prevMonth(): string {
  const d = new Date(); d.setDate(1); d.setMonth(d.getMonth() - 1)
  return d.toISOString().slice(0, 7)
}

type DayState = 0 | 0.5 | 1
type DayMap = Record<string, DayState>

function pad(n: number) { return String(n).padStart(2, '0') }

/** Default selection: every work-week day that is not a holiday, as a full day. */
export function defaultDays(month: string, workWeek: string, holidays: string[]): DayMap {
  const [y, m] = month.split('-').map(Number)
  const week = new Set(workWeek.split(',').map(Number))
  const hol = new Set(holidays)
  const out: DayMap = {}
  const last = new Date(y, m, 0).getDate()
  for (let d = 1; d <= last; d++) {
    const iso = `${y}-${pad(m)}-${pad(d)}`
    const dow = new Date(y, m - 1, d).getDay() || 7 // ISO: Mon=1 … Sun=7
    out[iso] = week.has(dow) && !hol.has(iso) ? 1 : 0
  }
  return out
}

export function countDays(days: DayMap): number { return Object.values(days).reduce<number>((a, v) => a + v, 0) }

function DayPicker({ month, value, holidays, onChange }: { month: string; value: DayMap; holidays: Record<string, string>; onChange: (v: DayMap) => void }) {
  const [y, m] = month.split('-').map(Number)
  const first = new Date(y, m - 1, 1)
  const offset = (first.getDay() || 7) - 1 // blanks before the 1st (Monday-first grid)
  const last = new Date(y, m, 0).getDate()
  const cycle = (iso: string) => { const cur = value[iso] ?? 0; onChange({ ...value, [iso]: cur === 1 ? 0.5 : cur === 0.5 ? 0 : 1 }) }
  const cells: React.ReactNode[] = []
  for (let i = 0; i < offset; i++) cells.push(<button key={`b${i}`} type="button" className="blank" tabIndex={-1} />)
  for (let d = 1; d <= last; d++) {
    const iso = `${y}-${pad(m)}-${pad(d)}`
    const st = value[iso] ?? 0
    const hol = holidays[iso]
    cells.push(<button key={iso} type="button" className={`${st === 1 ? 'on' : st === 0.5 ? 'half' : ''}${hol ? ' hol' : ''}`} title={`${iso}${hol ? ` · ${hol}` : ''} · click: full → half → off`} onClick={() => cycle(iso)}>{d}</button>)
  }
  const full = Object.values(value).filter(v => v === 1).length, half = Object.values(value).filter(v => v === 0.5).length
  return (
    <div>
      <div className="daypick">{['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'].map(d => <div key={d} className="dow">{d}</div>)}{cells}</div>
      <div className="muted small mt" style={{ marginTop: 6 }}><strong>{countDays(value)}</strong> days · {full} full{half ? `, ${half} half` : ''} · click a day to cycle full → half → off{Object.keys(holidays).length ? ' · red dot = holiday' : ''}</div>
    </div>
  )
}

const SOURCE: Record<string, string> = { working_days: 'working days', time_entries: 'tracked time', profile: 'recurring profile', client: 'client defaults' }

export default function MonthEnd() {
  const toast = useToast()
  const { settings } = useApp()
  const [month, setMonth] = useState(() => new URLSearchParams(window.location.search).get('month') || prevMonth())
  const { data: plan, loading, reload } = useAsync(() => api.get<MonthEndPlan>(`${V1}/month-end?month=${month}`), [month])
  const [rows, setRows] = useState<MonthEndRow[]>([])
  const [issue, setIssue] = useState('')
  const [busy, setBusy] = useState(false)
  const [send, setSend] = useState<Invoice | null>(null)
  const [days, setDays] = useState<Record<number, DayMap>>({})
  useEffect(() => {
    if (!plan) return
    setRows(plan.rows); setIssue(plan.issue_date)
    const init: Record<number, DayMap> = {}
    for (const r of plan.rows) if (usesCalendar(r)) init[r.client_id] = defaultDays(plan.month, plan.work_week, plan.all_holidays.map(h => h.date))
    setDays(init)
  }, [plan])
  const holidayMap = useMemo(() => Object.fromEntries((plan?.all_holidays || []).map(h => [h.date, h.name])), [plan])
  /** Apply a day selection to the row's quantities: day lines get the day count, hour lines (when hours are derived from days) days × hours/day. */
  const applyDays = (i: number, map: DayMap) => {
    const r = rows[i]
    setDays(d => ({ ...d, [r.client_id]: map }))
    const n = countDays(map)
    const hpd = plan?.hours_per_day || 8
    setRow(i, { items: r.items.map(it => it.unit === 'day' ? { ...it, quantity: n } : it.unit === 'hour' && r.quantity_source === 'working_days' ? { ...it, quantity: n * hpd } : it) })
  }

  const setRow = (i: number, p: Partial<MonthEndRow>) => setRows(rs => rs.map((r, j) => j === i ? { ...r, ...p } : r))
  const setItem = (i: number, k: number, p: Partial<InvoiceItem>) => setRows(rs => rs.map((r, j) => j === i ? { ...r, items: r.items.map((it, idx) => idx === k ? { ...it, ...p } : it) } : r))
  const selected = rows.filter(r => r.include)
  const totals = useMemo(() => {
    const m: Record<string, number> = {}
    for (const r of selected) m[r.currency] = (m[r.currency] || 0) + computeTotals(r.items, 'none', 0).total
    return m
  }, [rows])

  const create = async () => {
    if (!selected.length) return
    setBusy(true)
    try {
      const r = await api.post<{ invoices: Invoice[]; errors: string[] }>(`${V1}/month-end`, {
        month, issue_date: issue,
        rows: selected.map(r => ({ client_id: r.client_id, recurring_id: r.recurring_id, template_id: r.template_id, currency: r.currency, billing_mode: r.billing_mode, items: r.items, custom_fields: r.custom_fields, time_entry_ids: r.time_entry_ids, due_date: r.due_date, notes: r.notes, terms: r.terms, worked_days: days[r.client_id] ? Object.entries(days[r.client_id]).filter(([, v]) => v > 0).map(([d, v]) => v === 0.5 ? `${d}:0.5` : d) : [] })),
      })
      if (r.errors?.length) toast(r.errors.join('; '), 'error')
      if (r.invoices.length) toast(`Created ${r.invoices.map(i => i.number).join(', ')}`, 'success')
      reload()
    } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  const markSent = async (inv: Invoice) => { try { await api.post(`${V1}/invoices/${inv.id}/status`, { status: 'sent' }); toast(`${inv.number} marked as sent`, 'success'); reload() } catch (e) { toast((e as Error).message, 'error') } }
  const upload = async (inv: Invoice, files: FileList | null) => {
    if (!files?.length) return
    const fd = new FormData(); for (const f of Array.from(files)) fd.append('file', f)
    try { await api.post(`${V1}/invoices/${inv.id}/attachments`, fd); toast(`Uploaded to ${inv.number}`, 'success'); reload() } catch (e) { toast((e as Error).message, 'error') }
  }

  const base = settings?.base_currency || ''
  const cal = plan?.calendar
  const attachMode = settings?.email_attachment_mode || 'generated'
  return (
    <>
      <PageHeader title="Month-end billing" sub="Everything for one month on one screen: propose, create the drafts, add the fiscal PDF, send." actions={<input type="month" value={month} onChange={e => setMonth(e.target.value)} max={today().slice(0, 7)} />} />
      {loading || !plan ? <Loading /> : <>
        <div className="grid cols-4 mb">
          <div className="card stat"><div className="label">Service period</div><div className="value" style={{ fontSize: 18 }}>{plan.month_label}</div><div className="hint">{fmtDate(plan.period_start)} – {fmtDate(plan.period_end)}</div></div>
          <div className="card stat"><div className="label">Working days</div><div className="value">{cal?.working_days}</div><div className="hint">{cal?.week_days} weekdays{cal && cal.holidays.length > 0 ? `, ${cal.holidays.length} holiday${cal.holidays.length === 1 ? '' : 's'}` : ''} · {cal ? cal.working_days * plan.hours_per_day : 0} h at {plan.hours_per_day} h/day · <Link to="/settings/invoicing">calendar</Link></div></div>
          <div className="card stat"><div className="label">Issue date</div><div className="value" style={{ fontSize: 18 }}><input type="date" value={issue} onChange={e => setIssue(e.target.value)} style={{ fontSize: 15, padding: '4px 8px' }} /></div><div className="hint">Due dates follow each client's terms</div></div>
          <div className="card stat"><div className="label">To create</div><div className="value">{selected.length}</div><div className="hint">{Object.entries(totals).map(([c, v]) => money(v, c)).join(' + ') || '—'}</div></div>
        </div>
        {cal && cal.holidays.length > 0 && <div className="callout mb">Holidays skipped: {cal.holidays.map(h => `${fmtDate(h.date)} ${h.name}`).join(' · ')}</div>}

        <h2 className="mb" style={{ fontSize: 16 }}>1. Prepare invoices</h2>
        {!rows.length ? <Card><Empty title="No clients">Add a client first.</Empty></Card> : <div className="grid mb">
          {rows.map((r, i) => {
            const t = computeTotals(r.items, 'none', 0)
            const open = r.existing.filter(e => e.status !== 'cancelled')
            return (
              <Card key={r.client_id} className={r.include ? '' : 'muted-card'} title={<label className="check" style={{ fontSize: 15 }}><input type="checkbox" checked={r.include} onChange={e => setRow(i, { include: e.target.checked })} /> {r.client_name} <span className="muted small" style={{ fontWeight: 400 }}>· {r.currency} · {r.billing_mode}{r.recurring_name ? ` · ${r.recurring_name}` : ''}</span></label>}
                actions={open.length > 0 ? <span className="row" style={{ gap: 6 }}>{open.map(e => <Link key={e.id} to={`/invoices/${e.id}`} className="badge-link"><Badge status={e.status}>{e.number} · {e.status}</Badge></Link>)}</span> : <span className="muted small">{r.hint}</span>}>
                {open.length > 0 && <div className="callout warn mb">Already invoiced for {plan.month_label}: {open.map(e => e.number).join(', ')}. Finish it in step 2 below, or tick the box to create another one.</div>}
                {r.include && <>
                  <div className="table-wrap"><table className="table items-table">
                    <thead><tr><th>Description</th><th style={{ width: 80 }}>Unit</th><th style={{ width: 110 }}>Qty</th><th style={{ width: 120 }}>Rate</th><th style={{ width: 90 }}>Tax %</th><th className="num">Amount</th></tr></thead>
                    <tbody>{r.items.map((it, k) => <tr key={k}>
                      <td><input value={it.description} onChange={e => setItem(i, k, { description: e.target.value })} /></td>
                      <td className="muted">{it.unit}</td>
                      <td><input type="number" step="0.25" value={it.quantity} onChange={e => setItem(i, k, { quantity: parseFloat(e.target.value) || 0 })} /></td>
                      <td><input type="number" step="0.01" value={it.unit_price} onChange={e => setItem(i, k, { unit_price: parseFloat(e.target.value) || 0 })} /></td>
                      <td><input type="number" step="0.01" value={it.tax_rate} onChange={e => setItem(i, k, { tax_rate: parseFloat(e.target.value) || 0 })} /></td>
                      <td className="num">{money(t.lines[k], r.currency)}</td>
                    </tr>)}</tbody>
                  </table></div>
                  {usesCalendar(r) && days[r.client_id] && <div className="grid split-2-1 mt" style={{ alignItems: 'start' }}>
                    <div>
                      <div className="row between wrap" style={{ marginBottom: 6 }}><div className="small bold">Days worked in {plan.month_label}</div><div className="row" style={{ gap: 6 }}><button type="button" className="btn ghost sm" onClick={() => applyDays(i, defaultDays(plan.month, plan.work_week, plan.all_holidays.map(h => h.date)))}>All working days</button><button type="button" className="btn ghost sm" onClick={() => applyDays(i, Object.fromEntries(Object.keys(days[r.client_id]).map(k => [k, 0 as DayState])))}>None</button></div></div>
                      <DayPicker month={plan.month} value={days[r.client_id]} holidays={holidayMap} onChange={m => applyDays(i, m)} />
                    </div>
                    <div className="muted small">Working days are pre-selected as full days. Click a day you took off to turn it into a half day, click again to remove it; click a weekend to add it. The quantity above follows the selection{r.items.some(it => it.unit === 'hour') ? ` (hours = days × ${plan.hours_per_day})` : ''}.</div>
                  </div>}
                  <div className="row between wrap mt" style={{ alignItems: 'flex-end', gap: 12 }}>
                    <div className="row wrap" style={{ gap: 12 }}>
                      {plan.custom_fields.map(f => <Field key={f.key} label={f.label} help={r.custom_fields_last[f.key] ? `last: ${r.custom_fields_last[f.key]}` : undefined}><input value={r.custom_fields[f.key] || ''} onChange={e => setRow(i, { custom_fields: { ...r.custom_fields, [f.key]: e.target.value } })} style={{ width: 170 }} /></Field>)}
                      <Field label="Due date"><input type="date" value={r.due_date} onChange={e => setRow(i, { due_date: e.target.value })} /></Field>
                    </div>
                    <div style={{ textAlign: 'right' }}><div className="muted small">Quantity from {SOURCE[r.quantity_source] || r.quantity_source}{r.unbilled_minutes > 0 && r.quantity_source !== 'time_entries' ? ` · ${(r.unbilled_minutes / 60).toFixed(2)} h tracked but not used` : ''}</div><div className="bold" style={{ fontSize: 18 }}>{money(t.total, r.currency)}</div></div>
                  </div>
                </>}
              </Card>
            )
          })}
          <div className="row between wrap">
            <div className="muted small">Drafts get the next invoice number, the service period {fmtDate(plan.period_start)} – {fmtDate(plan.period_end)}, the exchange rate of the issue date, and the client's or profile's template. Recurring profiles are advanced so the scheduler does not create the same month again.</div>
            <button className="btn primary" disabled={busy || !selected.length} onClick={create}>{busy ? 'Creating…' : `Create ${selected.length} draft invoice${selected.length === 1 ? '' : 's'}`}</button>
          </div>
        </div>}

        <h2 className="mb" style={{ fontSize: 16 }}>2. Finish &amp; send</h2>
        <Card flush>
          {!plan.invoices.length ? <div className="empty muted">No invoices for {plan.month_label} yet. Create them above.</div> : <div className="table-wrap"><table className="table">
            <thead><tr><th>Invoice</th><th>Client</th><th className="num">Total</th><th>Status</th><th>Files</th><th></th></tr></thead>
            <tbody>{plan.invoices.map(inv => <tr key={inv.id}>
              <td><Link to={`/invoices/${inv.id}`} className="bold">{inv.number}</Link><div className="muted small">issued {fmtDate(inv.issue_date)} · due {fmtDate(inv.due_date)}</div></td>
              <td>{inv.client_name}</td>
              <td className="num">{money(inv.total, inv.currency)}{inv.currency !== base && <div className="muted small">{money(inv.total * inv.exchange_rate, base)}</div>}</td>
              <td><Badge status={inv.status} />{inv.sent_at && <div className="muted small">sent {fmtDate(inv.sent_at)}</div>}</td>
              <td>{inv.attachments?.length ? <span className="small">{inv.attachments.map(a => a.filename).join(', ')}</span> : <span className="muted small">{attachMode === 'uploaded' ? 'no fiscal PDF yet (email would send the generated one)' : 'none'}</span>}</td>
              <td className="actions">
                <a className="btn sm" href={`${V1}/invoices/${inv.id}/pdf`} target="_blank" rel="noreferrer">PDF</a>
                <label className="btn sm">Upload PDF<input type="file" multiple style={{ display: 'none' }} onChange={e => upload(inv, e.target.files)} /></label>
                {inv.status !== 'paid' && inv.status !== 'cancelled' && <button className="btn sm primary" onClick={() => setSend(inv)} disabled={!plan.smtp_configured} title={plan.smtp_configured ? '' : 'Configure SMTP under Settings → Email'}>Email</button>}
                {inv.status === 'draft' && <button className="btn sm" onClick={() => markSent(inv)}>Mark sent</button>}
              </td>
            </tr>)}</tbody>
          </table></div>}
        </Card>
      </>}
      {send && <Modal title={`Email ${send.number}`} onClose={() => setSend(null)}><SendForm inv={send} onDone={() => { setSend(null); reload() }} /></Modal>}
    </>
  )
}
