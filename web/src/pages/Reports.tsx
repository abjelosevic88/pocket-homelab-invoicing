import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, hours, hoursDecimal, money, monthLabel, today } from '../lib/format'
import type { Invoice, MonthlyRevenue } from '../lib/types'
import { Badge, Card, Empty, Loading, PageHeader, Tabs, useAsync } from '../components/ui'
import { RevenueChart } from './Dashboard'

type Tab = 'revenue' | 'clients' | 'aging' | 'tax' | 'income' | 'currencies' | 'time'
const TABS: { id: Tab; label: string }[] = [{ id: 'revenue', label: 'Revenue' }, { id: 'clients', label: 'By client' }, { id: 'aging', label: 'Outstanding & aging' }, { id: 'tax', label: 'Tax on invoices' }, { id: 'income', label: 'Income tax' }, { id: 'currencies', label: 'Currencies' }, { id: 'time', label: 'Time' }]

function yearStart() { return `${new Date().getFullYear()}-01-01` }

export default function Reports() {
  const { settings } = useApp()
  const navigate = useNavigate()
  const [tab, setTab] = useState<Tab>('revenue')
  const [from, setFrom] = useState(yearStart())
  const [to, setTo] = useState(today())
  const { data: years } = useAsync(() => api.get<string[]>(`${V1}/reports/years`))
  const [year, setYear] = useState(String(new Date().getFullYear()))
  const yearOptions = Array.from(new Set([...(years || []), String(new Date().getFullYear())])).sort().reverse()
  const base = settings?.base_currency || ''
  const q = `from=${from}&to=${to}`
  const presets: [string, () => [string, string]][] = [['This year', () => [yearStart(), today()]], ['Last year', () => { const y = new Date().getFullYear() - 1; return [`${y}-01-01`, `${y}-12-31`] }], ['Last 12 months', () => { const d = new Date(); d.setFullYear(d.getFullYear() - 1); return [d.toISOString().slice(0, 10), today()] }], ['This month', () => [today().slice(0, 7) + '-01', today()]]]
  return (
    <>
      <PageHeader title="Reports" sub={`Figures converted to ${base} at each invoice's exchange rate.`} actions={<a className="btn" href={`${V1}/reports/export.csv?type=items&${q}`}>Export line items CSV</a>} />
      <div className="filters">
        <select value={year} onChange={e => { setYear(e.target.value); setFrom(`${e.target.value}-01-01`); setTo(`${e.target.value}-12-31`) }}>{yearOptions.map(y => <option key={y} value={y}>{y}</option>)}</select>
        <input type="date" value={from} onChange={e => setFrom(e.target.value)} /><span className="muted">to</span><input type="date" value={to} onChange={e => setTo(e.target.value)} />
        {presets.map(([l, f]) => <button key={l} className="btn sm" onClick={() => { const [a, b] = f(); setFrom(a); setTo(b) }}>{l}</button>)}
      </div>
      <Tabs tabs={TABS} value={tab} onChange={setTab} />
      {tab === 'revenue' && <Revenue q={q} base={base} />}
      {tab === 'clients' && <ByClient q={q} base={base} onOpen={id => navigate(`/clients/${id}`)} />}
      {tab === 'aging' && <Aging base={base} onOpen={id => navigate(`/invoices/${id}`)} />}
      {tab === 'tax' && <Tax q={q} base={base} />}
      {tab === 'income' && <IncomeTax year={year} base={base} />}
      {tab === 'currencies' && <Currencies q={q} base={base} />}
      {tab === 'time' && <Time q={q} />}
    </>
  )
}

