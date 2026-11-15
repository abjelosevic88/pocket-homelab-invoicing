import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { addDays, BILLING_MODES, money, taxRateOptions, today, UNITS, unitForBilling } from '../lib/format'
import type { Client, InvoiceItem, InvoiceTemplate, Product, Quote, TaxRate } from '../lib/types'
import { Card, Field, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { useCrumb } from '../components/shell'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { computeTotals } from './InvoiceEditor'

type Item = InvoiceItem & { key: number }
let keySeq = 1
const newItem = (unit: string, tax: number, price = 0): Item => ({ key: keySeq++, description: '', unit, quantity: 1, unit_price: price, tax_rate: tax, discount: 0 })

export default function QuoteEditor() {
  const { id } = useParams()
  const [sp] = useSearchParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { settings, currencies } = useApp()
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients`))
  const { data: products } = useAsync(() => api.get<Product[]>(`${V1}/products`))
  const { data: taxRates } = useAsync(() => api.get<TaxRate[]>(`${V1}/tax-rates`))
  const { data: templates } = useAsync(() => api.get<InvoiceTemplate[]>(`${V1}/templates`))
  const [q, setQ] = useState<Partial<Quote> | null>(null)
  const [items, setItems] = useState<Item[]>([])
  const [busy, setBusy] = useState(false)
  useCrumb(id ? q?.number : undefined)

  useEffect(() => {
    if (!settings) return
    if (id) {
      api.get<Quote>(`${V1}/quotes/${id}`).then(x => { setQ(x); setItems((x.items || []).map(it => ({ ...it, key: keySeq++ }))) })
    } else {
      const issue = today()
      setQ({ client_id: Number(sp.get('client_id')) || 0, issue_date: issue, valid_until: addDays(issue, settings.quote_valid_days || 30), currency: settings.base_currency, billing_mode: settings.default_billing_mode || 'hourly', discount_type: 'none', discount_value: 0, notes: '', terms: settings.quote_terms || '', footer: settings.default_footer, po_number: '', template_id: null, status: 'draft' })
      setItems([newItem(unitForBilling(settings.default_billing_mode || 'hourly'), settings.default_tax_rate, settings.default_hourly_rate)])
    }
  }, [id, settings, sp])

  const client = useMemo(() => clients?.find(c => c.id === q?.client_id), [clients, q?.client_id])
  const onClientChange = (cid: number) => {
    const c = clients?.find(x => x.id === cid)
    setQ(x => {
      if (!x) return x
      const next: Partial<Quote> = { ...x, client_id: cid }
      if (c && !id) {
        next.currency = c.currency
        next.billing_mode = c.billing_mode
        if (c.default_rate) setItems(its => its.map((it, idx) => idx === 0 && !it.description && it.unit_price === (settings?.default_hourly_rate || 0) ? { ...it, unit_price: c.default_rate, unit: unitForBilling(c.billing_mode) } : it))
      }
      return next
    })
  }

  // A new quote opened from a client takes that client's currency, billing mode and rate once the list is in.
  const prefilled = useRef(false)
  useEffect(() => {
    if (id || prefilled.current || !clients || !q?.client_id) return
    prefilled.current = true
    onClientChange(q.client_id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clients, q?.client_id, id])

  if (!q || !settings) return <Loading />
  const locked = q.status === 'accepted' || q.status === 'invoiced'
  const cur = currencies.find(c => c.code === q.currency)
  const totals = computeTotals(items, q.discount_type || 'none', q.discount_value || 0, cur?.decimals ?? 2)
  const set = (k: keyof Quote, v: unknown) => setQ(x => ({ ...x!, [k]: v }))
  const setItem = (key: number, patch: Partial<Item>) => setItems(its => its.map(it => it.key === key ? { ...it, ...patch } : it))
  const addProduct = (p: Product) => setItems(its => [...its.filter(it => it.description || it.unit_price), { ...newItem(p.unit, p.tax_rate, p.unit_price), description: p.description ? `${p.name} – ${p.description}` : p.name }])
  const move = (idx: number, dir: -1 | 1) => setItems(its => { const n = [...its]; const j = idx + dir; if (j < 0 || j >= n.length) return its; [n[idx], n[j]] = [n[j], n[idx]]; return n })
  const mode = q.billing_mode || 'hourly'
  const enabled = currencies.filter(c => c.enabled || c.code === q.currency)
  const revising = !!id && ['sent', 'viewed', 'declined', 'expired'].includes(q.status || '')

  const save = async () => {
    if (!q.client_id) { toast('Please choose a client', 'error'); return }
    if (!items.some(it => it.description.trim())) { toast('Add at least one line', 'error'); return }
    setBusy(true)
    try {
      const body = { ...q, items: items.map(({ key, ...it }) => it), template_id: q.template_id || null }
      const saved = id ? await api.put<Quote>(`${V1}/quotes/${id}`, body) : await api.post<Quote>(`${V1}/quotes`, body)
      toast(id ? (saved.revision > (q.revision || 1) ? `Saved as revision ${saved.revision}` : 'Quote saved') : `Quote ${saved.number} created`, 'success')
      navigate(`/quotes/${saved.id}`)
    } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }

  if (locked) return (
    <>
      <PageHeader title={`Quote ${q.number}`} />
      <div className="callout warn">The client accepted this quote, so it can no longer be changed. Duplicate it from the quote page to make a new one.</div>
    </>
  )

  const actions = <>
    <button type="button" className="btn" onClick={() => navigate(-1)}>Cancel</button>
    <button type="button" className="btn primary" disabled={busy} onClick={save}>{id ? (revising ? 'Save new revision' : 'Save') : 'Save as draft'}</button>
  </>
  return (
    <>
      <PageHeader title={id ? `Edit ${q.number}` : 'New quote'} sub={id ? undefined : 'Pick a client, add lines, then send it from the next page.'} actions={actions} />
      {revising && <div className="callout mb">{q.status === 'declined' || q.status === 'expired' ? <>This quote was {q.status}. Saving makes revision {(q.revision || 1) + 1} and puts it back to draft, so you can send it again.</> : <>The client may already have seen this quote. Saving makes revision {(q.revision || 1) + 1}; their link shows the new version.</>}</div>}
      <div className="grid split-3-2 mb">
        <Card title="Client & dates">
          <div className="form-grid">
            <Field label="Client" className="full">
              <Select value={String(q.client_id || '')} onChange={v => onClientChange(Number(v))} autoFocus={!id} placeholder="Select a client…"
                options={[{ value: '', label: 'Select a client…' }, ...(clients || []).map(c => ({ value: String(c.id), label: c.name, hint: c.currency }))]} />
            </Field>
            <Field label="Quote number" help={id ? undefined : 'Leave blank to auto-number'}><input value={q.number || ''} onChange={e => set('number', e.target.value)} placeholder="auto" /></Field>
            <Field label="PO / reference"><input value={q.po_number || ''} onChange={e => set('po_number', e.target.value)} /></Field>
            <Field label="Date"><input type="date" value={q.issue_date} onChange={e => { const d = e.target.value; setQ(x => ({ ...x!, issue_date: d, valid_until: !id && d ? addDays(d, settings.quote_valid_days || 30) : x!.valid_until })) }} /></Field>
            <Field label="Valid until"><input type="date" value={q.valid_until} onChange={e => set('valid_until', e.target.value)} /></Field>
          </div>
        </Card>
        <Card title="Billing & currency">
          <div className="form-grid">
            <Field label="Billing mode"><Select value={mode} onChange={v => { set('billing_mode', v); setItems(its => its.map(it => it.description ? it : { ...it, unit: unitForBilling(v) })) }} options={BILLING_MODES.map(b => ({ value: b.value, label: b.label }))} /></Field>
            <Field label="Currency"><Select value={q.currency || ''} onChange={v => set('currency', v)} options={enabled.map(c => ({ value: c.code, label: c.code, hint: c.name }))} /></Field>
            <Field label="Template" help="Prints QUOTE and Valid until instead of the invoice words"><Select value={String(q.template_id || '')} onChange={v => set('template_id', Number(v) || null)} placeholder="Default" options={[{ value: '', label: 'Default' }, ...(templates || []).map(t => ({ value: String(t.id), label: t.name, hint: t.kind === 'docx' ? 'Word' : undefined }))]} /></Field>
            <Field label="Document discount">
              <div className="input-group">
                <Select className="discount-type" ariaLabel="Discount type" value={q.discount_type || 'none'} onChange={v => set('discount_type', v)} options={[{ value: 'none', label: 'None' }, { value: 'percent', label: 'Percent' }, { value: 'fixed', label: 'Fixed' }]} />
                <input type="number" step="0.01" disabled={q.discount_type === 'none'} value={q.discount_value || ''} onChange={e => set('discount_value', parseFloat(e.target.value) || 0)} />
              </div>
            </Field>
          </div>
        </Card>
      </div>

      <Card title="Line items" actions={products?.length ? <Select className="catalog-pick" value="" ariaLabel="Add from catalog" placeholder="Add from catalog…" onChange={v => { const p = products.find(x => x.id === Number(v)); if (p) addProduct(p) }} options={products.map(p => ({ value: String(p.id), label: p.name, hint: `${money(p.unit_price, p.currency || q.currency!)}/${p.unit}` }))} /> : undefined} flush>
        <div className="table-wrap"><table className="table items-table">
          <thead><tr><th style={{ width: '40%' }}>Description</th><th style={{ width: 90 }}>Unit</th><th style={{ width: 90 }} className="num">Qty</th><th style={{ width: 120 }} className="num">Rate</th><th style={{ width: 80 }} className="num">Disc %</th><th style={{ width: 110 }} className="num">Tax</th><th className="num">Amount</th><th style={{ width: 90 }}></th></tr></thead>
          <tbody>{items.map((it, idx) => <tr key={it.key}>
            <td><textarea rows={1} value={it.description} onChange={e => setItem(it.key, { description: e.target.value })} placeholder="What will you do?" /></td>
            <td><Select floating className="cell" ariaLabel="Unit" value={it.unit} onChange={v => setItem(it.key, { unit: v })} options={UNITS.map(u => ({ value: u.value, label: u.label }))} /></td>
            <td><input type="number" step="1" className="right" value={it.quantity} onChange={e => setItem(it.key, { quantity: parseFloat(e.target.value) || 0 })} /></td>
            <td><input type="number" step="0.01" className="right" value={it.unit_price} onChange={e => setItem(it.key, { unit_price: parseFloat(e.target.value) || 0 })} /></td>
            <td><input type="number" step="0.01" className="right" value={it.discount || ''} onChange={e => setItem(it.key, { discount: parseFloat(e.target.value) || 0 })} placeholder="0" /></td>
            <td><Select floating className="cell" ariaLabel="Tax" value={String(it.tax_rate)} onChange={v => setItem(it.key, { tax_rate: parseFloat(v) })} options={taxRateOptions(taxRates, it.tax_rate)} /></td>
            <td className="num bold">{money(totals.lines[idx], q.currency!)}</td>
            <td className="actions"><button type="button" className="btn ghost icon-only sm" aria-label="Move up" onClick={() => move(idx, -1)} title="Move up"><Icon name="chevron-up" size={14} /></button><button type="button" className="btn ghost icon-only sm" aria-label="Move down" onClick={() => move(idx, 1)} title="Move down"><Icon name="chevron-down" size={14} /></button><button type="button" className="btn ghost icon-only sm" aria-label="Delete" onClick={() => setItems(its => its.filter(x => x.key !== it.key))} title="Remove"><Icon name="trash" size={14} /></button></td>
          </tr>)}</tbody>
        </table></div>
        <div className="row between" style={{ padding: 14 }}>
          <button type="button" className="btn" onClick={() => setItems(its => [...its, newItem(unitForBilling(mode), settings.default_tax_rate, client?.default_rate || 0)])}><Icon name="plus" size={15} />Add line</button>
          <div className="totals-box">
            <div className="line"><span className="muted">Subtotal</span><span>{money(totals.subtotal, q.currency!)}</span></div>
            {totals.discount > 0 && <div className="line"><span className="muted">Discount</span><span>-{money(totals.discount, q.currency!)}</span></div>}
            {Object.entries(totals.breakdown).map(([r, v]) => <div className="line" key={r}><span className="muted">Tax {r}%</span><span>{money(v, q.currency!)}</span></div>)}
            <div className="line grand"><span>Total</span><span>{money(totals.total, q.currency!)}</span></div>
          </div>
        </div>
      </Card>

      <div className="grid cols-3 mt">
        <Field label="Notes (shown on the quote)"><textarea value={q.notes || ''} onChange={e => set('notes', e.target.value)} placeholder="Scope, timeline, what's included…" /></Field>
        <Field label="Terms" help="{valid_until} is replaced"><textarea value={q.terms || ''} onChange={e => set('terms', e.target.value)} /></Field>
        <Field label="Footer"><textarea value={q.footer || ''} onChange={e => set('footer', e.target.value)} /></Field>
      </div>
      <div className="form-actions">{actions}</div>
    </>
  )
}
