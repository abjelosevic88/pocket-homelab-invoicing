import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { BILLING_MODES, fmtDateTime, money, UNITS } from '../lib/format'
import type { APIToken, Currency, CustomFieldDef, ExchangeRate, InvoiceTemplate, Product, Settings as S, TaxRate, User, Webhook } from '../lib/types'
import { Card, Confirm, Empty, Field, Loading, Modal, Tabs, useAsync, useToast } from '../components/ui'

type Tab = 'company' | 'invoicing' | 'currencies' | 'taxes' | 'catalog' | 'templates' | 'email' | 'api' | 'webhooks' | 'users' | 'backup' | 'system'
const TABS: { id: Tab; label: string }[] = [
  { id: 'company', label: 'Company' }, { id: 'invoicing', label: 'Invoicing' }, { id: 'currencies', label: 'Currencies' }, { id: 'taxes', label: 'Taxes' }, { id: 'catalog', label: 'Catalog' },
  { id: 'templates', label: 'Templates' }, { id: 'email', label: 'Email' }, { id: 'api', label: 'API tokens' }, { id: 'webhooks', label: 'Webhooks' }, { id: 'users', label: 'Users' }, { id: 'backup', label: 'Backup' }, { id: 'system', label: 'System' },
]

export default function Settings() {
  const { '*': sub } = useParams()
  const navigate = useNavigate()
  const tab = (TABS.find(t => t.id === sub)?.id || 'company') as Tab
  return (
    <>
      <div className="page-header"><div><h1>Settings</h1></div></div>
      <Tabs tabs={TABS} value={tab} onChange={t => navigate(`/settings/${t}`)} />
      {tab === 'company' && <Company />}
      {tab === 'invoicing' && <Invoicing />}
      {tab === 'currencies' && <Currencies />}
      {tab === 'taxes' && <Taxes />}
      {tab === 'catalog' && <Catalog />}
      {tab === 'templates' && <Templates />}
      {tab === 'email' && <Email />}
      {tab === 'api' && <Tokens />}
      {tab === 'webhooks' && <Webhooks />}
      {tab === 'users' && <Users />}
      {tab === 'backup' && <Backup />}
      {tab === 'system' && <System />}
    </>
  )
}

// ---------- shared settings form hook ----------
function useSettingsForm() {
  const { settings, refresh } = useApp()
  const toast = useToast()
  const [s, setS] = useState<S | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setS(settings) }, [settings])
  const set = (k: keyof S) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => {
    const t = e.target as HTMLInputElement
    setS(x => x ? { ...x, [k]: t.type === 'checkbox' ? t.checked : t.type === 'number' ? parseFloat(t.value) || 0 : t.value } : x)
  }
  const save = async () => { if (!s) return; setBusy(true); try { await api.put(`${V1}/settings`, s); await refresh(); toast('Settings saved', 'success') } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) } }
  return { s, setS, set, save, busy }
}

function SaveBar({ onSave, busy }: { onSave: () => void; busy: boolean }) { return <div className="form-actions"><button className="btn primary" disabled={busy} onClick={onSave}>Save changes</button></div> }

