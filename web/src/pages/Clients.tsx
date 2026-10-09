import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { BILLING_MODES, money } from '../lib/format'
import type { Client, InvoiceTemplate, PaperlessEntry } from '../lib/types'
import { usePaperless } from '../components/paperless'
import { Card, Empty, Field, Loading, Modal, PageHeader, useAsync, useDebounce, useToast } from '../components/ui'

const blank = (currency: string, terms: number): Partial<Client> => ({ name: '', contact_name: '', email: '', phone: '', address1: '', address2: '', city: '', state: '', postal_code: '', country: '', tax_id: '', website: '', currency, billing_mode: 'hourly', default_rate: 0, payment_terms_days: terms, notes: '', email_subject: '', email_body: '', email_cc: '' })

export function ClientForm({ initial, onSaved, onClose }: { initial?: Client; onSaved: (c: Client) => void; onClose: () => void }) {
  const { settings, currencies } = useApp()
  const { data: templates } = useAsync(() => api.get<InvoiceTemplate[]>(`${V1}/templates`))
  const paperless = usePaperless()
  const { data: correspondents } = useAsync(() => paperless?.configured ? api.get<PaperlessEntry[]>(`${V1}/paperless/correspondents`) : Promise.resolve(null), [paperless?.configured])
  const [createCorr, setCreateCorr] = useState(false)
  const [c, setC] = useState<Partial<Client>>(initial ?? blank(settings?.base_currency || 'EUR', settings?.default_due_days || 14))
  const [busy, setBusy] = useState(false)
  const toast = useToast()
  const set = (k: keyof Client) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => setC(x => ({ ...x, [k]: e.target.type === 'number' ? parseFloat(e.target.value) || 0 : e.target.value }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body = { ...c }
      if (createCorr && c.name) { const e = await api.post<PaperlessEntry>(`${V1}/paperless/correspondents`, { name: c.name }); body.paperless_correspondent_id = e.id; body.paperless_correspondent = e.name }
      const saved = initial ? await api.put<Client>(`${V1}/clients/${initial.id}`, body) : await api.post<Client>(`${V1}/clients`, body)
      toast(initial ? 'Client updated' : 'Client created', 'success')
      onSaved(saved)
    } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  const enabled = currencies.filter(x => x.enabled)
  return (
    <form onSubmit={submit}>
      <div className="form-grid">
        <Field label="Company / client name" className="full"><input value={c.name} onChange={set('name')} required autoFocus /></Field>
        <Field label="Contact person"><input value={c.contact_name} onChange={set('contact_name')} /></Field>
        <Field label="Email" help="Invoices are sent here (comma-separate multiple)"><input value={c.email} onChange={set('email')} /></Field>
        <Field label="Phone"><input value={c.phone} onChange={set('phone')} /></Field>
        <Field label="Website"><input value={c.website} onChange={set('website')} /></Field>
        <Field label="Address line 1" className="full"><input value={c.address1} onChange={set('address1')} /></Field>
        <Field label="Address line 2" className="full"><input value={c.address2} onChange={set('address2')} /></Field>
        <Field label="Postal code"><input value={c.postal_code} onChange={set('postal_code')} /></Field>
        <Field label="City"><input value={c.city} onChange={set('city')} /></Field>
        <Field label="State / region"><input value={c.state} onChange={set('state')} /></Field>
        <Field label="Country"><input value={c.country} onChange={set('country')} /></Field>
        <Field label="Tax / VAT ID"><input value={c.tax_id} onChange={set('tax_id')} /></Field>
        <Field label="Currency"><select value={c.currency} onChange={set('currency')}>{enabled.map(x => <option key={x.code} value={x.code}>{x.code} – {x.name}</option>)}</select></Field>
        <Field label="Default billing mode"><select value={c.billing_mode} onChange={set('billing_mode')}>{BILLING_MODES.map(b => <option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
        <Field label="Default rate" help="Per hour / day / month depending on billing mode"><input type="number" step="0.01" value={c.default_rate} onChange={set('default_rate')} /></Field>
        <Field label="Payment terms (days)" help="0 = due on receipt"><input type="number" value={c.payment_terms_days} onChange={set('payment_terms_days')} /></Field>
        {paperless?.configured && <Field label="Paperless correspondent" help="Documents of this correspondent show on the client page; everything archived for this client is filed under it">
          <select value={createCorr ? '__create__' : (c.paperless_correspondent_id || '')} onChange={e => { if (e.target.value === '__create__') { setCreateCorr(true) } else { setCreateCorr(false); setC(x => ({ ...x, paperless_correspondent_id: Number(e.target.value) || 0, paperless_correspondent: correspondents?.find(k => k.id === Number(e.target.value))?.name || '' })) } }}>
            <option value="">— not linked —</option>
            {correspondents?.map(k => <option key={k.id} value={k.id}>{k.name}</option>)}
            {c.name && !correspondents?.some(k => k.name.toLowerCase() === c.name!.toLowerCase()) && <option value="__create__">Create "{c.name}" in Paperless</option>}
          </select>
        </Field>}
        <Field label="Invoice template" help="Used for this client's invoices unless an invoice picks another"><select value={c.template_id || ''} onChange={e => setC(x => ({ ...x, template_id: Number(e.target.value) || null }))}><option value="">Global default</option>{templates?.map(t => <option key={t.id} value={t.id}>{t.name}{t.kind === 'docx' ? ' (Word)' : ''}</option>)}</select></Field>
        <Field label="Internal notes" className="full"><textarea value={c.notes} onChange={set('notes')} /></Field>
        <details className="full"><summary className="small bold" style={{ cursor: 'pointer' }}>Email template for this client (optional, overrides Settings → Email)</summary>
          <div className="grid mt" style={{ gap: 12 }}>
            <Field label="Subject"><input value={c.email_subject || ''} onChange={set('email_subject')} placeholder={settings?.email_subject} /></Field>
            <Field label="Message" help="Placeholders: {number} {number_short} {client} {contact} {first_name} {company} {total} {balance} {due_date} {issue_date} {month} {year} {period} {link}"><textarea rows={7} value={c.email_body || ''} onChange={set('email_body')} placeholder={settings?.email_body} /></Field>
            <Field label="Always CC" help="Comma separated"><input value={c.email_cc || ''} onChange={set('email_cc')} placeholder="accounting@client.example" /></Field>
          </div>
        </details>
      </div>
      <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{initial ? 'Save changes' : 'Create client'}</button></div>
    </form>
  )
}

export default function Clients() {
  const [q, setQ] = useState('')
  const [archived, setArchived] = useState(false)
  const dq = useDebounce(q)
  const { data, loading, reload } = useAsync(() => api.get<Client[]>(`${V1}/clients?q=${encodeURIComponent(dq)}&archived=${archived ? 1 : 0}`), [dq, archived])
  const [creating, setCreating] = useState(false)
  const navigate = useNavigate()
  return (
    <>
      <PageHeader title="Clients" sub="Who you bill, in which currency, and how." actions={<button className="btn primary" onClick={() => setCreating(true)}>+ New client</button>} />
      <div className="filters">
        <input className="search" placeholder="Search clients…" value={q} onChange={e => setQ(e.target.value)} />
        <label className="check"><input type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} /> Show archived</label>
      </div>
      <Card flush>
        {loading && !data ? <Loading /> : !data?.length ? <Empty title="No clients yet">Add a client to start invoicing.</Empty> : (
          <div className="table-wrap"><table className="table">
            <thead><tr><th>Name</th><th>Contact</th><th>Currency</th><th>Billing</th><th className="num">Invoices</th><th className="num">Outstanding</th><th className="num">Total billed</th></tr></thead>
            <tbody>{data.map(c => <tr key={c.id} className="clickable" onClick={() => navigate(`/clients/${c.id}`)}>
              <td><div className="bold">{c.name}{c.archived && <span className="badge cancelled" style={{ marginLeft: 8 }}>archived</span>}</div><div className="muted small">{c.city}{c.city && c.country ? ', ' : ''}{c.country}</div></td>
              <td><div>{c.contact_name}</div><div className="muted small">{c.email}</div></td>
              <td>{c.currency}</td>
              <td className="muted">{BILLING_MODES.find(b => b.value === c.billing_mode)?.label}{c.default_rate ? ` · ${money(c.default_rate, c.currency)}` : ''}</td>
              <td className="num">{c.invoice_count ?? 0}</td>
              <td className="num">{c.outstanding ? <span style={{ color: 'var(--warning)' }}>{money(c.outstanding, c.currency)}</span> : <span className="muted">—</span>}</td>
              <td className="num">{money(c.total_billed, c.currency)}</td>
            </tr>)}</tbody>
          </table></div>
        )}
      </Card>
      {creating && <Modal title="New client" size="lg" onClose={() => setCreating(false)}><ClientForm onSaved={c => { setCreating(false); reload(); navigate(`/clients/${c.id}`) }} onClose={() => setCreating(false)} /></Modal>}
    </>
  )
}
