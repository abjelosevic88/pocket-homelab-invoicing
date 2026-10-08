import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate, fmtDateTime, hours, money, monthLabel } from '../lib/format'
import type { Activity, ClientRevenue, Currency, DashboardStats, Invoice, Lifetime, MonthlyRevenue, Recurring, TimeEntry, YearRevenue } from '../lib/types'
import { Badge, Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'

interface Dash { stats: DashboardStats; base_currency: Currency; revenue: MonthlyRevenue[]; recent: Invoice[]; overdue: Invoice[]; running_timer: TimeEntry | null; activity: Activity[]; upcoming_recurring: Recurring[]; lifetime: Lifetime; by_year: YearRevenue[]; by_client: ClientRevenue[] }

export function RevenueChart({ months, code }: { months: MonthlyRevenue[]; code: string }) {
  if (!months.length) return <Empty title="No revenue yet">Issue your first invoice to see the chart.</Empty>
  const max = Math.max(1, ...months.map(m => Math.max(m.invoiced, m.paid, m.expenses)))
  return (
    <div>
      <div className="bar-chart">
        {months.map(m => (
          <div className="col" key={m.month} title={`${m.month}: invoiced ${money(m.invoiced, code)}, paid ${money(m.paid, code)}${m.expenses ? `, expenses ${money(m.expenses, code)}` : ''}`}>
            <div className="bars">
              <div className="bar" style={{ height: `${(m.invoiced / max) * 100}%` }} />
              <div className="bar paid" style={{ height: `${(m.paid / max) * 100}%` }} />
              {m.expenses > 0 && <div className="bar exp" style={{ height: `${(m.expenses / max) * 100}%` }} />}
            </div>
            <span className="lbl">{monthLabel(m.month)}</span>
          </div>
        ))}
      </div>
      <div className="legend mt"><span>Invoiced</span><span className="paid">Paid</span><span className="exp">Expenses</span></div>
    </div>
  )
}

export function HBars({ rows, code }: { rows: { label: string; value: number; sub?: string }[]; code: string }) {
  const max = Math.max(1, ...rows.map(r => r.value))
  return <div className="hbar">{rows.map(r => <><span key={r.label + 'l'} className="bold" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.label}{r.sub && <span className="muted small"> · {r.sub}</span>}</span><div key={r.label + 't'} className="track"><div style={{ width: `${(r.value / max) * 100}%` }} /></div><span key={r.label + 'v'} className="val">{money(r.value, code)}</span></>)}</div>
}

