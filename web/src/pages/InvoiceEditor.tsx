import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { addDays, BILLING_MODES, money, today, UNITS, unitForBilling } from '../lib/format'
import type { Client, Invoice, InvoiceItem, InvoiceTemplate, Product, TaxRate } from '../lib/types'
import { Card, Field, Loading, PageHeader, useAsync, useDebounce, useToast } from '../components/ui'

type Item = InvoiceItem & { key: number }
let keySeq = 1
const newItem = (unit: string, tax: number, price = 0): Item => ({ key: keySeq++, description: '', unit, quantity: 1, unit_price: price, tax_rate: tax, discount: 0 })

export function computeTotals(items: InvoiceItem[], discountType: string, discountValue: number, decimals = 2) {
  const r = (v: number) => Math.round(v * 10 ** decimals) / 10 ** decimals
  let subtotal = 0
  const lines = items.map(it => { const l = r(it.quantity * it.unit_price * (1 - (it.discount || 0) / 100)); subtotal += l; return l })
  subtotal = r(subtotal)
  let discount = discountType === 'percent' ? r(subtotal * discountValue / 100) : discountType === 'fixed' ? r(discountValue) : 0
  if (discount > subtotal) discount = subtotal
  let tax = 0
  const breakdown: Record<string, number> = {}
  items.forEach((it, i) => {
    if (!it.tax_rate) return
    let base = lines[i]
    if (subtotal && discount) base -= base * discount / subtotal
    const t = r(base * it.tax_rate / 100)
    tax += t
    breakdown[String(it.tax_rate)] = r((breakdown[String(it.tax_rate)] || 0) + t)
  })
  tax = r(tax)
  return { lines, subtotal, discount, tax, total: r(subtotal - discount + tax), breakdown }
}