function Revenue({ q, base }: { q: string; base: string }) {
  const { data, loading } = useAsync(() => api.get<{ months: MonthlyRevenue[] }>(`${V1}/reports/revenue?${q}`), [q])
  if (loading || !data) return <Loading />
  const sum = (k: keyof MonthlyRevenue) => data.months.reduce((a, m) => a + (m[k] as number), 0)
  return (
    <div className="grid">
      <div className="grid cols-4">
        <div className="card stat"><div className="label">Invoiced</div><div className="value">{money(sum('invoiced'), base)}</div><div className="hint">{sum('count')} invoices</div></div>
        <div className="card stat success"><div className="label">Received</div><div className="value">{money(sum('paid'), base)}</div></div>
        <div className="card stat danger"><div className="label">Expenses</div><div className="value">{money(sum('expenses'), base)}</div></div>
        <div className="card stat"><div className="label">Net (received − expenses)</div><div className="value">{money(sum('paid') - sum('expenses'), base)}</div></div>
      </div>
      <Card title="Monthly"><RevenueChart months={data.months} code={base} /></Card>
      <Card flush><table className="table"><thead><tr><th>Month</th><th className="num">Invoices</th><th className="num">Invoiced</th><th className="num">Received</th><th className="num">Expenses</th><th className="num">Net</th></tr></thead>
        <tbody>{data.months.map(m => <tr key={m.month}><td>{monthLabel(m.month)}</td><td className="num">{m.count}</td><td className="num">{money(m.invoiced, base)}</td><td className="num">{money(m.paid, base)}</td><td className="num">{money(m.expenses, base)}</td><td className="num bold">{money(m.paid - m.expenses, base)}</td></tr>)}</tbody>
        <tfoot><tr><td>Total</td><td className="num">{sum('count')}</td><td className="num">{money(sum('invoiced'), base)}</td><td className="num">{money(sum('paid'), base)}</td><td className="num">{money(sum('expenses'), base)}</td><td className="num">{money(sum('paid') - sum('expenses'), base)}</td></tr></tfoot></table></Card>
    </div>
  )
}

function ByClient({ q, base, onOpen }: { q: string; base: string; onOpen: (id: number) => void }) {
  const { data, loading } = useAsync(() => api.get<{ clients: { client_id: number; client_name: string; currency: string; invoiced: number; paid: number; outstanding: number; count: number }[] }>(`${V1}/reports/clients?${q}`), [q])
  if (loading || !data) return <Loading />
  const total = data.clients.reduce((a, c) => a + c.invoiced, 0)
  return <Card flush>{!data.clients.length ? <Empty title="No data in range" /> : <table className="table"><thead><tr><th>Client</th><th>Currency</th><th className="num">Invoices</th><th className="num">Invoiced ({base})</th><th className="num">Received</th><th className="num">Outstanding</th><th className="num">Share</th></tr></thead>
    <tbody>{data.clients.map(c => <tr key={c.client_id} className="clickable" onClick={() => onOpen(c.client_id)}><td className="bold">{c.client_name}</td><td className="muted">{c.currency}</td><td className="num">{c.count}</td><td className="num">{money(c.invoiced, base)}</td><td className="num">{money(c.paid, base)}</td><td className="num">{money(c.outstanding, base)}</td><td className="num">{total ? ((c.invoiced / total) * 100).toFixed(1) : 0}%</td></tr>)}</tbody></table>}</Card>
}

function Aging({ base, onOpen }: { base: string; onOpen: (id: number) => void }) {
  const { data, loading } = useAsync(() => api.get<{ buckets: { bucket: string; amount: number; count: number }[]; invoices: Invoice[] }>(`${V1}/reports/aging`))
  if (loading || !data) return <Loading />
  const daysOver = (d: string) => Math.floor((Date.now() - new Date(d + 'T00:00:00').getTime()) / 86400000)
  return (
    <div className="grid">
      <div className="grid cols-5">{data.buckets.map(b => <div key={b.bucket} className={`card stat ${b.bucket === '90+' || b.bucket === '61-90' ? 'danger' : ''}`}><div className="label">{b.bucket === 'current' ? 'Not yet due' : `${b.bucket} days overdue`}</div><div className="value">{money(b.amount, base)}</div><div className="hint">{b.count} invoices</div></div>)}</div>
      <Card title="Open invoices" flush>{!data.invoices.length ? <Empty title="Nothing outstanding 🎉" /> : <table className="table"><thead><tr><th>Number</th><th>Client</th><th>Due</th><th className="num">Days overdue</th><th>Status</th><th className="num">Balance</th><th className="num">In {base}</th></tr></thead>
        <tbody>{data.invoices.map(i => { const d = daysOver(i.due_date); return <tr key={i.id} className="clickable" onClick={() => onOpen(i.id)}><td className="bold">{i.number}</td><td>{i.client_name}</td><td className="muted">{fmtDate(i.due_date)}</td><td className="num" style={d > 0 ? { color: 'var(--danger)' } : {}}>{d > 0 ? d : '—'}</td><td><Badge status={i.status} /></td><td className="num">{money(i.balance, i.currency)}</td><td className="num muted">{money(i.balance * i.exchange_rate, base)}</td></tr> })}</tbody></table>}</Card>
    </div>
  )
}