export default function Dashboard() {
  const [months, setMonths] = useState(12)
  const { data, loading, error } = useAsync(() => api.get<Dash>(`${V1}/dashboard?months=${months}`), [months])
  const navigate = useNavigate()
  if (loading && !data) return <Loading />
  if (error || !data) return <div className="callout danger">{error}</div>
  const { stats, base_currency: bc, lifetime: lt } = data
  const code = bc.code
  const avgMonth = lt.months_active ? lt.paid_total / lt.months_active : 0
  const bestYear = [...data.by_year].sort((a, b) => b.paid - a.paid)[0]
  return (
    <>
      <PageHeader title="Dashboard" sub={`All amounts in ${code} (base currency)`} actions={<>
        <Link className="btn" to="/time">Track time</Link>
        <Link className="btn primary" to="/invoices/new">New invoice</Link>
      </>} />
      <div className="grid cols-4 mb">
        <div className="card stat"><div className="label">Outstanding</div><div className="value">{money(stats.outstanding, code)}</div><div className="hint">{stats.outstanding_count} open invoice{stats.outstanding_count === 1 ? '' : 's'}</div></div>
        <div className={`card stat ${stats.overdue_count ? 'danger' : ''}`}><div className="label">Overdue</div><div className="value">{money(stats.overdue, code)}</div><div className="hint">{stats.overdue_count} overdue</div></div>
        <div className="card stat success"><div className="label">Paid this month</div><div className="value">{money(stats.paid_this_month, code)}</div><div className="hint">{money(stats.paid_this_year, code)} this year</div></div>
        <div className="card stat"><div className="label">Unbilled time</div><div className="value">{hours(stats.unbilled_minutes)}</div><div className="hint">{stats.unbilled_expenses > 0 ? `+ ${money(stats.unbilled_expenses, code)} expenses` : `${stats.draft_count} draft${stats.draft_count === 1 ? '' : 's'} waiting`}</div></div>
      </div>
      <div className="grid cols-4 mb">
        <div className="card stat success"><div className="label">Earned all time</div><div className="value">{money(lt.paid_total, code)}</div><div className="hint">{Object.entries(lt.paid_by_currency).map(([c, v]) => money(v, c)).join(' + ') || '—'}</div></div>
        <div className="card stat"><div className="label">Invoiced all time</div><div className="value">{money(lt.invoiced_total, code)}</div><div className="hint">{lt.invoice_count} invoices since {fmtDate(lt.first_invoice)}</div></div>
        <div className="card stat"><div className="label">Average per active month</div><div className="value">{money(avgMonth, code)}</div><div className="hint">{lt.months_active} months with invoices</div></div>
        <div className="card stat"><div className="label">Best year</div><div className="value">{bestYear ? money(bestYear.paid, code) : '—'}</div><div className="hint">{bestYear ? `${bestYear.year} · ${bestYear.count} invoices` : ''}</div></div>
      </div>
      <div className="grid cols-2 mb" style={{ gridTemplateColumns: '2fr 1fr' }}>
        <Card title={`Revenue (last ${months} months)`} actions={<div className="btn-group">{[12, 24, 36].map(m => <button key={m} className={`btn sm ${months === m ? 'primary' : ''}`} onClick={() => setMonths(m)}>{m}m</button>)}</div>}><RevenueChart months={data.revenue} code={code} /></Card>
        <Card title="Needs attention" flush>
          {data.overdue.length === 0 && data.stats.draft_count === 0 ? <Empty title="All clear">No overdue invoices.</Empty> : (
            <table className="table">
              <tbody>
                {data.overdue.map(i => <tr key={i.id} className="clickable" onClick={() => navigate(`/invoices/${i.id}`)}>
                  <td><div className="bold">{i.number}</div><div className="muted small">{i.client_name} · due {fmtDate(i.due_date)}</div></td>
                  <td className="num"><Badge status="overdue" /><div className="small">{money(i.balance, i.currency)}</div></td>
                </tr>)}
                {data.stats.draft_count > 0 && <tr className="clickable" onClick={() => navigate('/invoices?status=draft')}><td colSpan={2} className="muted">{data.stats.draft_count} draft invoice{data.stats.draft_count === 1 ? '' : 's'} not yet sent →</td></tr>}
              </tbody>
            </table>
          )}
        </Card>
      </div>
      <div className="grid cols-2 mb">
        <Card title="Earned by year" flush>
          {!data.by_year.length ? <Empty title="No invoices yet" /> : <table className="table"><thead><tr><th>Year</th><th className="num">Invoices</th><th className="num">Invoiced</th><th className="num">Received</th>{data.by_year.some(y => y.expenses) && <th className="num">Expenses</th>}</tr></thead>
            <tbody>{data.by_year.map(y => <tr key={y.year}><td className="bold">{y.year}</td><td className="num">{y.count}</td><td className="num">{money(y.invoiced, code)}</td><td className="num">{money(y.paid, code)}</td>{data.by_year.some(x => x.expenses) && <td className="num">{money(y.expenses, code)}</td>}</tr>)}</tbody>
            <tfoot><tr><td>Total</td><td className="num">{lt.invoice_count}</td><td className="num">{money(lt.invoiced_total, code)}</td><td className="num">{money(lt.paid_total, code)}</td>{data.by_year.some(x => x.expenses) && <td className="num">{money(data.by_year.reduce((a, y) => a + y.expenses, 0), code)}</td>}</tr></tfoot></table>}
        </Card>
        <Card title="Earned by client (all time)">
          {!data.by_client.length ? <Empty title="No clients invoiced yet" /> : <HBars code={code} rows={data.by_client.map(c => ({ label: c.client_name, value: c.paid, sub: `${c.count} inv · ${c.currency}` }))} />}
          <p className="muted small mt">Converted to {code} at each invoice's exchange rate. Details in <Link to="/reports">Reports</Link>.</p>
        </Card>
      </div>
      <div className="grid cols-2" style={{ gridTemplateColumns: '2fr 1fr' }}>
        <Card title="Recent invoices" actions={<Link to="/invoices" className="small">View all →</Link>} flush>
          {data.recent.length === 0 ? <Empty title="No invoices yet"><Link to="/invoices/new">Create your first invoice</Link></Empty> : (
            <div className="table-wrap"><table className="table">
              <thead><tr><th>Number</th><th>Client</th><th>Issued</th><th>Status</th><th className="num">Total</th></tr></thead>
              <tbody>{data.recent.map(i => <tr key={i.id} className="clickable" onClick={() => navigate(`/invoices/${i.id}`)}>
                <td className="bold">{i.number}</td><td>{i.client_name}</td><td className="muted">{fmtDate(i.issue_date)}</td><td><Badge status={i.status} /></td><td className="num">{money(i.total, i.currency)}</td>
              </tr>)}</tbody>
            </table></div>
          )}
        </Card>
        <div className="grid">
          <Card title="Upcoming recurring" flush>
            {data.upcoming_recurring?.length ? <table className="table"><tbody>{data.upcoming_recurring.map(r => <tr key={r.id} className="clickable" onClick={() => navigate('/recurring')}><td><div className="bold">{r.name}</div><div className="muted small">{r.client_name}</div></td><td className="num muted">{fmtDate(r.next_run)}</td></tr>)}</tbody></table> : <Empty title="No recurring profiles"><Link to="/recurring">Set up a retainer →</Link></Empty>}
          </Card>
          <Card title="Activity" flush>
            {data.activity.length ? <table className="table"><tbody>{data.activity.map(a => <tr key={a.id}><td><div>{a.message}</div><div className="muted small">{fmtDateTime(a.created_at)}</div></td></tr>)}</tbody></table> : <Empty title="Nothing yet" />}
          </Card>
        </div>
      </div>
    </>
  )
}