// ---------- Company ----------
function Company() {
  const { s, set, save, busy, setS } = useSettingsForm()
  const toast = useToast()
  const { refresh, currencies } = useApp()
  if (!s) return <Loading />
  const upload = async (f: File) => { const fd = new FormData(); fd.append('logo', f); try { await api.post(`${V1}/settings/logo`, fd); await refresh(); toast('Logo uploaded', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="grid cols-2" style={{ gridTemplateColumns: '2fr 1fr' }}>
      <Card title="Business details">
        <div className="form-grid">
          <Field label="Company / your name (printed on invoices)" className="full"><input value={s.company_name} onChange={set('company_name')} /></Field>
          <Field label="Short name" help="Shown in the app sidebar and browser tab" className="full"><input value={s.brand_name || ''} onChange={set('brand_name')} placeholder={s.company_name} /></Field>
          <Field label="Email"><input value={s.company_email} onChange={set('company_email')} /></Field>
          <Field label="Phone"><input value={s.company_phone} onChange={set('company_phone')} /></Field>
          <Field label="Website"><input value={s.company_website} onChange={set('company_website')} /></Field>
          <Field label="Tax / VAT ID"><input value={s.tax_id} onChange={set('tax_id')} /></Field>
          <Field label="Address line 1" className="full"><input value={s.address1} onChange={set('address1')} /></Field>
          <Field label="Address line 2" className="full"><input value={s.address2} onChange={set('address2')} /></Field>
          <Field label="Postal code"><input value={s.postal_code} onChange={set('postal_code')} /></Field>
          <Field label="City"><input value={s.city} onChange={set('city')} /></Field>
          <Field label="State / region"><input value={s.state} onChange={set('state')} /></Field>
          <Field label="Country"><input value={s.country} onChange={set('country')} /></Field>
          <Field label="Payment details (printed on every invoice)" className="full" help="Bank name, IBAN/BIC, PayPal, crypto address…"><textarea rows={4} value={s.payment_details} onChange={set('payment_details')} /></Field>
          {currencies.filter(c => c.enabled).map(c => <Field key={c.code} label={`Payment details for ${c.code} invoices (optional override)`} className="full"><textarea rows={3} value={s.payment_details_by_currency?.[c.code] || ''} onChange={e => setS(x => x ? { ...x, payment_details_by_currency: { ...(x.payment_details_by_currency || {}), [c.code]: e.target.value } } : x)} placeholder={`Leave empty to use the default above`} /></Field>)}
        </div>
        <SaveBar onSave={save} busy={busy} />
      </Card>
      <div className="grid">
        <Card title="Logo">
          {s.logo_path ? <img className="logo-preview" src={`/uploads/${s.logo_path}?v=${Date.now()}`} alt="logo" /> : <div className="dropzone mb">No logo yet</div>}
          <div className="row"><input type="file" accept="image/png,image/jpeg,image/gif,image/svg+xml" onChange={e => e.target.files?.[0] && upload(e.target.files[0])} />{s.logo_path && <button className="btn sm danger" onClick={async () => { await api.del(`${V1}/settings/logo`); await refresh(); setS(x => x ? { ...x, logo_path: '' } : x) }}>Remove</button>}</div>
          <p className="muted small mt">PNG or JPG recommended for the native PDF engine (SVG works with Chromium/Gotenberg).</p>
        </Card>
        <Card title="Localisation">
          <div className="grid" style={{ gap: 12 }}>
            <Field label="Date format (Go layout)" help="2006-01-02 · 02.01.2006 · Jan 2, 2006 · 02/01/2006"><input value={s.date_format} onChange={set('date_format')} /></Field>
            <Field label="Timezone"><input value={s.timezone} onChange={set('timezone')} placeholder="Europe/Berlin" /></Field>
          </div>
          <SaveBar onSave={save} busy={busy} />
        </Card>
      </div>
    </div>
  )
}

// ---------- Invoicing ----------
function CustomFieldsEditor({ value, onChange }: { value: CustomFieldDef[]; onChange: (v: CustomFieldDef[]) => void }) {
  const upd = (i: number, p: Partial<CustomFieldDef>) => onChange(value.map((f, j) => j === i ? { ...f, ...p } : f))
  return (
    <div className="grid" style={{ gap: 8 }}>
      {value.map((f, i) => <div key={i} className="row">
        <input value={f.label} onChange={e => upd(i, { label: e.target.value })} placeholder="Label (e.g. PFR broj računa)" style={{ width: 260 }} />
        <input value={f.key} onChange={e => upd(i, { key: e.target.value })} placeholder="key (auto)" className="mono" style={{ width: 160 }} />
        <label className="check"><input type="checkbox" checked={f.show_on_pdf} onChange={e => upd(i, { show_on_pdf: e.target.checked })} /> show on PDF</label>
        <button className="btn ghost sm" onClick={() => onChange(value.filter((_, j) => j !== i))}>✕</button>
      </div>)}
      <div><button className="btn sm" onClick={() => onChange([...value, { key: '', label: '', show_on_pdf: true }])}>+ Add field</button></div>
    </div>
  )
}

function Invoicing() {
  const { s, set, save, busy, setS } = useSettingsForm()
  const { data: next } = useAsync(() => api.get<{ number: string }>(`${V1}/invoices/next-number`))
  if (!s) return <Loading />
  return (
    <div className="grid cols-2">
      <Card title="Numbering">
        <div className="grid" style={{ gap: 12 }}>
          <Field label="Invoice number format" help={<>Placeholders: {'{YYYY} {YY} {MM} {DD} {SEQ} {SEQ:4} {CLIENT}'} · next: <code>{next?.number}</code></>}><input value={s.invoice_number_format} onChange={set('invoice_number_format')} /></Field>
          <Field label="Next sequence number"><input type="number" value={s.invoice_next_seq} onChange={set('invoice_next_seq')} /></Field>
          <label className="check"><input type="checkbox" checked={s.invoice_seq_reset_yearly} onChange={set('invoice_seq_reset_yearly')} /> Reset sequence every year</label>
        </div>
      </Card>
      <Card title="Defaults for new invoices">
        <div className="form-grid">
          <Field label="Payment terms (days)"><input type="number" value={s.default_due_days} onChange={set('default_due_days')} /></Field>
          <Field label="Default billing mode"><select value={s.default_billing_mode} onChange={set('default_billing_mode')}>{BILLING_MODES.map(b => <option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
          <Field label="Default hourly rate"><input type="number" step="0.01" value={s.default_hourly_rate} onChange={set('default_hourly_rate')} /></Field>
          <Field label="Default daily rate"><input type="number" step="0.01" value={s.default_daily_rate} onChange={set('default_daily_rate')} /></Field>
          <Field label="Default monthly rate"><input type="number" step="0.01" value={s.default_monthly_rate} onChange={set('default_monthly_rate')} /></Field>
          <Field label="Default tax rate (%)"><input type="number" step="0.01" value={s.default_tax_rate} onChange={set('default_tax_rate')} /></Field>
          <Field label="Hours per day" help="Used when billing tracked hours as days"><input type="number" step="0.5" value={s.hours_per_day} onChange={set('hours_per_day')} /></Field>
          <Field label="Round timer to (minutes)" help="1 = no rounding"><input type="number" value={s.time_rounding_minutes} onChange={set('time_rounding_minutes')} /></Field>
          <Field label="Number format"><select value={s.number_format} onChange={set('number_format')}>{['1,234.56', '1.234,56', '1 234,56', "1'234.56", '1234.56'].map(f => <option key={f} value={f}>{f}</option>)}</select></Field>
          <Field label="Email attachment" help="What to attach when emailing an invoice"><select value={s.email_attachment_mode} onChange={set('email_attachment_mode')}><option value="generated">Generated PDF</option><option value="uploaded">Uploaded files only (falls back to generated if none)</option><option value="both">Generated PDF + uploaded files</option></select></Field>
          <label className="check full"><input type="checkbox" checked={s.show_tax_column} onChange={set('show_tax_column')} /> Show tax column on invoices</label>
        </div>
      </Card>
      <Card title="Base currency total on invoices">
        <div className="grid" style={{ gap: 12 }}>
          <label className="check"><input type="checkbox" checked={s.show_base_total} onChange={set('show_base_total')} /> When an invoice is in another currency, also print the total in {s.base_currency}</label>
          <Field label="Sentence printed under the totals" help="Placeholders: {rate} {currency} {currency_name} {base} {base_name} {total_base} {total}"><textarea rows={3} value={s.base_total_note} onChange={set('base_total_note')} /></Field>
        </div>
        <SaveBar onSave={save} busy={busy} />
      </Card>
      <Card title="Custom invoice fields" className="full">
        <p className="muted small">Extra fields per invoice (e.g. fiscal receipt number, local invoice number, project code). They appear in the invoice editor and, if enabled, in the PDF header block.</p>
        <CustomFieldsEditor value={s.custom_fields || []} onChange={v => setS(x => x ? { ...x, custom_fields: v } : x)} />
        <SaveBar onSave={save} busy={busy} />
      </Card>
      <Card title="Default text" className="full" >
        <div className="form-grid cols-3">
          <Field label="Notes"><textarea value={s.default_notes} onChange={set('default_notes')} /></Field>
          <Field label="Terms" help="{due_days} is replaced"><textarea value={s.default_terms} onChange={set('default_terms')} /></Field>
          <Field label="Footer"><textarea value={s.default_footer} onChange={set('default_footer')} /></Field>
        </div>
        <SaveBar onSave={save} busy={busy} />
      </Card>
    </div>
  )
}

// ---------- Currencies ----------
function Currencies() {
  const { settings, refresh } = useApp()
  const toast = useToast()
  const { s, set, save, busy } = useSettingsForm()
  const { data: list, reload } = useAsync(() => api.get<Currency[]>(`${V1}/currencies`))
  const { data: rates, reload: reloadRates } = useAsync(() => api.get<{ base: string; rates: ExchangeRate[]; provider: string }>(`${V1}/rates`))
  const [manual, setManual] = useState({ quote: '', rate: '' })
  const [filter, setFilter] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  if (!s || !list) return <Loading />
  const toggle = async (c: Currency) => { await api.put(`${V1}/currencies/${c.code}`, { ...c, enabled: !c.enabled }); reload(); refresh() }
  const refreshRates = async () => { setRefreshing(true); try { const r = await api.post<{ updated: number }>(`${V1}/rates/refresh`); toast(`${r.updated} rates updated from ${rates?.provider}`, 'success'); reloadRates() } catch (e) { toast((e as Error).message, 'error') } finally { setRefreshing(false) } }
  const addManual = async () => { try { await api.put(`${V1}/rates`, { quote: manual.quote.toUpperCase(), rate: parseFloat(manual.rate) }); setManual({ quote: '', rate: '' }); reloadRates(); toast('Rate saved', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  const base = settings?.base_currency || ''
  return (
    <div className="grid cols-2">
      <div className="grid">
        <Card title="Base currency">
          <Field label="Base currency" help="Reports and the dashboard are shown in this currency. Changing it does not alter existing invoices."><select value={s.base_currency} onChange={set('base_currency')}>{list.map(c => <option key={c.code} value={c.code}>{c.code} – {c.name}</option>)}</select></Field>
          <SaveBar onSave={save} busy={busy} />
        </Card>
        <Card title={`Exchange rates (1 ${base} = …)`} actions={<button className="btn sm" disabled={refreshing || rates?.provider === 'none'} onClick={refreshRates}>{refreshing ? 'Refreshing…' : `Refresh from ${rates?.provider || 'provider'}`}</button>} flush>
          {rates?.provider === 'none' && <div className="callout warn" style={{ margin: 12 }}>Automatic rates are disabled (EXCHANGE_RATE_PROVIDER=none). Enter rates manually below.</div>}
          <table className="table"><thead><tr><th>Quote</th><th className="num">Rate</th><th>Source</th><th>Updated</th><th></th></tr></thead>
            <tbody>{rates?.rates.map(r => <tr key={r.quote}><td className="bold">{r.quote}</td><td className="num">{r.rate.toFixed(6)}</td><td className="muted">{r.source}</td><td className="muted small">{fmtDateTime(r.fetched_at)}</td><td className="actions"><button className="btn ghost sm" onClick={async () => { await api.del(`${V1}/rates/${r.quote}`); reloadRates() }}>✕</button></td></tr>)}
              <tr><td><input value={manual.quote} onChange={e => setManual({ ...manual, quote: e.target.value })} placeholder="USD" style={{ width: 80 }} /></td><td><input value={manual.rate} onChange={e => setManual({ ...manual, rate: e.target.value })} placeholder="1.08" style={{ width: 110 }} /></td><td colSpan={2} className="muted small">Manual rates are never overwritten by refresh.</td><td className="actions"><button className="btn sm" onClick={addManual} disabled={!manual.quote || !manual.rate}>Add</button></td></tr>
            </tbody></table>
        </Card>
      </div>
      <Card title="Enabled currencies" actions={<input placeholder="Filter…" value={filter} onChange={e => setFilter(e.target.value)} style={{ width: 140 }} />} flush>
        <div style={{ maxHeight: 600, overflow: 'auto' }}><table className="table"><tbody>{list.filter(c => !filter || c.code.includes(filter.toUpperCase()) || c.name.toLowerCase().includes(filter.toLowerCase())).map(c => <tr key={c.code}><td><label className="check"><input type="checkbox" checked={c.enabled} disabled={c.code === base} onChange={() => toggle(c)} /> <strong>{c.code}</strong> <span className="muted">{c.name}</span></label></td><td className="num muted">{money(1234.5, c.code)}</td></tr>)}</tbody></table></div>
      </Card>
    </div>
  )
}

// ---------- Taxes ----------
function Taxes() {
  const toast = useToast()
  const { data, reload } = useAsync(() => api.get<TaxRate[]>(`${V1}/tax-rates`))
  const [form, setForm] = useState({ name: '', rate: '', is_default: false })
  const add = async () => { try { await api.post(`${V1}/tax-rates`, { name: form.name, rate: parseFloat(form.rate) || 0, is_default: form.is_default }); setForm({ name: '', rate: '', is_default: false }); reload(); toast('Tax rate added', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <Card title="Tax rates" flush>
      <table className="table"><thead><tr><th>Name</th><th className="num">Rate</th><th>Default</th><th></th></tr></thead>
        <tbody>{data?.map(t => <tr key={t.id}><td>{t.name}</td><td className="num">{t.rate}%</td><td>{t.is_default ? <span className="badge ok">default</span> : <button className="link-btn small" onClick={async () => { await api.put(`${V1}/tax-rates/${t.id}`, { ...t, is_default: true }); reload() }}>make default</button>}</td><td className="actions"><button className="btn ghost sm" onClick={async () => { await api.del(`${V1}/tax-rates/${t.id}`); reload() }}>✕</button></td></tr>)}
          <tr><td><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="VAT 20%" /></td><td><input value={form.rate} onChange={e => setForm({ ...form, rate: e.target.value })} placeholder="20" style={{ width: 90 }} /></td><td><label className="check"><input type="checkbox" checked={form.is_default} onChange={e => setForm({ ...form, is_default: e.target.checked })} /> default</label></td><td className="actions"><button className="btn sm primary" onClick={add} disabled={!form.rate}>Add</button></td></tr>
        </tbody></table>
      {!data?.length && <div className="callout" style={{ margin: 12 }}>Tip: add a 0% rate named "Reverse charge" for cross-border B2B invoices.</div>}
    </Card>
  )
}

// ---------- Catalog ----------
function Catalog() {
  const toast = useToast()
  const { settings, currencies } = useApp()
  const { data, reload } = useAsync(() => api.get<Product[]>(`${V1}/products`))
  const [editing, setEditing] = useState<Partial<Product> | null>(null)
  const save = async () => { if (!editing) return; try { if (editing.id) await api.put(`${V1}/products/${editing.id}`, editing); else await api.post(`${V1}/products`, editing); setEditing(null); reload(); toast('Saved', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <>
      <Card title="Products & services" actions={<button className="btn sm primary" onClick={() => setEditing({ name: '', description: '', unit: 'hour', unit_price: 0, currency: '', tax_rate: settings?.default_tax_rate || 0 })}>+ Add</button>} flush>
        {!data?.length ? <Empty title="Catalog is empty">Add your standard rates (hourly, daily, retainer) to insert them into invoices with one click.</Empty> : <table className="table"><thead><tr><th>Name</th><th>Unit</th><th className="num">Price</th><th className="num">Tax</th><th></th></tr></thead>
          <tbody>{data.map(p => <tr key={p.id}><td><div className="bold">{p.name}</div><div className="muted small">{p.description}</div></td><td className="muted">{p.unit}</td><td className="num">{money(p.unit_price, p.currency || settings?.base_currency || '')}</td><td className="num">{p.tax_rate}%</td><td className="actions"><button className="btn ghost sm" onClick={() => setEditing(p)}>Edit</button><button className="btn ghost sm" onClick={async () => { await api.del(`${V1}/products/${p.id}`); reload() }}>✕</button></td></tr>)}</tbody></table>}
      </Card>
      {editing && <Modal title={editing.id ? 'Edit item' : 'New catalog item'} onClose={() => setEditing(null)}>
        <div className="form-grid">
          <Field label="Name" className="full"><input value={editing.name} onChange={e => setEditing({ ...editing, name: e.target.value })} autoFocus /></Field>
          <Field label="Description" className="full"><input value={editing.description} onChange={e => setEditing({ ...editing, description: e.target.value })} /></Field>
          <Field label="Unit"><select value={editing.unit} onChange={e => setEditing({ ...editing, unit: e.target.value })}>{UNITS.map(u => <option key={u.value} value={u.value}>{u.label}</option>)}</select></Field>
          <Field label="Unit price"><input type="number" step="0.01" value={editing.unit_price} onChange={e => setEditing({ ...editing, unit_price: parseFloat(e.target.value) || 0 })} /></Field>
          <Field label="Currency" help="Blank = any"><select value={editing.currency} onChange={e => setEditing({ ...editing, currency: e.target.value })}><option value="">Any</option>{currencies.filter(c => c.enabled).map(c => <option key={c.code} value={c.code}>{c.code}</option>)}</select></Field>
          <Field label="Tax rate (%)"><input type="number" step="0.01" value={editing.tax_rate} onChange={e => setEditing({ ...editing, tax_rate: parseFloat(e.target.value) || 0 })} /></Field>
        </div>
        <div className="form-actions"><button className="btn primary" onClick={save} disabled={!editing.name}>Save</button></div>
      </Modal>}
    </>
  )
}

// ---------- Templates ----------
const LABEL_KEYS = ['invoice', 'invoice_number', 'issue_date', 'due_date', 'period', 'po_number', 'bill_to', 'description', 'unit', 'quantity', 'unit_price', 'discount', 'tax', 'amount', 'subtotal', 'total', 'amount_paid', 'balance_due', 'notes', 'terms', 'payment_details', 'tax_id', 'page', 'paid_stamp', 'hour', 'hours', 'day', 'days', 'month', 'months']

function Templates() {
  const toast = useToast()
  const { data, reload } = useAsync(() => api.get<InvoiceTemplate[]>(`${V1}/templates`))
  const [sel, setSel] = useState<InvoiceTemplate | null>(null)
  const [tab, setTab] = useState<'design' | 'labels' | 'html'>('design')
  const [previewKey, setPreviewKey] = useState(0)
  const [del, setDel] = useState<InvoiceTemplate | null>(null)
  useEffect(() => { if (data && !sel) setSel(data[0] || null) }, [data, sel])
  const save = async () => { if (!sel) return; try { const saved = sel.id ? await api.put<InvoiceTemplate>(`${V1}/templates/${sel.id}`, sel) : await api.post<InvoiceTemplate>(`${V1}/templates`, sel); setSel(saved); reload(); setPreviewKey(k => k + 1); toast('Template saved', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  const loadDefaultHTML = async () => { const html = await api.getText(`${V1}/templates/default-html`); setSel(s => s ? { ...s, html } : s) }
  if (!data) return <Loading />
  const previewUrl = sel?.id ? `${V1}/templates/${sel.id}/preview?layout=${sel.layout}&accent=${encodeURIComponent(sel.accent_color)}&k=${previewKey}` : ''
  return (
    <div className="grid" style={{ gridTemplateColumns: '220px 1fr 1fr' }}>
      <Card title="Templates" actions={<button className="btn sm" onClick={() => setSel({ id: 0, name: 'New template', layout: 'classic', accent_color: '#2563eb', labels: {}, options: { hide_rate: false, hide_unit: false, show_quantity_total: false, signature_label: '', hide_logo: false }, html: '', is_default: false })}>+</button>} flush>
        <table className="table"><tbody>{data.map(t => <tr key={t.id} className="clickable" onClick={() => { setSel(t); setPreviewKey(k => k + 1) }} style={sel?.id === t.id ? { background: 'var(--accent-soft)' } : {}}><td><span className="bold">{t.name}</span>{t.is_default && <span className="badge ok" style={{ marginLeft: 6 }}>default</span>}<div className="muted small">{t.layout}{t.html ? ' · custom HTML' : ''}</div></td></tr>)}</tbody></table>
      </Card>
      {sel && <Card title={sel.id ? `Edit "${sel.name}"` : 'New template'} actions={<div className="row">{sel.id > 0 && !sel.is_default && <button className="btn sm danger" onClick={() => setDel(sel)}>Delete</button>}<button className="btn sm primary" onClick={save}>Save</button></div>}>
        <Tabs tabs={[{ id: 'design', label: 'Design' }, { id: 'labels', label: 'Labels / language' }, { id: 'html', label: 'HTML (advanced)' }]} value={tab} onChange={setTab} />
        {tab === 'design' && <div className="grid" style={{ gap: 12 }}>
          <Field label="Name"><input value={sel.name} onChange={e => setSel({ ...sel, name: e.target.value })} /></Field>
          <Field label="Layout"><select value={sel.layout} onChange={e => setSel({ ...sel, layout: e.target.value })}><option value="classic">Classic — header left, accent table</option><option value="modern">Modern — full-width coloured header</option><option value="minimal">Minimal — black & white, thin rules</option></select></Field>
          <Field label="Accent colour"><div className="row"><input type="color" value={sel.accent_color} onChange={e => setSel({ ...sel, accent_color: e.target.value })} /><input value={sel.accent_color} onChange={e => setSel({ ...sel, accent_color: e.target.value })} style={{ width: 120 }} /></div></Field>
          <label className="check"><input type="checkbox" checked={sel.is_default} onChange={e => setSel({ ...sel, is_default: e.target.checked })} /> Use as default template</label>
          <hr />
          <label className="check"><input type="checkbox" checked={!!sel.options?.hide_rate} onChange={e => setSel({ ...sel, options: { ...sel.options, hide_rate: e.target.checked } })} /> Hide the rate (unit price) column</label>
          <label className="check"><input type="checkbox" checked={!!sel.options?.hide_unit} onChange={e => setSel({ ...sel, options: { ...sel.options, hide_unit: e.target.checked } })} /> Hide the unit column (quantity shows as "21 days")</label>
          <label className="check"><input type="checkbox" checked={!!sel.options?.show_quantity_total} onChange={e => setSel({ ...sel, options: { ...sel.options, show_quantity_total: e.target.checked } })} /> Show summed quantity in totals (e.g. "100 hours")</label>
          <label className="check"><input type="checkbox" checked={!!sel.options?.hide_logo} onChange={e => setSel({ ...sel, options: { ...sel.options, hide_logo: e.target.checked } })} /> Hide logo</label>
          <Field label="Signature line label" help="Leave empty for none, e.g. 'Odgovorno lice' or 'Authorised signature'"><input value={sel.options?.signature_label || ''} onChange={e => setSel({ ...sel, options: { ...sel.options, signature_label: e.target.value } })} /></Field>
          <p className="muted small">Design settings apply to all PDF engines. Save to refresh the preview.</p>
        </div>}
        {tab === 'labels' && <div>
          <p className="muted small">Override any label to localise invoices (e.g. "INVOICE" → "RECHNUNG" / "FAKTURA"). Blank = English default.</p>
          <div className="form-grid cols-2" style={{ gap: 8 }}>{LABEL_KEYS.map(k => <Field key={k} label={k}><input value={sel.labels[k] || ''} onChange={e => setSel({ ...sel, labels: { ...sel.labels, [k]: e.target.value } })} /></Field>)}</div>
        </div>}
        {tab === 'html' && <div>
          <p className="muted small">Custom HTML (Go <code>html/template</code> syntax) is used by the <strong>chromium</strong> and <strong>gotenberg</strong> PDF engines and for the public web view. The native engine ignores it. <button className="link-btn" onClick={loadDefaultHTML}>Load built-in template</button> to start from.</p>
          <textarea className="mono" rows={24} value={sel.html} onChange={e => setSel({ ...sel, html: e.target.value })} placeholder="Leave empty to use the built-in HTML template" />
        </div>}
      </Card>}
      {sel && sel.id > 0 && <Card title="Preview (sample data)" actions={<a className="btn sm" href={`${V1}/templates/${sel.id}/preview.pdf?layout=${sel.layout}&accent=${encodeURIComponent(sel.accent_color)}`} target="_blank" rel="noreferrer">Open PDF</a>} flush><div style={{ height: 760, overflow: 'hidden' }}><iframe key={previewKey} src={previewUrl} title="preview" style={{ width: '200%', height: 1520, border: 0, transform: 'scale(0.5)', transformOrigin: '0 0', background: '#fff' }} /></div></Card>}
      {del && <Confirm title="Delete template?" message="Invoices using it fall back to the default template." onConfirm={async () => { await api.del(`${V1}/templates/${del.id}`); setDel(null); setSel(null); reload() }} onCancel={() => setDel(null)} />}
    </div>
  )
}

// ---------- Email ----------
function Email() {
  const { s, set, save, busy } = useSettingsForm()
  const toast = useToast()
  const [to, setTo] = useState('')
  if (!s) return <Loading />
  const test = async () => { try { await save(); await api.post(`${V1}/settings/test-email`, { to }); toast('Test email sent', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="grid cols-2">
      <Card title="SMTP server">
        <div className="form-grid">
          <Field label="Host" className="full"><input value={s.smtp_host} onChange={set('smtp_host')} placeholder="smtp.example.com" /></Field>
          <Field label="Port"><input type="number" value={s.smtp_port} onChange={set('smtp_port')} /></Field>
          <Field label="Encryption"><select value={s.smtp_tls} onChange={set('smtp_tls')}><option value="starttls">STARTTLS (587)</option><option value="tls">TLS/SSL (465)</option><option value="none">None (25)</option></select></Field>
          <Field label="Username"><input value={s.smtp_user} onChange={set('smtp_user')} /></Field>
          <Field label="Password" help="Leave blank to keep the current one"><input type="password" value={s.smtp_password} onChange={set('smtp_password')} autoComplete="new-password" /></Field>
          <Field label="From address"><input value={s.smtp_from} onChange={set('smtp_from')} placeholder="billing@example.com" /></Field>
          <Field label="From name"><input value={s.smtp_from_name} onChange={set('smtp_from_name')} /></Field>
          <Field label="BCC (copy of every invoice)" className="full"><input value={s.smtp_bcc} onChange={set('smtp_bcc')} /></Field>
        </div>
        <div className="row between mt"><div className="row"><input placeholder="Send test to…" value={to} onChange={e => setTo(e.target.value)} style={{ width: 220 }} /><button className="btn" onClick={test} disabled={!s.smtp_host}>Save & send test</button></div><button className="btn primary" disabled={busy} onClick={save}>Save</button></div>
      </Card>
      <Card title="Invoice email & reminders">
        <div className="grid" style={{ gap: 12 }}>
          <Field label="Subject"><input value={s.email_subject} onChange={set('email_subject')} /></Field>
          <Field label="Body" help="Placeholders: {number} {client} {company} {total} {balance} {due_date} {issue_date} {link}"><textarea rows={8} value={s.email_body} onChange={set('email_body')} /></Field>
          <label className="check"><input type="checkbox" checked={s.reminders_enabled} onChange={set('reminders_enabled')} /> Send automatic payment reminders for overdue invoices</label>
          <Field label="Remind on days after due date" help="Comma separated"><input value={s.reminder_days} onChange={set('reminder_days')} placeholder="3,7,14" /></Field>
        </div>
        <SaveBar onSave={save} busy={busy} />
      </Card>
    </div>
  )
}

// ---------- API tokens ----------
function Tokens() {
  const toast = useToast()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`))
  const [name, setName] = useState('')
  const [created, setCreated] = useState('')
  const create = async () => { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name }); setCreated(r.token); setName(''); reload() }
  return (
    <div className="grid cols-2">
      <Card title="Personal access tokens" flush>
        <table className="table"><thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th></th></tr></thead>
          <tbody>{data?.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="actions"><button className="btn ghost sm" onClick={async () => { await api.del(`${V1}/tokens/${t.id}`); reload() }}>Revoke</button></td></tr>)}
            <tr><td colSpan={3}><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. n8n, Home Assistant)" /></td><td className="actions"><button className="btn sm primary" onClick={create}>Create</button></td></tr></tbody></table>
        {created && <div className="callout" style={{ margin: 12 }}>Copy it now — it won't be shown again:<br /><code style={{ userSelect: 'all' }}>{created}</code> <button className="btn sm" onClick={() => { navigator.clipboard.writeText(created); toast('Copied', 'success') }}>Copy</button></div>}
      </Card>
      <Card title="Using the API">
        <pre>{`curl -H "Authorization: Bearer pi_…" \\
  ${window.location.origin}/api/v1/invoices

# create a client
curl -X POST -H "Authorization: Bearer pi_…" \\
  -H "Content-Type: application/json" \\
  -d '{"name":"Acme","currency":"USD","billing_mode":"hourly"}' \\
  ${window.location.origin}/api/v1/clients

# start the timer
curl -X POST -H "Authorization: Bearer pi_…" \\
  -H "Content-Type: application/json" \\
  -d '{"client_id":1,"description":"Patching"}' \\
  ${window.location.origin}/api/v1/time/start`}</pre>
        <p className="muted small">Full reference: <code>docs/api.md</code> in the repository. Metrics for Prometheus are at <code>/metrics</code>.</p>
      </Card>
    </div>
  )
}

// ---------- Webhooks ----------
function Webhooks() {
  const toast = useToast()
  const { data, reload } = useAsync(() => api.get<Webhook[]>(`${V1}/webhooks`))
  const [form, setForm] = useState({ url: '', events: '*', secret: '' })
  const add = async () => { try { await api.post(`${V1}/webhooks`, form); setForm({ url: '', events: '*', secret: '' }); reload(); toast('Webhook added', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="grid cols-2">
      <Card title="Outgoing webhooks" flush>
        <table className="table"><thead><tr><th>URL</th><th>Events</th><th>Enabled</th><th></th></tr></thead>
          <tbody>{data?.map(w => <tr key={w.id}><td className="mono small">{w.url}</td><td className="muted">{w.events}</td><td><input type="checkbox" checked={w.enabled} onChange={async e => { await api.put(`${V1}/webhooks/${w.id}`, { ...w, enabled: e.target.checked }); reload() }} /></td><td className="actions"><button className="btn ghost sm" onClick={async () => { await api.del(`${V1}/webhooks/${w.id}`); reload() }}>✕</button></td></tr>)}
            <tr><td><input value={form.url} onChange={e => setForm({ ...form, url: e.target.value })} placeholder="https://n8n.local/webhook/…" /></td><td><input value={form.events} onChange={e => setForm({ ...form, events: e.target.value })} style={{ width: 150 }} /></td><td><input value={form.secret} onChange={e => setForm({ ...form, secret: e.target.value })} placeholder="secret" style={{ width: 100 }} /></td><td className="actions"><button className="btn sm primary" onClick={add} disabled={!form.url}>Add</button></td></tr></tbody></table>
        <div style={{ padding: 12 }}><button className="btn sm" onClick={async () => { await api.post(`${V1}/webhooks/test`); toast('Test event sent to all enabled webhooks') }}>Send test event</button></div>
      </Card>
      <Card title="Events">
        <p className="small">Subscribe with <code>*</code>, a comma list, or prefixes like <code>invoice.*</code>. Payloads are JSON <code>{'{event, timestamp, data}'}</code> and signed with <code>X-Webhook-Signature: sha256=HMAC(secret, body)</code>.</p>
        <div className="inline-list">{['invoice.created', 'invoice.updated', 'invoice.sent', 'invoice.viewed', 'invoice.paid', 'invoice.partial', 'invoice.overdue', 'invoice.cancelled', 'invoice.deleted', 'payment.created', 'client.created', 'client.updated', 'test'].map(e => <span key={e} className="badge">{e}</span>)}</div>
        <p className="muted small mt">Great for n8n, Home Assistant, ntfy, Gotify, or a Discord/Slack relay.</p>
      </Card>
    </div>
  )
}

// ---------- Users ----------
function Users() {
  const toast = useToast()
  const { user, status, refresh } = useApp()
  const { data, reload } = useAsync(() => api.get<User[]>(`${V1}/users`))
  const [form, setForm] = useState({ email: '', name: '', password: '' })
  const [me, setMe] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const add = async () => { try { await api.post(`${V1}/users`, form); setForm({ email: '', name: '', password: '' }); reload(); toast('User created', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  const saveMe = async () => { try { await api.put(`${V1}/auth/me`, me); await refresh(); setMe({ ...me, current_password: '', new_password: '' }); toast('Profile updated', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="grid cols-2">
      <Card title="Your profile">
        {status?.auth_mode !== 'local' && <div className="callout mb">Authentication is handled by <strong>{status?.auth_mode}</strong> mode (reverse proxy / disabled). Passwords are not used.</div>}
        <div className="grid" style={{ gap: 12 }}>
          <Field label="Name"><input value={me.name} onChange={e => setMe({ ...me, name: e.target.value })} /></Field>
          <Field label="Email"><input value={me.email} onChange={e => setMe({ ...me, email: e.target.value })} /></Field>
          {status?.auth_mode === 'local' && <><Field label="Current password"><input type="password" value={me.current_password} onChange={e => setMe({ ...me, current_password: e.target.value })} autoComplete="current-password" /></Field>
            <Field label="New password"><input type="password" value={me.new_password} onChange={e => setMe({ ...me, new_password: e.target.value })} autoComplete="new-password" /></Field></>}
        </div>
        <SaveBar onSave={saveMe} busy={false} />
      </Card>
      <Card title="All users" flush>
        <table className="table"><thead><tr><th>Name</th><th>Email</th><th>Role</th><th></th></tr></thead>
          <tbody>{data?.map(u => <tr key={u.id}><td>{u.name}</td><td>{u.email}</td><td className="muted">{u.role}</td><td className="actions">{u.id !== user?.id && <button className="btn ghost sm" onClick={async () => { if (confirm('Delete user?')) { await api.del(`${V1}/users/${u.id}`); reload() } }}>✕</button>}</td></tr>)}
            <tr><td><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="Name" /></td><td><input value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} placeholder="email" /></td><td><input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} placeholder="password" /></td><td className="actions"><button className="btn sm primary" onClick={add} disabled={!form.email || form.password.length < 8}>Add</button></td></tr></tbody></table>
      </Card>
    </div>
  )
}

// ---------- Backup ----------
function Backup() {
  return (
    <div className="grid cols-2">
      <Card title="Download backup">
        <p>Everything lives in one SQLite file plus the <code>uploads/</code> folder (logo). Download a consistent snapshot any time:</p>
        <div className="row mb"><a className="btn primary" href={`${V1}/backup`}>Download database (.db)</a><a className="btn" href={`${V1}/export.json`}>Export JSON</a></div>
        <p className="muted small">The JSON export is human-readable and handy for migrations or your own scripts. The .db file is a byte-exact copy made with <code>VACUUM INTO</code>.</p>
      </Card>
      <Card title="Restore / automate">
        <pre>{`# restore: stop the container, replace the file, start again
docker compose stop
cp backup.db ./data/pocket-invoicing.db
docker compose start

# scripted backups from the host (cron)
docker compose exec app pocket-invoicing backup -o /data/backups/nightly.db

# or via API
curl -H "Authorization: Bearer pi_…" -o backup.db \\
  ${window.location.origin}/api/v1/backup`}</pre>
      </Card>
    </div>
  )
}

// ---------- System ----------
function System() {
  const { data } = useAsync(() => api.get<Record<string, string | number | boolean>>(`${V1}/settings/system`))
  const toast = useToast()
  if (!data) return <Loading />
  return (
    <div className="grid cols-2">
      <Card title="Runtime">
        <dl className="kv">{Object.entries(data).map(([k, v]) => <div key={k} style={{ display: 'contents' }}><dt>{k}</dt><dd className="mono">{String(v)}</dd></div>)}</dl>
      </Card>
      <Card title="Maintenance">
        <p className="small">The scheduler generates recurring invoices, marks overdue invoices, sends reminders and refreshes exchange rates. It runs automatically every <code>SCHEDULER_INTERVAL</code>; you can trigger it now:</p>
        <button className="btn" onClick={async () => { const r = await api.post<Record<string, number>>(`${V1}/scheduler/run`); toast(`Scheduler: ${Object.entries(r).map(([k, v]) => `${k}=${v}`).join(', ')}`, 'success') }}>Run scheduler now</button>
        <p className="muted small mt">Health: <code>/healthz</code> · readiness: <code>/readyz</code> · metrics: <code>/metrics</code></p>
      </Card>
    </div>
  )
}