function Tax({ q, base }: { q: string; base: string }) {
  const { data, loading } = useAsync(() => api.get<{ rates: { rate: number; taxable: number; tax: number }[] }>(`${V1}/reports/tax?${q}`), [q])
  if (loading || !data) return <Loading />
  return <Card title="Tax collected by rate (issued invoices)" flush>{!data.rates.length ? <Empty title="No data in range" /> : <table className="table"><thead><tr><th>Rate</th><th className="num">Taxable amount</th><th className="num">Tax</th></tr></thead>
    <tbody>{data.rates.map(r => <tr key={r.rate}><td>{r.rate}%</td><td className="num">{money(r.taxable, base)}</td><td className="num">{money(r.tax, base)}</td></tr>)}</tbody>
    <tfoot><tr><td>Total</td><td className="num">{money(data.rates.reduce((a, r) => a + r.taxable, 0), base)}</td><td className="num">{money(data.rates.reduce((a, r) => a + r.tax, 0), base)}</td></tr></tfoot></table>}</Card>
}

function Currencies({ q, base }: { q: string; base: string }) {
  const { data, loading } = useAsync(() => api.get<{ currencies: { currency: string; invoiced: number; paid: number; outstanding: number; in_base: number; count: number }[] }>(`${V1}/reports/currencies?${q}`), [q])
  if (loading || !data) return <Loading />
  return <Card title="Invoicing by currency" flush>{!data.currencies.length ? <Empty title="No data in range" /> : <table className="table"><thead><tr><th>Currency</th><th className="num">Invoices</th><th className="num">Invoiced</th><th className="num">Received</th><th className="num">Outstanding</th><th className="num">Invoiced in {base}</th></tr></thead>
    <tbody>{data.currencies.map(c => <tr key={c.currency}><td className="bold">{c.currency}</td><td className="num">{c.count}</td><td className="num">{money(c.invoiced, c.currency)}</td><td className="num">{money(c.paid, c.currency)}</td><td className="num">{money(c.outstanding, c.currency)}</td><td className="num">{money(c.in_base, base)}</td></tr>)}</tbody></table>}</Card>
}

function Time({ q }: { q: string }) {
  const { data, loading } = useAsync(() => api.get<{ rows: { client_id: number; client_name: string; project: string; minutes: number; billable_minutes: number; unbilled_minutes: number; entries: number }[] }>(`${V1}/reports/time?${q}`), [q])
  if (loading || !data) return <Loading />
  const tot = (k: 'minutes' | 'billable_minutes' | 'unbilled_minutes') => data.rows.reduce((a, r) => a + r[k], 0)
  return <Card title="Tracked time by client and project" flush>{!data.rows.length ? <Empty title="No time tracked in range" /> : <table className="table"><thead><tr><th>Client</th><th>Project</th><th className="num">Entries</th><th className="num">Total</th><th className="num">Billable</th><th className="num">Unbilled</th></tr></thead>
    <tbody>{data.rows.map((r, i) => <tr key={i}><td className="bold">{r.client_name}</td><td className="muted">{r.project || '—'}</td><td className="num">{r.entries}</td><td className="num">{hours(r.minutes)}</td><td className="num">{hoursDecimal(r.billable_minutes)} h</td><td className="num">{hoursDecimal(r.unbilled_minutes)} h</td></tr>)}</tbody>
    <tfoot><tr><td colSpan={3}>Total</td><td className="num">{hours(tot('minutes'))}</td><td className="num">{hoursDecimal(tot('billable_minutes'))} h</td><td className="num">{hoursDecimal(tot('unbilled_minutes'))} h</td></tr></tfoot></table>}</Card>
}

interface TaxRow { month: string; income: number; expenses: number; taxable: number; tax: number; contributions: number; net: number; elapsed: boolean }
interface IncomeTaxReport { year: number; rows: TaxRow[]; total: TaxRow; years: { year: string; income: number; expenses: number; taxable: number; tax: number; contributions: number; net: number }[]; settings: { rate: number; basis: string; by_payment_date: boolean; min_yearly: number; deduction: number; contributions_monthly: number; label: string } }

