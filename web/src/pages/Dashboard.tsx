import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate, fmtDateTime, hours, money, monthLabel } from '../lib/format'
import type { Activity, Currency, DashboardStats, Invoice, MonthlyRevenue, Recurring, TimeEntry } from '../lib/types'
import { Badge, Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'

interface Dash { stats: DashboardStats; base_currency: Currency; revenue: MonthlyRevenue[]; recent: Invoice[]; overdue: Invoice[]; running_timer: TimeEntry | null; activity: Activity[]; upcoming_recurring: Recurring[] }

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

export default function Dashboard() {
  const { data, loading, error } = useAsync(() => api.get<Dash>(`${V1}/dashboard`))
  const navigate = useNavigate()
  if (loading || !data) return <Loading />
  if (error) return <div className="callout danger">{error}</div>
  const { stats, base_currency: bc } = data
  const code = bc.code
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
      <div className="grid cols-2 mb" style={{ gridTemplateColumns: '2fr 1fr' }}>
        <Card title="Revenue (last 12 months)"><RevenueChart months={data.revenue} code={code} /></Card>
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
