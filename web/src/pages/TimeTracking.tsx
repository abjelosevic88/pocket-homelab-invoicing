import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDateTime, hours, hoursDecimal, money, today } from '../lib/format'
import type { Client, TimeEntry } from '../lib/types'
import { Card, Empty, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'

function toLocalInput(iso: string) { const d = new Date(iso); const p = (n: number) => String(n).padStart(2, '0'); return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}` }

function EntryForm({ initial, clients, onDone }: { initial?: TimeEntry; clients: Client[]; onDone: () => void }) {
  const toast = useToast()
  const [e, setE] = useState({ client_id: initial?.client_id || clients[0]?.id || 0, project: initial?.project || '', description: initial?.description || '', started_at: initial ? toLocalInput(initial.started_at) : `${today()}T09:00`, duration_minutes: initial?.duration_minutes || 60, billable: initial?.billable ?? true, rate: initial?.rate ?? null as number | null })
  const [busy, setBusy] = useState(false)
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault(); setBusy(true)
    try {
      const body = { ...e, started_at: new Date(e.started_at).toISOString(), ended_at: new Date(new Date(e.started_at).getTime() + e.duration_minutes * 60000).toISOString() }
      if (initial) await api.put(`${V1}/time/${initial.id}`, body); else await api.post(`${V1}/time`, body)
      toast('Saved', 'success'); onDone()
    } catch (err) { toast((err as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit}>
      <div className="form-grid">
        <Field label="Client"><select value={e.client_id} onChange={ev => setE({ ...e, client_id: Number(ev.target.value) })}>{clients.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field>
        <Field label="Project"><input value={e.project} onChange={ev => setE({ ...e, project: ev.target.value })} placeholder="optional" /></Field>
        <Field label="Description" className="full"><input value={e.description} onChange={ev => setE({ ...e, description: ev.target.value })} autoFocus /></Field>
        <Field label="Start"><input type="datetime-local" value={e.started_at} onChange={ev => setE({ ...e, started_at: ev.target.value })} /></Field>
        <Field label="Duration (minutes)"><input type="number" min={1} value={e.duration_minutes} onChange={ev => setE({ ...e, duration_minutes: Number(ev.target.value) || 0 })} /></Field>
        <Field label="Rate override" help="Blank = client default"><input type="number" step="0.01" value={e.rate ?? ''} onChange={ev => setE({ ...e, rate: ev.target.value === '' ? null : parseFloat(ev.target.value) })} /></Field>
        <Field label=" "><label className="check" style={{ marginTop: 8 }}><input type="checkbox" checked={e.billable} onChange={ev => setE({ ...e, billable: ev.target.checked })} /> Billable</label></Field>
      </div>
      <div className="form-actions"><button className="btn primary" disabled={busy}>Save</button></div>
    </form>
  )
}

function InvoiceFromTime({ clientId, clients, entries, onDone }: { clientId: number; clients: Client[]; entries: TimeEntry[]; onDone: (id: number) => void }) {
  const toast = useToast()
  const { settings } = useApp()
  const client = clients.find(c => c.id === clientId)
  const [opts, setOpts] = useState({ group_by: 'entry', billing_mode: client?.billing_mode === 'daily' ? 'daily' : 'hourly', rate: client?.default_rate || settings?.default_hourly_rate || 0, include_expenses: true })
  const [selected, setSelected] = useState<Set<number>>(new Set(entries.map(e => e.id)))
  const [busy, setBusy] = useState(false)
  const mins = entries.filter(e => selected.has(e.id)).reduce((a, e) => a + e.duration_minutes, 0)
  const submit = async () => {
    setBusy(true)
    try { const inv = await api.post<{ id: number; number: string }>(`${V1}/invoices/from-time`, { client_id: clientId, entry_ids: [...selected], ...opts }); toast(`Draft ${inv.number} created`, 'success'); onDone(inv.id) } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <div>
      <div className="form-grid cols-3 mb">
        <Field label="Group lines by"><select value={opts.group_by} onChange={e => setOpts({ ...opts, group_by: e.target.value })}><option value="entry">One line per entry</option><option value="day">Per day</option><option value="project">Per project</option><option value="total">Single line</option></select></Field>
        <Field label="Bill as"><select value={opts.billing_mode} onChange={e => setOpts({ ...opts, billing_mode: e.target.value })}><option value="hourly">Hours</option><option value="daily">Days ({settings?.hours_per_day || 8}h = 1 day)</option></select></Field>
        <Field label={`Rate per ${opts.billing_mode === 'daily' ? 'day' : 'hour'}`}><input type="number" step="0.01" value={opts.rate} onChange={e => setOpts({ ...opts, rate: parseFloat(e.target.value) || 0 })} /></Field>
        <Field label=" " className="full"><label className="check"><input type="checkbox" checked={opts.include_expenses} onChange={e => setOpts({ ...opts, include_expenses: e.target.checked })} /> Include unbilled billable expenses for this client</label></Field>
      </div>
      <div className="table-wrap" style={{ maxHeight: 300, overflow: 'auto' }}><table className="table">
        <thead><tr><th><input type="checkbox" checked={selected.size === entries.length} onChange={e => setSelected(e.target.checked ? new Set(entries.map(x => x.id)) : new Set())} /></th><th>Date</th><th>Description</th><th className="num">Time</th></tr></thead>
        <tbody>{entries.map(e => <tr key={e.id}><td><input type="checkbox" checked={selected.has(e.id)} onChange={ev => { const n = new Set(selected); ev.target.checked ? n.add(e.id) : n.delete(e.id); setSelected(n) }} /></td><td className="muted">{fmtDateTime(e.started_at)}</td><td>{e.project && <span className="muted">{e.project}: </span>}{e.description}</td><td className="num">{hours(e.duration_minutes)}</td></tr>)}</tbody>
      </table></div>
      <div className="row between mt"><span className="bold">{selected.size} entries · {hours(mins)} ({hoursDecimal(mins)} h)</span><button className="btn primary" disabled={busy || !selected.size} onClick={submit}>Create draft invoice</button></div>
    </div>
  )
}

export default function TimeTracking() {
  const [sp, setSp] = useSearchParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { settings } = useApp()
  const clientId = Number(sp.get('client_id')) || 0
  const unbilled = sp.get('unbilled') === '1'
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients`))
  const { data: entries, loading, reload } = useAsync(() => api.get<TimeEntry[]>(`${V1}/time?client_id=${clientId || ''}&unbilled=${unbilled ? 1 : 0}&limit=300`), [clientId, unbilled])
  const [timer, setTimer] = useState({ client_id: 0, project: '', description: '' })
  const [running, setRunning] = useState<TimeEntry | null>(null)
  const [tick, setTick] = useState(0)
  const [editing, setEditing] = useState<TimeEntry | 'new' | null>(null)
  const [invoicing, setInvoicing] = useState(false)
  const loadRunning = () => api.get<{ running: TimeEntry | null }>(`${V1}/time/running`).then(r => setRunning(r.running))
  useEffect(() => { loadRunning() }, [])
  useEffect(() => { const i = setInterval(() => setTick(t => t + 1), 1000); return () => clearInterval(i) }, [])
  useEffect(() => { if (clients?.length && !timer.client_id) setTimer(t => ({ ...t, client_id: clientId || clients[0].id })) }, [clients, clientId, timer.client_id])
  const start = async () => { try { await api.post(`${V1}/time/start`, timer); await loadRunning(); window.dispatchEvent(new Event('pi:timer')); reload() } catch (e) { toast((e as Error).message, 'error') } }
  const stop = async () => { await api.post(`${V1}/time/stop`); await loadRunning(); window.dispatchEvent(new Event('pi:timer')); reload(); toast('Timer stopped', 'success') }
  const del = async (e: TimeEntry) => { if (!confirm('Delete entry?')) return; await api.del(`${V1}/time/${e.id}`); reload() }
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n) }
  const elapsed = running ? Math.max(0, Math.floor((Date.now() - new Date(running.started_at).getTime()) / 1000)) : 0
  void tick
  const unbilledEntries = (entries || []).filter(e => e.billable && !e.invoice_id && e.ended_at)
  const unbilledByClient = unbilledEntries.reduce((m, e) => { m[e.client_id] = (m[e.client_id] || 0) + e.duration_minutes; return m }, {} as Record<number, number>)
  const totalMinutes = (entries || []).reduce((a, e) => a + e.duration_minutes, 0)
  const invoiceClient = clientId || Number(Object.keys(unbilledByClient)[0]) || 0
  return (
    <>
      <PageHeader title="Time tracking" sub={`Rounded to ${settings?.time_rounding_minutes || 15} min blocks when the timer stops (Settings → Invoicing).`} actions={<>
        <a className="btn" href={`${V1}/reports/export.csv?type=time`}>Export CSV</a>
        <button className="btn" onClick={() => setEditing('new')}>+ Manual entry</button>
        <button className="btn primary" disabled={!unbilledEntries.length} onClick={() => setInvoicing(true)}>Invoice unbilled time</button>
      </>} />
      <Card className="mb">
        {running ? (
          <div className="row between">
            <div className="row"><span className="timer-pill"><span className="dot" /> {String(Math.floor(elapsed / 3600)).padStart(2, '0')}:{String(Math.floor((elapsed % 3600) / 60)).padStart(2, '0')}:{String(elapsed % 60).padStart(2, '0')}</span><span><strong>{running.client_name}</strong>{running.project && <span className="muted"> · {running.project}</span>}<div className="muted small">{running.description}</div></span></div>
            <button className="btn danger" onClick={stop}>■ Stop</button>
          </div>
        ) : (
          <div className="row" style={{ gap: 10 }}>
            <select value={timer.client_id} onChange={e => setTimer({ ...timer, client_id: Number(e.target.value) })} style={{ width: 200 }}>{clients?.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
            <input value={timer.project} onChange={e => setTimer({ ...timer, project: e.target.value })} placeholder="Project (optional)" style={{ width: 180 }} />
            <input value={timer.description} onChange={e => setTimer({ ...timer, description: e.target.value })} placeholder="What are you working on?" style={{ flex: 1, minWidth: 200 }} onKeyDown={e => e.key === 'Enter' && start()} />
            <button className="btn primary" onClick={start} disabled={!timer.client_id}>▶ Start timer</button>
          </div>
        )}
      </Card>
      <div className="filters">
        <select value={clientId} onChange={e => setParam('client_id', e.target.value === '0' ? '' : e.target.value)}><option value={0}>All clients</option>{clients?.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
        <label className="check"><input type="checkbox" checked={unbilled} onChange={e => setParam('unbilled', e.target.checked ? '1' : '')} /> Unbilled only</label>
        <span className="muted small">{entries?.length || 0} entries · {hours(totalMinutes)} · unbilled {hours(unbilledEntries.reduce((a, e) => a + e.duration_minutes, 0))}</span>
      </div>
      <Card flush>
        {loading ? <Loading /> : !entries?.length ? <Empty title="No time entries">Start the timer above or add a manual entry.</Empty> : <div className="table-wrap"><table className="table">
          <thead><tr><th>When</th><th>Client</th><th>Project / description</th><th className="num">Duration</th><th>Status</th><th></th></tr></thead>
          <tbody>{entries.map(e => <tr key={e.id}>
            <td className="muted">{fmtDateTime(e.started_at)}</td>
            <td>{e.client_name}</td>
            <td>{e.project && <span className="muted">{e.project}: </span>}{e.description}{e.rate != null && <span className="muted small"> · {money(e.rate, clients?.find(c => c.id === e.client_id)?.currency || '')}/h</span>}</td>
            <td className="num">{e.ended_at ? hours(e.duration_minutes) : <span className="timer-pill">running</span>}</td>
            <td>{!e.billable ? <span className="badge">non-billable</span> : e.invoice_id ? <span className="badge paid" onClick={() => navigate(`/invoices/${e.invoice_id}`)} style={{ cursor: 'pointer' }}>invoiced</span> : <span className="badge sent">unbilled</span>}</td>
            <td className="actions">{!e.invoice_id && <><button className="btn ghost sm" onClick={() => setEditing(e)}>Edit</button><button className="btn ghost sm" onClick={() => del(e)}>✕</button></>}</td>
          </tr>)}</tbody>
        </table></div>}
      </Card>
      {editing && clients && <Modal title={editing === 'new' ? 'Manual time entry' : 'Edit entry'} onClose={() => setEditing(null)}><EntryForm initial={editing === 'new' ? undefined : editing} clients={clients} onDone={() => { setEditing(null); reload() }} /></Modal>}
      {invoicing && clients && <Modal title="Create invoice from unbilled time" size="lg" onClose={() => setInvoicing(false)}>
        {!clientId && Object.keys(unbilledByClient).length > 1 && <div className="callout mb">Multiple clients have unbilled time. Choose one: {Object.keys(unbilledByClient).map(id => <button key={id} className="link-btn" style={{ marginLeft: 8 }} onClick={() => setParam('client_id', id)}>{clients.find(c => c.id === Number(id))?.name} ({hours(unbilledByClient[Number(id)])})</button>)}</div>}
        {invoiceClient > 0 && <InvoiceFromTime clientId={invoiceClient} clients={clients} entries={unbilledEntries.filter(e => e.client_id === invoiceClient)} onDone={id => { setInvoicing(false); navigate(`/invoices/${id}`) }} />}
      </Modal>}
    </>
  )
}
