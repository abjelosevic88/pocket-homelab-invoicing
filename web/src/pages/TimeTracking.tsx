import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, ApiError, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDateTime, hours, hoursDecimal, money, today } from '../lib/format'
import type { Client, TimeEntry } from '../lib/types'
import { Card, Confirm, Empty, Field, Loading, Modal, PageHeader, useAsync, useToast } from '../components/ui'
import { elapsed, timerChanged, timerLabel, useTimer } from '../components/shell'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'

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
        <Field label="Client"><Select value={String(e.client_id)} onChange={v => setE({ ...e, client_id: Number(v) })} placeholder="Choose a client…" options={clients.map(c => ({ value: String(c.id), label: c.name }))} /></Field>
        <Field label="Project"><input value={e.project} onChange={ev => setE({ ...e, project: ev.target.value })} placeholder="optional" /></Field>
        <Field label="Description" className="full"><input value={e.description} onChange={ev => setE({ ...e, description: ev.target.value })} autoFocus /></Field>
        <Field label="Start"><input type="datetime-local" value={e.started_at} onChange={ev => setE({ ...e, started_at: ev.target.value })} /></Field>
        <Field label="Duration (minutes)"><input type="number" min={1} value={e.duration_minutes} onChange={ev => setE({ ...e, duration_minutes: Number(ev.target.value) || 0 })} /></Field>
        <Field label="Rate override" help="Blank = client default"><input type="number" step="0.01" value={e.rate ?? ''} onChange={ev => setE({ ...e, rate: ev.target.value === '' ? null : parseFloat(ev.target.value) })} /></Field>
        <Field label=" "><label className="check" style={{ marginTop: 8 }}><input type="checkbox" checked={e.billable} onChange={ev => setE({ ...e, billable: ev.target.checked })} /> Billable</label></Field>
      </div>
      <div className="form-actions"><button type="submit" className="btn primary" disabled={busy}>Save</button></div>
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
        <Field label="Group lines by"><Select value={opts.group_by} onChange={v => setOpts({ ...opts, group_by: v })} options={[{ value: 'entry', label: 'One line per entry' }, { value: 'day', label: 'Per day' }, { value: 'project', label: 'Per project' }, { value: 'total', label: 'Single line' }]} /></Field>
        <Field label="Bill as"><Select value={opts.billing_mode} onChange={v => setOpts({ ...opts, billing_mode: v })} options={[{ value: 'hourly', label: 'Hours' }, { value: 'daily', label: 'Days', hint: `${settings?.hours_per_day || 8}h = 1 day` }]} /></Field>
        <Field label={`Rate per ${opts.billing_mode === 'daily' ? 'day' : 'hour'}`}><input type="number" step="0.01" value={opts.rate} onChange={e => setOpts({ ...opts, rate: parseFloat(e.target.value) || 0 })} /></Field>
        <Field label=" " className="full"><label className="check"><input type="checkbox" checked={opts.include_expenses} onChange={e => setOpts({ ...opts, include_expenses: e.target.checked })} /> Include unbilled billable expenses for this client</label></Field>
      </div>
      <div className="table-wrap" style={{ maxHeight: 300, overflow: 'auto' }}><table className="table">
        <thead><tr><th><input type="checkbox" checked={selected.size === entries.length} onChange={e => setSelected(e.target.checked ? new Set(entries.map(x => x.id)) : new Set())} /></th><th>Date</th><th>Description</th><th className="num">Time</th></tr></thead>
        <tbody>{entries.map(e => <tr key={e.id}><td><input type="checkbox" checked={selected.has(e.id)} onChange={ev => { const n = new Set(selected); ev.target.checked ? n.add(e.id) : n.delete(e.id); setSelected(n) }} /></td><td className="muted">{fmtDateTime(e.started_at)}</td><td>{e.project && <span className="muted">{e.project}: </span>}{e.description}</td><td className="num">{hours(e.duration_minutes)}</td></tr>)}</tbody>
      </table></div>
      <div className="row between mt"><span className="bold">{selected.size} entries · {hours(mins)} ({hoursDecimal(mins)} h)</span><button type="button" className="btn primary" disabled={busy || !selected.size} onClick={submit}>Create draft invoice</button></div>
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
  const { running, shared, time_error: timeError } = useTimer()
  const [now, setNow] = useState(Date.now())
  const [editing, setEditing] = useState<TimeEntry | 'new' | null>(null)
  const [invoicing, setInvoicing] = useState(false)
  // "Stop it and start this": the running timer (or what Pocket Time said about it) when starting another
  const [switchAsk, setSwitchAsk] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { const i = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(i) }, [])
  useEffect(() => { if (clients?.length && !timer.client_id) setTimer(t => ({ ...t, client_id: clientId || clients[0].id })) }, [clients, clientId, timer.client_id])
  // time from Pocket Time arrives through the worklog a moment after it stops
  const reloadSoon = () => { reload(); setTimeout(reload, 2500); setTimeout(reload, 6000) }
  const start = async (sw = false) => {
    if (!sw && running) { setSwitchAsk(`${timerLabel(running)} is running${running.ours === false && running.started_in ? ` (started in ${running.started_in})` : ''}.`); return }
    setBusy(true)
    try { await api.post(`${V1}/time/start`, { ...timer, switch: sw }); setSwitchAsk(null); timerChanged(); if (running) reloadSoon(); else reload() }
    catch (e) { if (e instanceof ApiError && e.status === 409) setSwitchAsk(e.message); else toast((e as Error).message, 'error'); timerChanged() }
    finally { setBusy(false) }
  }
  const stop = async () => {
    if (!running) return
    setBusy(true)
    try {
      await api.post(`${V1}/time/stop`, running.shared ? { time_id: running.time_id } : {})
      toast(running.shared ? (running.ours ? 'Timer stopped. Pocket Time adds the time here in a moment.' : 'Timer stopped in Pocket Time') : 'Timer stopped', 'success')
      if (running.shared) reloadSoon(); else reload()
    } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false); timerChanged() }
  }
  const del = async (e: TimeEntry) => { if (!confirm('Delete entry?')) return; await api.del(`${V1}/time/${e.id}`); reload() }
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n) }
  const unbilledEntries = (entries || []).filter(e => e.billable && !e.invoice_id && e.ended_at)
  const unbilledByClient = unbilledEntries.reduce((m, e) => { m[e.client_id] = (m[e.client_id] || 0) + e.duration_minutes; return m }, {} as Record<number, number>)
  const totalMinutes = (entries || []).reduce((a, e) => a + e.duration_minutes, 0)
  const invoiceClient = clientId || Number(Object.keys(unbilledByClient)[0]) || 0
  return (
    <>
      <PageHeader title="Time tracking" sub={`Rounded to ${settings?.time_rounding_minutes || 15} min blocks when the timer stops (Settings → Invoicing)${shared ? '. The timer is your timer in Pocket Time: start it here or there, stop it in either place.' : '.'}`} actions={<>
        <a className="btn" href={`${V1}/reports/export.csv?type=time`}>Export CSV</a>
        <button type="button" className="btn" onClick={() => setEditing('new')}><Icon name="plus" size={15} />Manual entry</button>
        <button type="button" className="btn primary" disabled={!unbilledEntries.length} onClick={() => setInvoicing(true)}>Invoice unbilled time</button>
      </>} />
      <Card className="mb">
        {running && <div className="row between" style={running.ours === false ? { marginBottom: 12 } : undefined}>
          <div className="row"><span className="timer-pill"><span className="dot" /> {elapsed(running.started_at, now)}</span>{running.ours === false
            ? <span><strong>Timer running in Pocket Time: {running.description}</strong><div className="muted small">{running.started_in ? `Started in ${running.started_in}. ` : ''}Only one timer runs at a time.</div></span>
            : <span><strong>{running.client_name}</strong>{running.project && <span className="muted"> · {running.project}</span>}<div className="muted small">{running.description}{running.shared && <>{running.description ? ' · ' : ''}also running in Pocket Time</>}</div></span>}</div>
          <button type="button" className="btn danger" onClick={stop} disabled={busy}><Icon name="stop" size={14} />Stop</button>
        </div>}
        {(!running || running.ours === false) && (
          <div className="row" style={{ gap: 10 }}>
            <Select className="timer-client" ariaLabel="Client" placeholder="Choose a client…" value={String(timer.client_id)} onChange={v => setTimer({ ...timer, client_id: Number(v) })} options={(clients || []).map(c => ({ value: String(c.id), label: c.name }))} />
            <input value={timer.project} onChange={e => setTimer({ ...timer, project: e.target.value })} placeholder="Project (optional)" style={{ width: 180 }} />
            <input value={timer.description} onChange={e => setTimer({ ...timer, description: e.target.value })} placeholder="What are you working on?" style={{ flex: 1, minWidth: 200 }} onKeyDown={e => e.key === 'Enter' && start()} />
            <button type="button" className="btn primary" onClick={() => start()} disabled={!timer.client_id || busy}><Icon name="play" size={14} />Start timer</button>
          </div>
        )}
        {timeError && <div className="muted small" style={{ marginTop: 8 }}>Pocket Time could not be asked just now: {timeError}</div>}
      </Card>
      <div className="filters">
        <Select value={String(clientId)} onChange={v => setParam('client_id', v === '0' ? '' : v)} ariaLabel="Client" placeholder="All clients" options={[{ value: '0', label: 'All clients' }, ...(clients || []).map(c => ({ value: String(c.id), label: c.name }))]} />
        <label className="check"><input type="checkbox" checked={unbilled} onChange={e => setParam('unbilled', e.target.checked ? '1' : '')} /> Unbilled only</label>
        <span className="muted small">{entries?.length || 0} entries · {hours(totalMinutes)} · unbilled {hours(unbilledEntries.reduce((a, e) => a + e.duration_minutes, 0))}</span>
      </div>
      <Card flush>
        {loading ? <Loading /> : !entries?.length ? <Empty title="No time entries">Start the timer above or add a manual entry.</Empty> : <div className="table-wrap"><table className="table">
          <thead><tr><th>When</th><th>Client</th><th>Project / description</th><th className="num">Duration</th><th>Status</th><th></th></tr></thead>
          <tbody>{entries.map(e => <tr key={e.id}>
            <td className="muted">{fmtDateTime(e.started_at)}</td>
            <td>{e.client_name}</td>
            <td>{e.project && <span className="muted">{e.project}: </span>}{e.description}{e.rate != null && <span className="muted small"> · {money(e.rate, clients?.find(c => c.id === e.client_id)?.currency || '')}/h</span>}{e.source === 'time' && <span className="badge" style={{ marginLeft: 6 }} title="Tracked in Pocket Time">Pocket Time</span>}</td>
            <td className="num">{e.ended_at ? hours(e.duration_minutes) : <span className="timer-pill">running</span>}</td>
            <td>{!e.billable ? <span className="badge">non-billable</span> : e.invoice_id ? <span className="badge paid" onClick={() => navigate(`/invoices/${e.invoice_id}`)} style={{ cursor: 'pointer' }}>invoiced</span> : <span className="badge sent">unbilled</span>}</td>
            <td className="actions">{!e.invoice_id && <><button type="button" className="btn ghost sm" onClick={() => setEditing(e)}>Edit</button><button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={() => del(e)}><Icon name="trash" size={14} /></button></>}</td>
          </tr>)}</tbody>
        </table></div>}
      </Card>
      {switchAsk && <Confirm title="Switch timers?" danger={false} confirmLabel="Stop it and start this" message={<>{switchAsk} Only one timer runs at a time. Stop it and start the timer for <b>{clients?.find(c => c.id === timer.client_id)?.name}</b>?</>} onCancel={() => setSwitchAsk(null)} onConfirm={() => start(true)} />}
      {editing && clients && <Modal title={editing === 'new' ? 'Manual time entry' : 'Edit entry'} onClose={() => setEditing(null)}><EntryForm initial={editing === 'new' ? undefined : editing} clients={clients} onDone={() => { setEditing(null); reload() }} /></Modal>}
      {invoicing && clients && <Modal title="Create invoice from unbilled time" size="lg" onClose={() => setInvoicing(false)}>
        {!clientId && Object.keys(unbilledByClient).length > 1 && <div className="callout mb">Multiple clients have unbilled time. Choose one: {Object.keys(unbilledByClient).map(id => <button type="button" key={id} className="link-btn" style={{ marginLeft: 8 }} onClick={() => setParam('client_id', id)}>{clients.find(c => c.id === Number(id))?.name} ({hours(unbilledByClient[Number(id)])})</button>)}</div>}
        {invoiceClient > 0 && <InvoiceFromTime clientId={invoiceClient} clients={clients} entries={unbilledEntries.filter(e => e.client_id === invoiceClient)} onDone={id => { setInvoicing(false); navigate(`/invoices/${id}`) }} />}
      </Modal>}
    </>
  )
}
