import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fmtDate, money } from '../lib/format'
import type { Payment } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'

export default function Payments() {
  const navigate = useNavigate()
  const { data, loading } = useAsync(() => api.get<Payment[]>(`${V1}/payments?limit=500`))
  const totals = (data || []).reduce((m, p) => { m[p.currency] = (m[p.currency] || 0) + p.amount; return m }, {} as Record<string, number>)
  return (
    <>
      <PageHeader title="Payments" sub="Everything you've received, newest first." actions={<a className="btn" href={`${V1}/reports/export.csv?type=payments`}>Export CSV</a>} />
      <Card flush>
        {loading ? <Loading /> : !data?.length ? <Empty title="No payments yet">Record payments from an invoice page.</Empty> : <div className="table-wrap"><table className="table">
          <thead><tr><th>Date</th><th>Invoice</th><th>Client</th><th>Method</th><th>Reference</th><th className="num">Amount</th></tr></thead>
          <tbody>{data.map(p => <tr key={p.id} className="clickable" onClick={() => navigate(`/invoices/${p.invoice_id}`)}><td className="muted">{fmtDate(p.date)}</td><td className="bold">{p.invoice_number}</td><td>{p.client_name}</td><td className="muted">{p.method.replace('_', ' ')}</td><td className="muted">{p.reference}</td><td className="num">{money(p.amount, p.currency)}</td></tr>)}</tbody>
          <tfoot><tr><td colSpan={5}>Total received</td><td className="num">{Object.entries(totals).map(([c, v]) => money(v, c)).join(' + ')}</td></tr></tfoot>
        </table></div>}
      </Card>
    </>
  )
}