function IncomeTax({ year, base }: { year: string; base: string }) {
  const { data, loading } = useAsync(() => api.get<IncomeTaxReport>(`${V1}/reports/income-tax?year=${year}`), [year])
  if (loading || !data) return <Loading />
  const st = data.settings
  const t = data.total
  const showExp = st.basis === 'profit' || data.rows.some(r => r.expenses)
  const showContrib = st.contributions_monthly > 0
  return (
    <div className="grid">
      <div className="grid cols-4">
        <div className="card stat"><div className="label">Income {year}</div><div className="value">{money(t.income, base)}</div><div className="hint">{st.by_payment_date ? 'by payment date' : 'by invoice date'}</div></div>
        <div className="card stat"><div className="label">Taxable base</div><div className="value">{money(t.taxable, base)}</div><div className="hint">{st.basis === 'profit' ? 'income − expenses' : 'gross income'}{st.deduction ? ` − ${money(st.deduction, base)} allowance` : ''}</div></div>
        <div className="card stat danger"><div className="label">{st.label || 'Income tax'} ({st.rate}%)</div><div className="value">{money(t.tax, base)}</div><div className="hint">{st.min_yearly && t.tax === st.min_yearly ? 'yearly minimum applied' : `${st.rate}% of taxable base`}{showContrib ? ` + ${money(t.contributions, base)} contributions` : ''}</div></div>
        <div className="card stat success"><div className="label">Net after tax</div><div className="value">{money(t.net, base)}</div><div className="hint">income − expenses − tax{showContrib ? ' − contributions' : ''}</div></div>
      </div>
      <Card title={`Monthly breakdown ${year}`} flush>
        <table className="table"><thead><tr><th>Month</th><th className="num">Income</th>{showExp && <th className="num">Expenses</th>}<th className="num">Taxable</th><th className="num">{st.label || 'Tax'} {st.rate}%</th>{showContrib && <th className="num">Contributions</th>}<th className="num">Net</th></tr></thead>
          <tbody>{data.rows.map(r => <tr key={r.month} style={!r.elapsed ? { opacity: .45 } : {}}><td>{monthLabel(r.month)}</td><td className="num">{money(r.income, base)}</td>{showExp && <td className="num">{money(r.expenses, base)}</td>}<td className="num">{money(r.taxable, base)}</td><td className="num">{money(r.tax, base)}</td>{showContrib && <td className="num">{money(r.contributions, base)}</td>}<td className="num">{money(r.net, base)}</td></tr>)}</tbody>
          <tfoot><tr><td>Year {year}</td><td className="num">{money(t.income, base)}</td>{showExp && <td className="num">{money(t.expenses, base)}</td>}<td className="num">{money(t.taxable, base)}</td><td className="num">{money(t.tax, base)}</td>{showContrib && <td className="num">{money(t.contributions, base)}</td>}<td className="num">{money(t.net, base)}</td></tr></tfoot></table>
        <p className="muted small" style={{ padding: 12 }}>Estimate only, based on Settings → Taxes → Income tax estimate. Yearly minimum and allowance are applied to the whole year, so the monthly tax column may not sum to the yearly figure. Always confirm with your accountant.</p>
      </Card>
      <Card title="All years" flush>
        <table className="table"><thead><tr><th>Year</th><th className="num">Income</th>{showExp && <th className="num">Expenses</th>}<th className="num">Taxable</th><th className="num">{st.label || 'Tax'}</th>{showContrib && <th className="num">Contributions</th>}<th className="num">Net</th></tr></thead>
          <tbody>{data.years.map(y => <tr key={y.year} className={y.year === year ? 'bold' : ''}><td>{y.year}</td><td className="num">{money(y.income, base)}</td>{showExp && <td className="num">{money(y.expenses, base)}</td>}<td className="num">{money(y.taxable, base)}</td><td className="num">{money(y.tax, base)}</td>{showContrib && <td className="num">{money(y.contributions, base)}</td>}<td className="num">{money(y.net, base)}</td></tr>)}</tbody>
          <tfoot><tr><td>Total</td><td className="num">{money(data.years.reduce((a, y) => a + y.income, 0), base)}</td>{showExp && <td className="num">{money(data.years.reduce((a, y) => a + y.expenses, 0), base)}</td>}<td className="num">{money(data.years.reduce((a, y) => a + y.taxable, 0), base)}</td><td className="num">{money(data.years.reduce((a, y) => a + y.tax, 0), base)}</td>{showContrib && <td className="num">{money(data.years.reduce((a, y) => a + y.contributions, 0), base)}</td>}<td className="num">{money(data.years.reduce((a, y) => a + y.net, 0), base)}</td></tr></tfoot></table>
      </Card>
    </div>
  )
}