export default function InvoiceEditor() {
  const { id } = useParams()
  const [sp] = useSearchParams()
  const navigate = useNavigate()
  const toast = useToast()
  const { settings, currencies } = useApp()
  const { data: clients } = useAsync(() => api.get<Client[]>(`${V1}/clients`))
  const { data: products } = useAsync(() => api.get<Product[]>(`${V1}/products`))
  const { data: taxRates } = useAsync(() => api.get<TaxRate[]>(`${V1}/tax-rates`))
  const { data: templates } = useAsync(() => api.get<InvoiceTemplate[]>(`${V1}/templates`))
  const [inv, setInv] = useState<Partial<Invoice> | null>(null)
  const [items, setItems] = useState<Item[]>([])
  const [busy, setBusy] = useState(false)
  const [rateHint, setRateHint] = useState<string>('')
  const [numberHint, setNumberHint] = useState<{ kind: 'error' | 'warn'; text: string } | null>(null)
  const debouncedNumber = useDebounce(inv?.number || '', 400)
  useEffect(() => {
    if (!debouncedNumber || !inv?.client_id) { setNumberHint(null); return }
    api.get<{ same_client: boolean; other_clients: string[] }>(`${V1}/invoices/check-number?number=${encodeURIComponent(debouncedNumber)}&client_id=${inv.client_id}&exclude=${id || 0}`)
      .then(r => setNumberHint(r.same_client ? { kind: 'error', text: 'This client already has an invoice with this number.' } : r.other_clients.length ? { kind: 'warn', text: `Also used for ${r.other_clients.join(', ')} — allowed, but your numbering is normally unique.` } : null))
      .catch(() => setNumberHint(null))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedNumber, inv?.client_id])

  // Load existing or initialise new
  useEffect(() => {
    if (!settings) return
    if (id) {
      api.get<Invoice>(`${V1}/invoices/${id}`).then(i => { setInv(i); setItems((i.items || []).map(it => ({ ...it, key: keySeq++ }))) })
    } else {
      const issue = today()
      setInv({ client_id: Number(sp.get('client_id')) || 0, issue_date: issue, due_date: addDays(issue, settings.default_due_days), currency: settings.base_currency, exchange_rate: 0, billing_mode: settings.default_billing_mode || 'hourly', discount_type: 'none', discount_value: 0, notes: settings.default_notes, terms: settings.default_terms.replace('{due_days}', String(settings.default_due_days)), footer: settings.default_footer, po_number: '', period_start: '', period_end: '', template_id: null, status: 'draft', custom_fields: {} })
      setItems([newItem(unitForBilling(settings.default_billing_mode || 'hourly'), settings.default_tax_rate, settings.default_hourly_rate)])
    }
  }, [id, settings, sp])

  // When client changes on a new invoice, pick up currency, billing mode, terms
  const client = useMemo(() => clients?.find(c => c.id === inv?.client_id), [clients, inv?.client_id])
  const onClientChange = (cid: number) => {
    const c = clients?.find(x => x.id === cid)
    setInv(i => {
      if (!i) return i
      const next: Partial<Invoice> = { ...i, client_id: cid }
      if (c && !id) {
        next.currency = c.currency
        next.billing_mode = c.billing_mode
        next.due_date = addDays(i.issue_date || today(), c.payment_terms_days || settings?.default_due_days || 14)
        next.terms = (settings?.default_terms || '').replace('{due_days}', String(c.payment_terms_days || settings?.default_due_days || 14))
        if (c.default_rate) setItems(its => its.map((it, idx) => idx === 0 && !it.description && it.unit_price === (settings?.default_hourly_rate || 0) ? { ...it, unit_price: c.default_rate, unit: unitForBilling(c.billing_mode) } : it))
      }
      return next
    })
  }

  // Exchange rate hint when currency differs from base
  useEffect(() => {
    if (!inv?.currency || !settings || inv.currency === settings.base_currency) { setRateHint(''); return }
    api.get<{ rate: number; source: string }>(`${V1}/rates/convert?from=${inv.currency}&to=${settings.base_currency}&amount=1&date=${inv.issue_date || ''}`)
      .then(r => setRateHint(`1 ${inv.currency} ≈ ${r.rate.toFixed(5)} ${settings.base_currency} (${r.source === 'stored' ? 'stored rate' : `official list ${r.source}`}; leave 0 to use it)`))
      .catch(() => setRateHint(`No stored rate for ${inv.currency}→${settings.base_currency}. Enter one or add it under Settings → Currencies.`))
  }, [inv?.currency, inv?.issue_date, settings])

  if (!inv || !settings) return <Loading />
  const cur = currencies.find(c => c.code === inv.currency)
  const totals = computeTotals(items, inv.discount_type || 'none', inv.discount_value || 0, cur?.decimals ?? 2)
  const set = (k: keyof Invoice, v: unknown) => setInv(i => ({ ...i!, [k]: v }))
  const setItem = (key: number, patch: Partial<Item>) => setItems(its => its.map(it => it.key === key ? { ...it, ...patch } : it))
  const addProduct = (p: Product) => setItems(its => [...its.filter(it => it.description || it.unit_price), { ...newItem(p.unit, p.tax_rate, p.unit_price), description: p.description ? `${p.name} – ${p.description}` : p.name }])
  const move = (idx: number, dir: -1 | 1) => setItems(its => { const n = [...its]; const j = idx + dir; if (j < 0 || j >= n.length) return its; [n[idx], n[j]] = [n[j], n[idx]]; return n })

  const save = async (status?: string) => {
    if (!inv.client_id) { toast('Please choose a client', 'error'); return }
    if (!items.some(it => it.description.trim())) { toast('Add at least one line item', 'error'); return }
    setBusy(true)
    try {
      const body = { ...inv, status: status ?? inv.status, items: items.map(({ key, ...it }) => it), period_start: inv.period_start || null, period_end: inv.period_end || null, template_id: inv.template_id || null }
      const saved = id ? await api.put<Invoice>(`${V1}/invoices/${id}`, body) : await api.post<Invoice>(`${V1}/invoices`, body)
      toast(id ? 'Invoice saved' : `Invoice ${saved.number} created`, 'success')
      navigate(`/invoices/${saved.id}`)
    } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }

  const mode = inv.billing_mode || 'hourly'
  const enabled = currencies.filter(c => c.enabled || c.code === inv.currency)
  return (
    <>
      <PageHeader title={id ? `Edit ${inv.number}` : 'New invoice'} sub={id ? undefined : 'Pick a client, add lines, save as draft or send.'} actions={<>
        <button className="btn" onClick={() => navigate(-1)}>Cancel</button>
        <button className="btn" disabled={busy} onClick={() => save()}>{id ? 'Save' : 'Save as draft'}</button>
        {(!id || inv.status === 'draft') && <button className="btn primary" disabled={busy} onClick={() => save('sent')}>Save & mark sent</button>}
      </>} />
      <div className="grid split-3-2 mb">
        <Card title="Client & dates">
          <div className="form-grid">
            <Field label="Client" className="full">
              <select value={inv.client_id || ''} onChange={e => onClientChange(Number(e.target.value))} autoFocus={!id}>
                <option value="">Select a client…</option>
                {clients?.map(c => <option key={c.id} value={c.id}>{c.name} ({c.currency})</option>)}
              </select>
            </Field>
            <Field label="Invoice number" help={numberHint ? <span style={{ color: numberHint.kind === 'error' ? 'var(--danger)' : 'var(--warning)' }}>{numberHint.text}</span> : id ? undefined : 'Leave blank to auto-number'}><input value={inv.number || ''} onChange={e => set('number', e.target.value)} placeholder="auto" /></Field>
            <Field label="PO / reference"><input value={inv.po_number || ''} onChange={e => set('po_number', e.target.value)} /></Field>
            <Field label="Issue date"><input type="date" value={inv.issue_date} onChange={e => set('issue_date', e.target.value)} /></Field>
            <Field label="Due date"><input type="date" value={inv.due_date} onChange={e => set('due_date', e.target.value)} /></Field>
            <Field label="Service period from" help="Optional; shown on the invoice"><input type="date" value={inv.period_start || ''} onChange={e => set('period_start', e.target.value)} /></Field>
            <Field label="Service period to"><input type="date" value={inv.period_end || ''} onChange={e => set('period_end', e.target.value)} /></Field>
            {(settings.custom_fields || []).map(f => <Field key={f.key} label={f.label}><input value={inv.custom_fields?.[f.key] || ''} onChange={e => set('custom_fields', { ...(inv.custom_fields || {}), [f.key]: e.target.value })} /></Field>)}
          </div>
        </Card>
        <Card title="Billing & currency">
          <div className="form-grid">
            <Field label="Billing mode"><select value={mode} onChange={e => { set('billing_mode', e.target.value); setItems(its => its.map(it => it.description ? it : { ...it, unit: unitForBilling(e.target.value) })) }}>{BILLING_MODES.map(b => <option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
            <Field label="Currency"><select value={inv.currency} onChange={e => set('currency', e.target.value)}>{enabled.map(c => <option key={c.code} value={c.code}>{c.code} – {c.name}</option>)}</select></Field>
            {inv.currency !== settings.base_currency && <Field label={`Exchange rate → ${settings.base_currency}`} help={rateHint} className="full"><input type="number" step="0.000001" value={inv.exchange_rate || ''} onChange={e => set('exchange_rate', parseFloat(e.target.value) || 0)} placeholder="0 = use stored rate" /></Field>}
            <Field label="Template"><select value={inv.template_id || ''} onChange={e => set('template_id', Number(e.target.value) || null)}><option value="">Default</option>{templates?.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</select></Field>
            <Field label="Document discount">
              <div className="input-group">
                <select value={inv.discount_type} onChange={e => set('discount_type', e.target.value)} style={{ width: 110 }}><option value="none">None</option><option value="percent">Percent</option><option value="fixed">Fixed</option></select>
                <input type="number" step="0.01" disabled={inv.discount_type === 'none'} value={inv.discount_value || ''} onChange={e => set('discount_value', parseFloat(e.target.value) || 0)} />
              </div>
            </Field>
          </div>
        </Card>
      </div>

      <Card title="Line items" actions={products?.length ? <select value="" onChange={e => { const p = products.find(x => x.id === Number(e.target.value)); if (p) addProduct(p) }} style={{ width: 'auto' }}><option value="">+ Add from catalog…</option>{products.map(p => <option key={p.id} value={p.id}>{p.name} — {money(p.unit_price, p.currency || inv.currency!)}/{p.unit}</option>)}</select> : undefined} flush>
        <div className="table-wrap"><table className="table items-table">
          <thead><tr><th style={{ width: '40%' }}>Description</th><th style={{ width: 90 }}>Unit</th><th style={{ width: 90 }} className="num">Qty</th><th style={{ width: 120 }} className="num">Rate</th><th style={{ width: 80 }} className="num">Disc %</th><th style={{ width: 110 }} className="num">Tax</th><th className="num">Amount</th><th style={{ width: 90 }}></th></tr></thead>
          <tbody>{items.map((it, idx) => <tr key={it.key}>
            <td><textarea rows={1} value={it.description} onChange={e => setItem(it.key, { description: e.target.value })} placeholder={mode === 'monthly' ? 'e.g. Managed hosting retainer – {month}' : 'What did you do?'} /></td>
            <td><select value={it.unit} onChange={e => setItem(it.key, { unit: e.target.value })}>{UNITS.map(u => <option key={u.value} value={u.value}>{u.label}</option>)}</select></td>
            <td><input type="number" step="1" className="right" value={it.quantity} onChange={e => setItem(it.key, { quantity: parseFloat(e.target.value) || 0 })} /></td>
            <td><input type="number" step="0.01" className="right" value={it.unit_price} onChange={e => setItem(it.key, { unit_price: parseFloat(e.target.value) || 0 })} /></td>
            <td><input type="number" step="0.01" className="right" value={it.discount || ''} onChange={e => setItem(it.key, { discount: parseFloat(e.target.value) || 0 })} placeholder="0" /></td>
            <td><select value={it.tax_rate} onChange={e => setItem(it.key, { tax_rate: parseFloat(e.target.value) })}>
              {!taxRates?.some(t => t.rate === it.tax_rate) && <option value={it.tax_rate}>{it.tax_rate}%</option>}
              {taxRates?.map(t => <option key={t.id} value={t.rate}>{t.name}</option>)}
              {!taxRates?.some(t => t.rate === 0) && <option value={0}>No tax</option>}
            </select></td>
            <td className="num bold">{money(totals.lines[idx], inv.currency!)}</td>
            <td className="actions"><button className="btn ghost sm" onClick={() => move(idx, -1)} title="Move up">↑</button><button className="btn ghost sm" onClick={() => move(idx, 1)} title="Move down">↓</button><button className="btn ghost sm" onClick={() => setItems(its => its.filter(x => x.key !== it.key))} title="Remove">✕</button></td>
          </tr>)}</tbody>
        </table></div>
        <div className="row between" style={{ padding: 14 }}>
          <button className="btn" onClick={() => setItems(its => [...its, newItem(unitForBilling(mode), settings.default_tax_rate, client?.default_rate || 0)])}>+ Add line</button>
          <div className="totals-box">
            <div className="line"><span className="muted">Subtotal</span><span>{money(totals.subtotal, inv.currency!)}</span></div>
            {totals.discount > 0 && <div className="line"><span className="muted">Discount</span><span>-{money(totals.discount, inv.currency!)}</span></div>}
            {Object.entries(totals.breakdown).map(([r, v]) => <div className="line" key={r}><span className="muted">Tax {r}%</span><span>{money(v, inv.currency!)}</span></div>)}
            <div className="line grand"><span>Total</span><span>{money(totals.total, inv.currency!)}</span></div>
            {inv.currency !== settings.base_currency && (inv.exchange_rate || 0) > 0 && <div className="line muted small"><span>≈ in {settings.base_currency}</span><span>{money(totals.total * (inv.exchange_rate || 0), settings.base_currency)}</span></div>}
          </div>
        </div>
      </Card>

      <div className="grid cols-3 mt">
        <Field label="Notes (shown on invoice)"><textarea value={inv.notes || ''} onChange={e => set('notes', e.target.value)} /></Field>
        <Field label="Terms"><textarea value={inv.terms || ''} onChange={e => set('terms', e.target.value)} /></Field>
        <Field label="Footer"><textarea value={inv.footer || ''} onChange={e => set('footer', e.target.value)} /></Field>
      </div>
      <div className="form-actions">
        <button className="btn" onClick={() => navigate(-1)}>Cancel</button>
        <button className="btn" disabled={busy} onClick={() => save()}>{id ? 'Save' : 'Save as draft'}</button>
        {(!id || inv.status === 'draft') && <button className="btn primary" disabled={busy} onClick={() => save('sent')}>Save & mark sent</button>}
      </div>
    </>
  )
}
