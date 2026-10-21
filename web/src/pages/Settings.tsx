import React, { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { BILLING_MODES, PAYMENT_METHODS, UNITS, methodLabel, fmtDate, fmtDateTime, money, today } from '../lib/format'
import type { APIToken, ConnMode, Currency, CustomFieldDef, ExchangeRate, Holiday, InvoiceTemplate, Product, Settings as S, TaxRate, User, Webhook, WorkingDays } from '../lib/types'
import { Card, Confirm, Empty, Field, Loading, Modal, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'

type Tab = 'company' | 'invoicing' | 'currencies' | 'taxes' | 'catalog' | 'templates' | 'email' | 'documents' | 'api' | 'webhooks' | 'users' | 'backup' | 'system'
const TABS: { id: Tab; label: string; icon: string; sub: string; wide?: boolean }[] = [
  { id: 'company', label: 'Company', icon: 'building', sub: 'Your business as it is printed on invoices, the logo, and the date format and timezone.' },
  { id: 'invoicing', label: 'Invoicing', icon: 'invoice', sub: 'Numbering, custom fields, defaults for new invoices, the text printed on them and your working days.' },
  { id: 'currencies', label: 'Currencies', icon: 'banknote', sub: 'The base currency, where exchange rates come from, and the currencies you invoice in.' },
  { id: 'taxes', label: 'Taxes', icon: 'percent', sub: 'Tax rates for invoice lines, and an estimate of your own income tax for Reports.' },
  { id: 'catalog', label: 'Catalog', icon: 'package', sub: 'Your standard products and rates, added to an invoice with one click.' },
  { id: 'templates', label: 'Templates', icon: 'palette', sub: 'How invoice PDFs look: design templates, Word templates and the labels printed on them.', wide: true },
  { id: 'email', label: 'Email', icon: 'mail', sub: 'The SMTP server invoices are sent through, the invoice email and payment reminders.' },
  { id: 'documents', label: 'Pocket Documents', icon: 'file', sub: 'File invoices and company documents in Pocket Documents, through Pocket Core or directly.' },
  { id: 'api', label: 'API tokens', icon: 'key', sub: 'Personal tokens for scripts, n8n, Home Assistant and the phone app.' },
  { id: 'webhooks', label: 'Webhooks', icon: 'zap', sub: 'Send invoice, payment and client events to other services as they happen.' },
  { id: 'users', label: 'Users', icon: 'users', sub: 'Your profile, and everyone who has an account on this server.' },
  { id: 'backup', label: 'Backup', icon: 'database', sub: 'Download the database, or export your data as JSON.' },
  { id: 'system', label: 'System', icon: 'server', sub: 'What the server runs with, and the scheduler.' },
]
// old names of sections: the Paperless tab became Pocket Documents
const ALIAS: Record<string, Tab> = { paperless: 'documents', profile: 'users', people: 'users', tokens: 'api', general: 'company' }

export default function Settings() {
  const { '*': rest } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const { user } = useApp()
  const isAdmin = user?.role === 'admin'
  // Everything here is the signed-in person's own, except System (admins only) and the
  // user list; members see just their profile under that tab.
  const tabs = TABS.filter(t => isAdmin || t.id !== 'system').map(t => (!isAdmin && t.id === 'users' ? { ...t, label: 'Profile', icon: 'user', sub: 'Your name, email and password.' } : t))
  const seg = (rest || '').split('/')[0]
  const raw = seg || new URLSearchParams(window.location.search).get('tab') || ''
  const tab = (tabs.find(t => t.id === (ALIAS[raw] || raw))?.id || 'company') as Tab
  const current = tabs.find(t => t.id === tab)!
  const go = (id: Tab) => navigate(`/settings/${id}`, { replace: true })
  // ?tab= links and old section names land on /settings/<section>
  useEffect(() => { if (raw && seg !== tab) navigate(`/settings/${tab}`, { replace: true }) }, [raw, seg, tab, navigate])
  const back = () => ((window.history.state as { idx?: number } | null)?.idx ? navigate(-1) : navigate('/'))
  const backRef = useRef(back)
  backRef.current = back

  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return
      const t = e.target as HTMLElement
      if (t.closest?.('input, textarea, [contenteditable="true"]') || document.querySelector('.modal-backdrop, .menu, .sel-pop')) return
      backRef.current()
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={back}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={tabs.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {tabs.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className={`set-inner ${current.wide ? 'wide' : ''}`} key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'company' && <Company />}
            {tab === 'invoicing' && <Invoicing />}
            {tab === 'currencies' && <Currencies />}
            {tab === 'taxes' && <Taxes />}
            {tab === 'catalog' && <Catalog />}
            {tab === 'templates' && <Templates />}
            {tab === 'email' && <Email />}
            {tab === 'documents' && <PocketDocuments />}
            {tab === 'api' && <Tokens />}
            {tab === 'webhooks' && <Webhooks />}
            {tab === 'users' && <Users />}
            {tab === 'backup' && <Backup />}
            {tab === 'system' && isAdmin && <System />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, flush, className = '', children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; flush?: boolean; className?: string; children?: React.ReactNode }) {
  return (
    <section className={`set-section ${className}`}>
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className={`set-section-body ${flush ? 'flush' : ''}`}>{children}</div>}
    </section>
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
  const put = (k: keyof S) => (v: S[keyof S]) => setS(x => x ? { ...x, [k]: v } : x)
  const save = async () => { if (!s) return; setBusy(true); try { await api.put(`${V1}/settings`, s); await refresh(); toast('Settings saved', 'success') } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) } }
  return { s, setS, set, put, save, busy }
}

function SaveBar({ onSave, busy }: { onSave: () => void; busy: boolean }) { return <div className="form-actions"><button type="button" className="btn primary" disabled={busy} onClick={onSave}>Save changes</button></div> }

// ---------- Company ----------
function Company() {
  const { s, set, save, busy, setS } = useSettingsForm()
  const toast = useToast()
  const { refresh, currencies } = useApp()
  if (!s) return <Loading />
  const upload = async (f: File) => { const fd = new FormData(); fd.append('logo', f); try { await api.post(`${V1}/settings/logo`, fd); await refresh(); toast('Logo uploaded', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="stack loose">
      <Section title="Business details">
        <div className="form-grid">
          <Field label="Company / your name (printed on invoices)" className="full"><input value={s.company_name} onChange={set('company_name')} /></Field>
          <Field label="Short name" help="Shown in the app sidebar and browser tab" className="full"><input value={s.brand_name || ''} onChange={set('brand_name')} placeholder={s.company_name} /></Field>
          <Field label="Email"><input value={s.company_email} onChange={set('company_email')} /></Field>
          <Field label="Phone"><input value={s.company_phone} onChange={set('company_phone')} /></Field>
          <Field label="Website"><input value={s.company_website} onChange={set('company_website')} /></Field>
          <Field label="Tax ID" help="e.g. JIB; printed as the template's tax_id label"><input value={s.tax_id} onChange={set('tax_id')} /></Field>
          <Field label="VAT ID" help="e.g. PDV broj; printed under the tax ID (label vat_id)"><input value={s.vat_id || ''} onChange={set('vat_id')} /></Field>
          <Field label="Place of issue" help="Printed next to the invoice date, e.g. Banja Luka"><input value={s.issue_place || ''} onChange={set('issue_place')} /></Field>
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
      </Section>
      <div className="stack loose">
        <Section title="Logo">
          {s.logo_path ? <img className="logo-preview" src={`/uploads/${s.logo_path}?v=${Date.now()}`} alt="logo" /> : <div className="dropzone mb">No logo yet</div>}
          <div className="row"><input type="file" accept="image/png,image/jpeg,image/gif,image/svg+xml" onChange={e => e.target.files?.[0] && upload(e.target.files[0])} />{s.logo_path && <button type="button" className="btn sm danger" onClick={async () => { await api.del(`${V1}/settings/logo`); await refresh(); setS(x => x ? { ...x, logo_path: '' } : x) }}>Remove</button>}</div>
          <p className="muted small mt">PNG or JPG recommended for the native PDF engine (SVG works with Chromium/Gotenberg).</p>
        </Section>
        <Section title="Localisation">
          <div className="grid" style={{ gap: 12 }}>
            <Field label="Date format (Go layout)" help="2006-01-02 · 02.01.2006 · Jan 2, 2006 · 02/01/2006"><input value={s.date_format} onChange={set('date_format')} /></Field>
            <Field label="Timezone"><input value={s.timezone} onChange={set('timezone')} placeholder="Europe/Berlin" /></Field>
          </div>
          <SaveBar onSave={save} busy={busy} />
        </Section>
      </div>
    </div>
  )
}

// ---------- Invoicing ----------
function CustomFieldsEditor({ value, onChange }: { value: CustomFieldDef[]; onChange: (v: CustomFieldDef[]) => void }) {
  const upd = (i: number, p: Partial<CustomFieldDef>) => onChange(value.map((f, j) => j === i ? { ...f, ...p } : f))
  return (
    <div className="grid" style={{ gap: 8 }}>
      {value.map((f, i) => <div key={i} className="cf-item">
        <div className="cf-line">
          <input value={f.label} onChange={e => upd(i, { label: e.target.value })} placeholder="Label (e.g. PFR broj računa)" aria-label="Label" className="grow" />
          <input value={f.key} onChange={e => upd(i, { key: e.target.value })} placeholder="key (auto)" aria-label="Key" className="mono" style={{ width: 170 }} />
        </div>
        <div className="cf-line">
          <div style={{ width: 200 }}><Select value={f.fill || ''} onChange={v => upd(i, { fill: v as CustomFieldDef['fill'] })} ariaLabel="Auto-fill"
            options={[{ value: '', label: 'No auto-fill' }, { value: 'copy', label: 'Copy previous' }, { value: 'increment', label: 'Increment previous' }]} /></div>
          <label className="check"><input type="checkbox" checked={f.show_on_pdf} onChange={e => upd(i, { show_on_pdf: e.target.checked })} /> show on PDF</label>
          <button type="button" className="btn ghost sm danger" style={{ marginLeft: 'auto' }} onClick={() => onChange(value.filter((_, j) => j !== i))}>Remove</button>
        </div>
      </div>)}
      <div><button type="button" className="btn sm" onClick={() => onChange([...value, { key: '', label: '', show_on_pdf: true }])}><Icon name="plus" size={15} />Add field</button></div>
    </div>
  )
}

function Invoicing() {
  const { settings } = useApp()
  const { s, set, put, save, busy, setS } = useSettingsForm()
  const { data: next } = useAsync(() => api.get<{ number: string }>(`${V1}/invoices/next-number`))
  if (!s) return <Loading />
  const dirty = JSON.stringify(s) !== JSON.stringify(settings)
  return (
    <>
      <div className="stack loose">
        <div className="stack loose">
          <Section title="Numbering">
            <div className="grid" style={{ gap: 12 }}>
              <Field label="Invoice number format" help={<>Placeholders: {'{YYYY} {YY} {MM} {DD} {SEQ} {SEQ:4} {CLIENT}'} · next: <code>{next?.number}</code></>}><input value={s.invoice_number_format} onChange={set('invoice_number_format')} /></Field>
              <div className="form-grid">
                <Field label="Next sequence number"><input type="number" value={s.invoice_next_seq} onChange={set('invoice_next_seq')} /></Field>
              </div>
              <SettingRow title="Reset every year" help="The sequence starts again at 1 on January 1st"><Switch checked={s.invoice_seq_reset_yearly} onChange={put('invoice_seq_reset_yearly')} label="Reset every year" /></SettingRow>
            </div>
          </Section>
          <Section title="Custom invoice fields">
            <p className="muted small" style={{ marginTop: 0 }}>Extra fields per invoice (fiscal receipt number, local invoice number, project code…), shown in the editor and, if enabled, in the PDF header. Auto-fill takes the value from the client's previous sent or paid invoice: <b>Copy</b> reuses it, <b>Increment</b> adds 1 to its last number (14/15ПП → 15/16ПП adds 1 to both, ELJA9MLH-…-15 → …-16).</p>
            <CustomFieldsEditor value={s.custom_fields || []} onChange={v => setS(x => x ? { ...x, custom_fields: v } : x)} />
          </Section>
        </div>
        <Section title="Defaults for new invoices">
          <div className="form-grid">
            <Field label="Payment terms (days)"><input type="number" value={s.default_due_days} onChange={set('default_due_days')} /></Field>
            <Field label="Payment method" help="Used when recording a payment and by Mark fully paid; clients can override it"><Select value={s.default_payment_method || 'bank_transfer'} onChange={put('default_payment_method')} options={PAYMENT_METHODS.map(m => ({ value: m, label: methodLabel(m) }))} /></Field>
            <Field label="Default billing mode"><Select value={s.default_billing_mode} onChange={put('default_billing_mode')} options={BILLING_MODES.map(b => ({ value: b.value, label: b.label }))} /></Field>
            <Field label="Default hourly rate"><input type="number" step="0.01" value={s.default_hourly_rate} onChange={set('default_hourly_rate')} /></Field>
            <Field label="Default daily rate"><input type="number" step="0.01" value={s.default_daily_rate} onChange={set('default_daily_rate')} /></Field>
            <Field label="Default monthly rate"><input type="number" step="0.01" value={s.default_monthly_rate} onChange={set('default_monthly_rate')} /></Field>
            <Field label="Default tax rate (%)"><input type="number" step="0.01" value={s.default_tax_rate} onChange={set('default_tax_rate')} /></Field>
            <Field label="Hours per day" help="Used when billing tracked hours as days"><input type="number" step="0.5" value={s.hours_per_day} onChange={set('hours_per_day')} /></Field>
            <Field label="Round timer to (minutes)" help="1 = no rounding"><input type="number" value={s.time_rounding_minutes} onChange={set('time_rounding_minutes')} /></Field>
            <Field label="Number format"><Select value={s.number_format} onChange={put('number_format')} options={['1,234.56', '1.234,56', '1 234,56', "1'234.56", '1234.56'].map(f => ({ value: f, label: f }))} /></Field>
            <Field label="Email attachment" help="What to attach when emailing an invoice"><Select value={s.email_attachment_mode} onChange={put('email_attachment_mode')} options={[{ value: 'generated', label: 'Generated PDF' }, { value: 'uploaded', label: 'Uploaded files only', hint: 'falls back to generated if none' }, { value: 'both', label: 'Generated PDF + uploaded files' }]} /></Field>
          </div>
          <div className="mt">
            <SettingRow title="Show tax column on invoices"><Switch checked={s.show_tax_column} onChange={put('show_tax_column')} label="Show tax column on invoices" /></SettingRow>
            <SettingRow title="Always print the tax line in the totals" help="0% when the invoice has no tax"><Switch checked={!!s.always_show_tax} onChange={put('always_show_tax')} label="Always print the tax line" /></SettingRow>
          </div>
        </Section>
        <Section title="Text printed on invoices" className="full">
          <p className="muted small" style={{ marginTop: 0 }}>In the order they appear on the PDF. Notes, terms and footer are copied into each new invoice, where you can still change them.</p>
          <SettingRow title={`Also print the total in ${s.base_currency}`} help="When an invoice is in another currency"><Switch checked={s.show_base_total} onChange={put('show_base_total')} label="Also print the total in the base currency" /></SettingRow>
          <div className="form-grid mt">
            <Field label="Sentence under the totals" className="full" help="Placeholders: {rate} {currency} {currency_name} {base} {base_name} {total_base} {total}"><textarea rows={2} value={s.base_total_note} onChange={set('base_total_note')} disabled={!s.show_base_total} /></Field>
            <Field label="Notes" help="e.g. a VAT exemption sentence; also used by recurring profiles without their own notes"><textarea rows={4} value={s.default_notes} onChange={set('default_notes')} /></Field>
            <Field label="Terms" help="{due_days} is replaced"><textarea rows={4} value={s.default_terms} onChange={set('default_terms')} /></Field>
            <Field label="Footer" className="full" help="Bottom of every page"><textarea rows={2} value={s.default_footer} onChange={set('default_footer')} /></Field>
          </div>
        </Section>
        <WorkingDaysCard s={s} setS={setS} />
      </div>
      <div className="save-sticky">
        {dirty && <span className="muted small">Unsaved changes</span>}
        <button type="button" className="btn primary" disabled={busy || !dirty} onClick={save}>Save changes</button>
      </div>
    </>
  )
}

// ---------- Working days & holidays ----------
const WEEKDAYS: [number, string][] = [[1, 'Mon'], [2, 'Tue'], [3, 'Wed'], [4, 'Thu'], [5, 'Fri'], [6, 'Sat'], [7, 'Sun']]
function WorkingDaysCard({ s, setS }: { s: S; setS: React.Dispatch<React.SetStateAction<S | null>> }) {
  const toast = useToast()
  const { data: presets } = useAsync(() => api.get<{ id: string; label: string }[]>(`${V1}/calendar/presets`))
  const [preset, setPreset] = useState('rs')
  const [year, setYear] = useState(new Date().getFullYear())
  const [month, setMonth] = useState(new Date().toISOString().slice(0, 7))
  const { data: wd } = useAsync(() => api.get<WorkingDays>(`${V1}/calendar/working-days?month=${month}`), [month, s.work_week, JSON.stringify(s.holidays)])
  const week = new Set((s.work_week || '1,2,3,4,5').split(',').map(Number))
  const holidays = s.holidays || []
  const setHol = (h: Holiday[]) => setS(x => x ? { ...x, holidays: h } : x)
  const toggleDay = (d: number) => { const w = new Set(week); if (w.has(d)) w.delete(d); else w.add(d); setS(x => x ? { ...x, work_week: Array.from(w).sort().join(',') } : x) }
  const addPreset = async () => {
    try { const list = await api.get<Holiday[]>(`${V1}/calendar/presets?set=${preset}&year=${year}`); const keys = new Set(holidays.map(h => h.yearly ? 'Y' + h.date.slice(5) : h.date)); const add = list.filter(h => !keys.has(h.yearly ? 'Y' + h.date.slice(5) : h.date)); setHol([...holidays, ...add]); toast(add.length ? `${add.length} holidays added — save to keep them` : 'Nothing new to add', add.length ? 'success' : 'info') } catch (e) { toast((e as Error).message, 'error') }
  }
  const sorted = [...holidays].sort((a, b) => (a.yearly === b.yearly ? (a.yearly ? a.date.slice(5).localeCompare(b.date.slice(5)) : a.date.localeCompare(b.date)) : a.yearly ? -1 : 1))
  return (
    <Section title="Working days & holidays" className="full">
      <p className="muted small">Used by recurring profiles with "Quantity: working days of the period" and by the month-end wizard to prefill the number of days or hours. Yearly holidays repeat every year; one-off entries (vacation, movable feasts such as Orthodox Easter) apply to that date only.</p>
      <div className="stack loose">
        <div>
          <Field label="Work week"><div className="row wrap" style={{ gap: 10 }}>{WEEKDAYS.map(([d, l]) => <label key={d} className="check"><input type="checkbox" checked={week.has(d)} onChange={() => toggleDay(d)} /> {l}</label>)}</div></Field>
          <div className="row wrap mt" style={{ gap: 8, alignItems: 'flex-start' }}>
            <Field label="Add public holidays"><Select className="holiday-preset" value={preset} onChange={setPreset} options={(presets || []).filter(p => p.id !== 'weekend').map(p => ({ value: p.id, label: p.label }))} /></Field>
            <Field label="Year" help="Only matters for movable feasts"><input type="number" value={year} onChange={e => setYear(Number(e.target.value) || year)} style={{ width: 90 }} /></Field>
            <div className="field-like"><span>&nbsp;</span><button type="button" className="btn" onClick={addPreset}>Add</button></div>
          </div>
          <div className="table-wrap mt"><table className="table">
            <thead><tr><th style={{ width: 170 }}>Date</th><th>Name</th><th style={{ width: 150 }}>Repeats</th><th style={{ width: 90 }}></th></tr></thead>
            <tbody>
              {sorted.map(h => { const i = holidays.indexOf(h); return <tr key={h.date + h.name + i}>
                <td><input type="date" value={h.date} style={{ width: '100%' }} onChange={e => setHol(holidays.map((x, j) => j === i ? { ...x, date: e.target.value } : x))} /></td>
                <td><input value={h.name} style={{ width: '100%', minWidth: 180 }} placeholder="Holiday or day off" onChange={e => setHol(holidays.map((x, j) => j === i ? { ...x, name: e.target.value } : x))} /></td>
                <td style={{ whiteSpace: 'nowrap' }}><label className="check"><input type="checkbox" checked={h.yearly} onChange={e => setHol(holidays.map((x, j) => j === i ? { ...x, yearly: e.target.checked } : x))} /> {h.yearly ? 'every year' : 'once'}</label></td>
                <td className="actions"><button type="button" className="btn ghost sm danger" onClick={() => setHol(holidays.filter((_, j) => j !== i))}>Remove</button></td>
              </tr> })}
              <tr><td colSpan={4}><button type="button" className="btn sm" onClick={() => setHol([...holidays, { date: today(), name: '', yearly: false }])}><Icon name="plus" size={15} />Add day off</button></td></tr>
            </tbody>
          </table></div>
        </div>
        <div className="cf-item">
          <div className="setting-title">Preview</div>
          <Field label="Month"><input type="month" value={month} onChange={e => setMonth(e.target.value)} /></Field>
          {wd && <div className="mt">
            <div style={{ fontSize: 28, fontWeight: 600 }}>{wd.working_days} <span className="muted" style={{ fontSize: 14, fontWeight: 400 }}>working days · {wd.working_hours} h</span></div>
            <div className="muted small">{wd.week_days} work-week days, {wd.holidays.length} holiday{wd.holidays.length === 1 ? '' : 's'} on working days</div>
            {wd.holidays.length > 0 && <ul className="small mt" style={{ paddingLeft: 18 }}>{wd.holidays.map(h => <li key={h.date}>{fmtDate(h.date)} ({h.weekday}) – {h.name}</li>)}</ul>}
            <div className="muted small mt">Saved settings are used; unsaved changes above are previewed live.</div>
          </div>}
        </div>
      </div>
    </Section>
  )
}

// ---------- Currencies ----------
function Currencies() {
  const { settings, refresh, user } = useApp()
  const isAdmin = user?.role === 'admin'
  const toast = useToast()
  const { s, set, put, save, busy } = useSettingsForm()
  const { data: list, reload } = useAsync(() => api.get<Currency[]>(`${V1}/currencies`))
  const { data: rates, reload: reloadRates } = useAsync(() => api.get<{ base: string; rates: ExchangeRate[]; provider: string; default_provider: string; providers: { ID: string; Label: string }[] }>(`${V1}/rates`))
  const [manual, setManual] = useState({ quote: '', rate: '' })
  const [filter, setFilter] = useState('')
  const [refreshing, setRefreshing] = useState(false)
  if (!s || !list) return <Loading />
  const toggle = async (c: Currency) => { await api.put(`${V1}/currencies/${c.code}`, { ...c, enabled: !c.enabled }); reload(); refresh() }
  const refreshRates = async () => { setRefreshing(true); try { const r = await api.post<{ updated: number }>(`${V1}/rates/refresh`); toast(`${r.updated} rates updated from ${rates?.provider}`, 'success'); reloadRates() } catch (e) { toast((e as Error).message, 'error') } finally { setRefreshing(false) } }
  const addManual = async () => { try { await api.put(`${V1}/rates`, { quote: manual.quote.toUpperCase(), rate: parseFloat(manual.rate) }); setManual({ quote: '', rate: '' }); reloadRates(); toast('Rate saved', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  const base = settings?.base_currency || ''
  return (
    <div className="stack loose">
      <div className="grid">
        <Section title="Base currency">
          {!isAdmin && <div className="callout mb">Currencies and exchange rates are shared by everyone on this server; only an admin can change them.</div>}
          <Field label="Base currency" help="Reports and the dashboard are shown in this currency. Changing it does not alter existing invoices."><Select value={s.base_currency} onChange={put('base_currency')} disabled={!isAdmin} options={list.map(c => ({ value: c.code, label: c.code, hint: c.name }))} /></Field>
          <Field label="Rate source" help={s.exchange_rate_provider === 'cbbh' ? 'Official daily list of the Central Bank of BiH (middle rate). New invoices lock the rate valid on their issue date, so backdated invoices get that day\'s rate.' : 'Where automatic rates come from. Invoices lock the stored rate at creation; you can override it per invoice.'}>
            <Select value={s.exchange_rate_provider || ''} onChange={put('exchange_rate_provider')} disabled={!isAdmin} placeholder="Server default"
              options={[{ value: '', label: 'Server default', hint: rates?.default_provider || '…' }, ...(rates?.providers || []).map(p => ({ value: p.ID, label: p.Label }))]} />
          </Field>
          {isAdmin && <SaveBar onSave={async () => { await save(); reloadRates() }} busy={busy} />}
        </Section>
        <Section title={`Exchange rates (1 ${base} = …)`} actions={isAdmin && <button type="button" className="btn sm" disabled={refreshing || rates?.provider === 'none'} onClick={refreshRates}>{refreshing ? 'Refreshing…' : `Refresh from ${rates?.provider || 'provider'}`}</button>} flush>
          {rates?.provider === 'none' && <div className="callout warn" style={{ margin: 12 }}>Automatic rates are disabled. Pick a rate source above or enter rates manually below.</div>}
          {rates?.provider === 'cbbh' && base !== 'BAM' && <div className="callout warn" style={{ margin: 12 }}>The Central Bank of BiH quotes everything against BAM. With {base} as base currency, rates are derived through BAM.</div>}
          <table className="table"><thead><tr><th>Quote</th><th className="num">Rate</th><th>Source</th><th>Updated</th><th></th></tr></thead>
            <tbody>{rates?.rates.map(r => <tr key={r.quote}><td className="bold">{r.quote}</td><td className="num">{r.rate.toFixed(6)}</td><td className="muted">{r.source}</td><td className="muted small">{fmtDateTime(r.fetched_at)}</td><td className="actions">{isAdmin && <button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={async () => { await api.del(`${V1}/rates/${r.quote}`); reloadRates() }}><Icon name="trash" size={14} /></button>}</td></tr>)}
              {isAdmin && <tr><td><input value={manual.quote} onChange={e => setManual({ ...manual, quote: e.target.value })} placeholder="USD" style={{ width: 80 }} /></td><td><input value={manual.rate} onChange={e => setManual({ ...manual, rate: e.target.value })} placeholder="1.08" style={{ width: 110 }} /></td><td colSpan={2} className="muted small">Manual rates are never overwritten by refresh.</td><td className="actions"><button type="button" className="btn sm" onClick={addManual} disabled={!manual.quote || !manual.rate}>Add</button></td></tr>}
            </tbody></table>
        </Section>
      </div>
      <Section title="Enabled currencies" actions={<input placeholder="Filter…" value={filter} onChange={e => setFilter(e.target.value)} style={{ width: 140 }} />} flush>
        <div style={{ maxHeight: 600, overflow: 'auto' }}><table className="table"><tbody>{list.filter(c => !filter || c.code.includes(filter.toUpperCase()) || c.name.toLowerCase().includes(filter.toLowerCase())).map(c => <tr key={c.code}><td><label className="check"><input type="checkbox" checked={c.enabled} disabled={!isAdmin || c.code === base} onChange={() => toggle(c)} /> <strong>{c.code}</strong> <span className="muted">{c.name}</span></label></td><td className="num muted">{money(1234.5, c.code)}</td></tr>)}</tbody></table></div>
      </Section>
    </div>
  )
}

// ---------- Taxes ----------
function IncomeTaxSettings() {
  const { s, set, put, save, busy } = useSettingsForm()
  if (!s) return <Loading />
  return (
    <Section title="Income tax estimate (your own tax, not shown on invoices)">
      <p className="muted small">Used by Reports → Income tax to estimate what you owe per month and year. Examples for Republika Srpska: regular SP = 10% of income with fixed monthly contributions; "mali preduzetnik" = 2% of gross income with a 600 KM yearly minimum.</p>
      <div className="form-grid">
        <Field label="Label"><input value={s.income_tax_label} onChange={set('income_tax_label')} placeholder="Porez na dohodak" /></Field>
        <Field label="Rate (%)"><input type="number" step="0.01" value={s.income_tax_rate} onChange={set('income_tax_rate')} /></Field>
        <Field label="Tax base"><Select value={s.income_tax_basis} onChange={put('income_tax_basis')} options={[{ value: 'revenue', label: 'Gross income', hint: 'received' }, { value: 'profit', label: 'Income minus expenses' }]} /></Field>
        <Field label="Count income by"><Select value={s.income_tax_by_payment_date ? 'payment' : 'invoice'} onChange={v => put('income_tax_by_payment_date')(v === 'payment')} options={[{ value: 'payment', label: 'Payment date', hint: 'cash basis' }, { value: 'invoice', label: 'Invoice date' }]} /></Field>
        <Field label="Fixed monthly contributions" help="Doprinosi, in base currency; shown as a separate line"><input type="number" step="0.01" value={s.contributions_monthly} onChange={set('contributions_monthly')} /></Field>
        <Field label="Minimum yearly tax" help="0 = none"><input type="number" step="0.01" value={s.income_tax_min_yearly} onChange={set('income_tax_min_yearly')} /></Field>
        <Field label="Yearly deduction / allowance" help="Subtracted from the yearly base before applying the rate"><input type="number" step="0.01" value={s.income_tax_deduction} onChange={set('income_tax_deduction')} /></Field>
      </div>
      <SaveBar onSave={save} busy={busy} />
    </Section>
  )
}

function Taxes() {
  const toast = useToast()
  const { data, reload } = useAsync(() => api.get<TaxRate[]>(`${V1}/tax-rates`))
  const [form, setForm] = useState({ name: '', rate: '', is_default: false })
  const add = async () => { try { await api.post(`${V1}/tax-rates`, { name: form.name, rate: parseFloat(form.rate) || 0, is_default: form.is_default }); setForm({ name: '', rate: '', is_default: false }); reload(); toast('Tax rate added', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="stack loose">
    <Section title="Tax rates (applied to invoice lines)" flush>
      <table className="table"><thead><tr><th>Name</th><th className="num">Rate</th><th>Default</th><th></th></tr></thead>
        <tbody>{data?.map(t => <tr key={t.id}><td>{t.name}</td><td className="num">{t.rate}%</td><td>{t.is_default ? <span className="badge ok">default</span> : <button type="button" className="link-btn small" onClick={async () => { await api.put(`${V1}/tax-rates/${t.id}`, { ...t, is_default: true }); reload() }}>make default</button>}</td><td className="actions"><button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={async () => { await api.del(`${V1}/tax-rates/${t.id}`); reload() }}><Icon name="trash" size={14} /></button></td></tr>)}
          <tr><td><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="VAT 20%" /></td><td><input value={form.rate} onChange={e => setForm({ ...form, rate: e.target.value })} placeholder="20" style={{ width: 90 }} /></td><td><label className="check"><input type="checkbox" checked={form.is_default} onChange={e => setForm({ ...form, is_default: e.target.checked })} /> default</label></td><td className="actions"><button type="button" className="btn sm primary" onClick={add} disabled={!form.rate}>Add</button></td></tr>
        </tbody></table>
      {!data?.length && <div className="callout" style={{ margin: 12 }}>Tip: add a 0% rate named "Reverse charge" for cross-border B2B invoices.</div>}
    </Section>
    <IncomeTaxSettings />
    </div>
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
      <Section title="Products & services" actions={<button type="button" className="btn sm primary" onClick={() => setEditing({ name: '', description: '', unit: 'hour', unit_price: 0, currency: '', tax_rate: settings?.default_tax_rate || 0 })}><Icon name="plus" size={15} />Add</button>} flush>
        {!data?.length ? <Empty title="Catalog is empty">Add your standard rates (hourly, daily, retainer) to insert them into invoices with one click.</Empty> : <table className="table"><thead><tr><th>Name</th><th>Unit</th><th className="num">Price</th><th className="num">Tax</th><th></th></tr></thead>
          <tbody>{data.map(p => <tr key={p.id}><td><div className="bold">{p.name}</div><div className="muted small">{p.description}</div></td><td className="muted">{p.unit}</td><td className="num">{money(p.unit_price, p.currency || settings?.base_currency || '')}</td><td className="num">{p.tax_rate}%</td><td className="actions"><button type="button" className="btn ghost sm" onClick={() => setEditing(p)}>Edit</button><button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={async () => { await api.del(`${V1}/products/${p.id}`); reload() }}><Icon name="trash" size={14} /></button></td></tr>)}</tbody></table>}
      </Section>
      {editing && <Modal title={editing.id ? 'Edit item' : 'New catalog item'} onClose={() => setEditing(null)}>
        <div className="form-grid">
          <Field label="Name" className="full"><input value={editing.name} onChange={e => setEditing({ ...editing, name: e.target.value })} autoFocus /></Field>
          <Field label="Description" className="full"><input value={editing.description} onChange={e => setEditing({ ...editing, description: e.target.value })} /></Field>
          <Field label="Unit"><Select value={editing.unit || ''} onChange={v => setEditing({ ...editing, unit: v })} options={UNITS.map(u => ({ value: u.value, label: u.label }))} /></Field>
          <Field label="Unit price"><input type="number" step="0.01" value={editing.unit_price} onChange={e => setEditing({ ...editing, unit_price: parseFloat(e.target.value) || 0 })} /></Field>
          <Field label="Currency" help="Blank = any"><Select value={editing.currency || ''} onChange={v => setEditing({ ...editing, currency: v })} placeholder="Any" options={[{ value: '', label: 'Any' }, ...currencies.filter(c => c.enabled).map(c => ({ value: c.code, label: c.code, hint: c.name }))]} /></Field>
          <Field label="Tax rate (%)"><input type="number" step="0.01" value={editing.tax_rate} onChange={e => setEditing({ ...editing, tax_rate: parseFloat(e.target.value) || 0 })} /></Field>
        </div>
        <div className="form-actions"><button type="button" className="btn primary" onClick={save} disabled={!editing.name}>Save</button></div>
      </Modal>}
    </>
  )
}

// ---------- Templates ----------
const LABEL_KEYS = ['invoice', 'invoice_number', 'issue_date', 'issue_place', 'due_date', 'period', 'po_number', 'bill_to', 'description', 'unit', 'quantity', 'unit_price', 'discount', 'tax', 'amount', 'subtotal', 'total', 'amount_paid', 'balance_due', 'notes', 'terms', 'payment_details', 'tax_id', 'vat_id', 'client_tax_id', 'registration_no', 'page', 'paid_stamp', 'hour', 'hours', 'day', 'days', 'month', 'months']

function WordUpload({ templateId, onDone, label = 'Upload Word template' }: { templateId?: number; onDone: (t: InvoiceTemplate, ph: string[]) => void; label?: string }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  return (
    <label className="btn sm">{busy ? 'Uploading…' : <><Icon name="upload" size={14} />{label}</>}<input type="file" accept=".docx" style={{ display: 'none' }} onChange={async e => {
      const f = e.target.files?.[0]; if (!f) return
      const fd = new FormData(); fd.append('file', f); setBusy(true)
      try { const r = await api.post<{ template: InvoiceTemplate; placeholders: string[] }>(templateId ? `${V1}/templates/${templateId}/docx` : `${V1}/templates/docx`, fd); toast(`Word template saved (${r.placeholders.length} placeholders found)`, 'success'); onDone(r.template, r.placeholders) } catch (err) { toast((err as Error).message, 'error') } finally { setBusy(false); e.target.value = '' }
    }} /></label>
  )
}

function Templates() {
  const toast = useToast()
  const { data, reload } = useAsync(() => api.get<InvoiceTemplate[]>(`${V1}/templates`))
  const { data: keys } = useAsync(() => api.get<{ Key: string; Description: string }[]>(`${V1}/templates/placeholders`))
  const [sel, setSel] = useState<InvoiceTemplate | null>(null)
  const [tab, setTab] = useState<'design' | 'labels' | 'html' | 'word'>('design')
  const [previewKey, setPreviewKey] = useState(0)
  const [del, setDel] = useState<InvoiceTemplate | null>(null)
  const [found, setFound] = useState<string[]>([])
  useEffect(() => { if (data && !sel) setSel(data[0] || null) }, [data, sel])
  const save = async () => { if (!sel) return; try { const saved = sel.id ? await api.put<InvoiceTemplate>(`${V1}/templates/${sel.id}`, sel) : await api.post<InvoiceTemplate>(`${V1}/templates`, sel); setSel(saved); reload(); setPreviewKey(k => k + 1); toast('Template saved', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  const loadDefaultHTML = async () => { const html = await api.getText(`${V1}/templates/default-html`); setSel(s => s ? { ...s, html } : s) }
  if (!data) return <Loading />
  const previewUrl = sel?.id ? `${V1}/templates/${sel.id}/preview?layout=${sel.layout}&accent=${encodeURIComponent(sel.accent_color)}&k=${previewKey}` : ''
  return (
    <div className="grid split-tpl">
      <Card title="Templates" actions={<div className="row"><WordUpload onDone={(t, ph) => { reload(); setSel(t); setFound(ph); setTab('word'); setPreviewKey(k => k + 1) }} label="Word" /><button type="button" className="btn sm" title="New design template" onClick={() => setSel({ id: 0, name: 'New template', layout: 'classic', accent_color: '#2563eb', labels: {}, options: { hide_rate: false, hide_unit: false, show_quantity_total: false, signature_label: '', hide_logo: false }, html: '', is_default: false, kind: 'design', docx_path: '', docx_name: '' })}><Icon name="plus" size={15} />Design</button></div>} flush>
        <table className="table"><tbody>{data.map(t => <tr key={t.id} className="clickable" onClick={() => { setSel(t); setFound([]); setTab(t.kind === 'docx' ? 'word' : 'design'); setPreviewKey(k => k + 1) }} style={sel?.id === t.id ? { background: 'var(--accent-soft)' } : {}}><td><span className="bold">{t.name}</span>{t.is_default && <span className="badge ok" style={{ marginLeft: 6 }}>default</span>}<div className="muted small">{t.kind === 'docx' ? <span className="badge accent">Word</span> : t.layout}{t.html ? ' · custom HTML' : ''}</div></td></tr>)}</tbody></table>
        <div className="muted small" style={{ padding: 12 }}>Word templates: a .docx with placeholders like <code>{'{{number}}'}</code>. Each client can pick its own template; otherwise the default applies. Download <a href="https://github.com/abjelosevic88/pocket-homelab-invoicing/tree/main/docs/templates" target="_blank" rel="noreferrer">sample templates</a>.</div>
      </Card>
      {sel && <Card title={sel.id ? `Edit "${sel.name}"` : 'New template'} actions={<div className="row">{sel.id > 0 && !sel.is_default && <button type="button" className="btn sm danger" onClick={() => setDel(sel)}>Delete</button>}<button type="button" className="btn sm primary" onClick={save}>Save</button></div>}>
        <Tabs tabs={sel.kind === 'docx' ? [{ id: 'word' as const, label: 'Word file' }, { id: 'labels' as const, label: 'Labels / language' }] : [{ id: 'design' as const, label: 'Design' }, { id: 'labels' as const, label: 'Labels / language' }, { id: 'html' as const, label: 'HTML' }]} value={tab} onChange={setTab} />
        {tab === 'word' && <div className="grid" style={{ gap: 12 }}>
          <Field label="Name"><input value={sel.name} onChange={e => setSel({ ...sel, name: e.target.value })} /></Field>
          <SettingRow title="Default template" help="Used by clients without a template of their own"><Switch checked={sel.is_default} onChange={v => setSel({ ...sel, is_default: v })} label="Use as default template" /></SettingRow>
          <div className="row"><span className="muted small">File: <strong>{sel.docx_name || '—'}</strong></span>{sel.id > 0 && <a className="btn sm" href={`${V1}/templates/${sel.id}/docx`}>Download</a>}{sel.id > 0 && <WordUpload templateId={sel.id} label="Replace file" onDone={(t, ph) => { reload(); setSel(t); setFound(ph); setPreviewKey(k => k + 1) }} />}</div>
          {found.length > 0 && <div className="callout small">Placeholders found in the file: {found.join(' ')}</div>}
          <details><summary className="small bold" style={{ cursor: 'pointer' }}>Available placeholders</summary>
            <table className="table small" style={{ marginTop: 8 }}><tbody>{keys?.map(k => <tr key={k.Key}><td className="mono">{'{{' + k.Key + '}}'}</td><td className="muted">{k.Description}</td></tr>)}</tbody></table>
            <p className="muted small mt">Put <code>{'{{#items}}'}</code> in the first cell of a table row and <code>{'{{/items}}'}</code> in its last cell to repeat that row per line item. Sections like <code>{'{{#paid}} … {{/paid}}'}</code> show text conditionally; <code>{'{{^paid}}'}</code> is the inverse. PDF conversion uses the Gotenberg container (LibreOffice).</p>
          </details>
        </div>}
        {tab === 'design' && <div className="grid" style={{ gap: 12 }}>
          <Field label="Name"><input value={sel.name} onChange={e => setSel({ ...sel, name: e.target.value })} /></Field>
          <Field label="Layout"><Select value={sel.layout} onChange={v => setSel({ ...sel, layout: v })} options={[{ value: 'classic', label: 'Classic', hint: 'header left, accent table' }, { value: 'modern', label: 'Modern', hint: 'full-width coloured header' }, { value: 'minimal', label: 'Minimal', hint: 'black & white, thin rules' }]} /></Field>
          <Field label="Accent colour"><div className="row"><input type="color" value={sel.accent_color} onChange={e => setSel({ ...sel, accent_color: e.target.value })} /><input value={sel.accent_color} onChange={e => setSel({ ...sel, accent_color: e.target.value })} style={{ width: 120 }} /></div></Field>
          <div>
            <SettingRow title="Default template" help="Used by clients without a template of their own"><Switch checked={sel.is_default} onChange={v => setSel({ ...sel, is_default: v })} label="Use as default template" /></SettingRow>
            <SettingRow title="Hide the rate column" help="The unit price"><Switch checked={!!sel.options?.hide_rate} onChange={v => setSel({ ...sel, options: { ...sel.options, hide_rate: v } })} label="Hide the rate column" /></SettingRow>
            <SettingRow title="Hide the unit column" help={'Quantity shows as "21 days"'}><Switch checked={!!sel.options?.hide_unit} onChange={v => setSel({ ...sel, options: { ...sel.options, hide_unit: v } })} label="Hide the unit column" /></SettingRow>
            <SettingRow title="Summed quantity in the totals" help={'e.g. "100 hours"'}><Switch checked={!!sel.options?.show_quantity_total} onChange={v => setSel({ ...sel, options: { ...sel.options, show_quantity_total: v } })} label="Show summed quantity in totals" /></SettingRow>
            <SettingRow title="Hide logo"><Switch checked={!!sel.options?.hide_logo} onChange={v => setSel({ ...sel, options: { ...sel.options, hide_logo: v } })} label="Hide logo" /></SettingRow>
          </div>
          <Field label="Signature line label" help="Leave empty for none, e.g. 'Odgovorno lice' or 'Authorised signature'"><input value={sel.options?.signature_label || ''} onChange={e => setSel({ ...sel, options: { ...sel.options, signature_label: e.target.value } })} /></Field>
          <p className="muted small">Design settings apply to all PDF engines. Save to refresh the preview.</p>
        </div>}
        {tab === 'labels' && <div>
          <p className="muted small">Override any label to localise invoices (e.g. "INVOICE" → "RECHNUNG" / "FAKTURA"). Blank = English default.</p>
          <div className="form-grid cols-2" style={{ gap: 8 }}>{LABEL_KEYS.map(k => <Field key={k} label={k}><input value={sel.labels[k] || ''} onChange={e => setSel({ ...sel, labels: { ...sel.labels, [k]: e.target.value } })} /></Field>)}</div>
        </div>}
        {tab === 'html' && <div>
          <p className="muted small">Custom HTML (Go <code>html/template</code> syntax) is used by the <strong>chromium</strong> and <strong>gotenberg</strong> PDF engines and for the public web view. The native engine ignores it. <button type="button" className="link-btn" onClick={loadDefaultHTML}>Load built-in template</button> to start from.</p>
          <textarea className="mono" rows={24} value={sel.html} onChange={e => setSel({ ...sel, html: e.target.value })} placeholder="Leave empty to use the built-in HTML template" />
        </div>}
      </Card>}
      {sel && sel.id > 0 && <Card title="Preview (sample data)" actions={<a className="btn sm" href={`${V1}/templates/${sel.id}/preview.pdf?layout=${sel.layout}&accent=${encodeURIComponent(sel.accent_color)}&k=${previewKey}`} target="_blank" rel="noreferrer">Open PDF</a>} flush>
        {sel.kind === 'docx' ? <iframe key={previewKey} src={`${V1}/templates/${sel.id}/preview.pdf?k=${previewKey}`} title="preview" style={{ width: '100%', height: 760, border: 0, background: '#fff' }} /> : <div style={{ height: 760, overflow: 'hidden' }}><iframe key={previewKey} src={previewUrl} title="preview" style={{ width: '200%', height: 1520, border: 0, transform: 'scale(0.5)', transformOrigin: '0 0', background: '#fff' }} /></div>}
      </Card>}
      {del && <Confirm title="Delete template?" message="Invoices using it fall back to the default template." onConfirm={async () => { await api.del(`${V1}/templates/${del.id}`); setDel(null); setSel(null); reload() }} onCancel={() => setDel(null)} />}
    </div>
  )
}

// ---------- Email ----------
function Email() {
  const { s, set, put, save, busy } = useSettingsForm()
  const toast = useToast()
  const [to, setTo] = useState('')
  if (!s) return <Loading />
  const test = async () => { try { await save(); await api.post(`${V1}/settings/test-email`, { to }); toast('Test email sent', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="stack loose">
      <Section title="SMTP server">
        <div className="form-grid">
          <Field label="Host" className="full"><input value={s.smtp_host} onChange={set('smtp_host')} placeholder="smtp.example.com" /></Field>
          <Field label="Port"><input type="number" value={s.smtp_port} onChange={set('smtp_port')} /></Field>
          <Field label="Encryption"><Select value={s.smtp_tls} onChange={put('smtp_tls')} options={[{ value: 'starttls', label: 'STARTTLS', hint: '587' }, { value: 'tls', label: 'TLS/SSL', hint: '465' }, { value: 'none', label: 'None', hint: '25' }]} /></Field>
          <Field label="Username"><input value={s.smtp_user} onChange={set('smtp_user')} /></Field>
          <Field label="Password" help={s.smtp_password_set ? 'A password is stored. Leave blank to keep it.' : 'Leave blank to keep the current one'}><input type="password" value={s.smtp_password} onChange={set('smtp_password')} autoComplete="new-password" placeholder={s.smtp_password_set ? '••••••••' : ''} /></Field>
          <Field label="From address"><input value={s.smtp_from} onChange={set('smtp_from')} placeholder="billing@example.com" /></Field>
          <Field label="From name"><input value={s.smtp_from_name} onChange={set('smtp_from_name')} /></Field>
          <Field label="BCC (copy of every invoice)" className="full"><input value={s.smtp_bcc} onChange={set('smtp_bcc')} /></Field>
        </div>
        <div className="row between mt"><div className="row"><input placeholder="Send test to…" value={to} onChange={e => setTo(e.target.value)} style={{ width: 220 }} /><button type="button" className="btn" onClick={test} disabled={!s.smtp_host}>Save & send test</button></div><button type="button" className="btn primary" disabled={busy} onClick={save}>Save</button></div>
      </Section>
      <Section title="Invoice email & reminders">
        <div className="grid" style={{ gap: 12 }}>
          <Field label="Subject"><input value={s.email_subject} onChange={set('email_subject')} /></Field>
          <Field label="Body" help="Placeholders: {number} {number_short} {client} {contact} {first_name} {company} {total} {balance} {due_date} {issue_date} {month} {year} {period} {link}. Clients can override subject and message on their own page."><textarea rows={8} value={s.email_body} onChange={set('email_body')} /></Field>
          <SettingRow title="Payment reminders" help="Email automatic reminders for overdue invoices"><Switch checked={s.reminders_enabled} onChange={put('reminders_enabled')} label="Send automatic payment reminders" /></SettingRow>
          <Field label="Remind on days after due date" help="Comma separated"><input value={s.reminder_days} onChange={set('reminder_days')} placeholder="3,7,14" /></Field>
        </div>
        <SaveBar onSave={save} busy={busy} />
      </Section>
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
    <div className="stack loose">
      <Section title="Personal access tokens" flush>
        <table className="table"><thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th></th></tr></thead>
          <tbody>{data?.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="actions"><button type="button" className="btn ghost sm" onClick={async () => { await api.del(`${V1}/tokens/${t.id}`); reload() }}>Revoke</button></td></tr>)}
            <tr><td colSpan={3}><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. n8n, Home Assistant)" /></td><td className="actions"><button type="button" className="btn sm primary" onClick={create}>Create</button></td></tr></tbody></table>
        {created && <div className="callout" style={{ margin: 12 }}>Copy it now — it won't be shown again:<br /><code style={{ userSelect: 'all' }}>{created}</code> <button type="button" className="btn sm" onClick={() => { navigator.clipboard.writeText(created); toast('Copied', 'success') }}>Copy</button></div>}
      </Section>
      <Section title="Using the API">
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
      </Section>
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
    <div className="stack loose">
      <Section title="Outgoing webhooks" flush>
        <table className="table"><thead><tr><th>URL</th><th>Events</th><th>Enabled</th><th></th></tr></thead>
          <tbody>{data?.map(w => <tr key={w.id}><td className="mono small">{w.url}</td><td className="muted">{w.events}</td><td><input type="checkbox" checked={w.enabled} onChange={async e => { await api.put(`${V1}/webhooks/${w.id}`, { ...w, enabled: e.target.checked }); reload() }} /></td><td className="actions"><button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={async () => { await api.del(`${V1}/webhooks/${w.id}`); reload() }}><Icon name="trash" size={14} /></button></td></tr>)}
            <tr><td><input value={form.url} onChange={e => setForm({ ...form, url: e.target.value })} placeholder="https://n8n.local/webhook/…" /></td><td><input value={form.events} onChange={e => setForm({ ...form, events: e.target.value })} style={{ width: 150 }} /></td><td><input value={form.secret} onChange={e => setForm({ ...form, secret: e.target.value })} placeholder="secret" style={{ width: 100 }} /></td><td className="actions"><button type="button" className="btn sm primary" onClick={add} disabled={!form.url}>Add</button></td></tr></tbody></table>
        <div style={{ padding: 12 }}><button type="button" className="btn sm" onClick={async () => { await api.post(`${V1}/webhooks/test`); toast('Test event sent to all enabled webhooks') }}>Send test event</button></div>
      </Section>
      <Section title="Events">
        <p className="small">Subscribe with <code>*</code>, a comma list, or prefixes like <code>invoice.*</code>. Payloads are JSON <code>{'{event, timestamp, data}'}</code> and signed with <code>X-Webhook-Signature: sha256=HMAC(secret, body)</code>.</p>
        <div className="inline-list">{['invoice.created', 'invoice.updated', 'invoice.sent', 'invoice.viewed', 'invoice.paid', 'invoice.partial', 'invoice.overdue', 'invoice.cancelled', 'invoice.deleted', 'payment.created', 'client.created', 'client.updated', 'test'].map(e => <span key={e} className="badge">{e}</span>)}</div>
        <p className="muted small mt">Great for n8n, Home Assistant, ntfy, Gotify, or a Discord/Slack relay.</p>
      </Section>
    </div>
  )
}

// ---------- Users ----------
function Users() {
  const toast = useToast()
  const { user, status, refresh } = useApp()
  const isAdmin = user?.role === 'admin'
  const { data, reload } = useAsync(() => (isAdmin ? api.get<User[]>(`${V1}/users`) : Promise.resolve([] as User[])), [isAdmin])
  const [form, setForm] = useState({ email: '', name: '', password: '' })
  const [me, setMe] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const add = async () => { try { await api.post(`${V1}/users`, form); setForm({ email: '', name: '', password: '' }); reload(); toast('User created', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  const saveMe = async () => { try { await api.put(`${V1}/auth/me`, me); await refresh(); setMe({ ...me, current_password: '', new_password: '' }); toast('Profile updated', 'success') } catch (e) { toast((e as Error).message, 'error') } }
  return (
    <div className="stack loose">
      <Section title="Your profile">
        {status?.auth_mode !== 'local' && <div className="callout mb">Authentication is handled by <strong>{status?.auth_mode}</strong> mode (reverse proxy / disabled). Passwords are not used.</div>}
        <div className="grid" style={{ gap: 12 }}>
          <Field label="Name"><input value={me.name} onChange={e => setMe({ ...me, name: e.target.value })} /></Field>
          <Field label="Email"><input value={me.email} onChange={e => setMe({ ...me, email: e.target.value })} /></Field>
          {status?.auth_mode === 'local' && <><Field label="Current password"><input type="password" value={me.current_password} onChange={e => setMe({ ...me, current_password: e.target.value })} autoComplete="current-password" /></Field>
            <Field label="New password"><input type="password" value={me.new_password} onChange={e => setMe({ ...me, new_password: e.target.value })} autoComplete="new-password" /></Field></>}
        </div>
        <SaveBar onSave={saveMe} busy={false} />
      </Section>
      {isAdmin && <Section title="All users" flush>
        <table className="table"><thead><tr><th>Name</th><th>Email</th><th>Role</th><th></th></tr></thead>
          <tbody>{data?.map(u => <tr key={u.id}><td>{u.name}</td><td>{u.email}</td><td className="muted">{u.role}</td><td className="actions">{u.id !== user?.id && <button type="button" className="btn ghost icon-only sm" aria-label="Delete" title="Delete" onClick={async () => { if (confirm('Delete user?')) { await api.del(`${V1}/users/${u.id}`); reload() } }}><Icon name="trash" size={14} /></button>}</td></tr>)}
            <tr><td><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="Name" /></td><td><input value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} placeholder="email" /></td><td><input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} placeholder="password" /></td><td className="actions"><button type="button" className="btn sm primary" onClick={add} disabled={!form.email || form.password.length < 8}>Add</button></td></tr></tbody></table>
        <p className="muted small" style={{ margin: 12 }}>New people start as members with their own, empty business: their own clients, invoices, numbering, company profile and email settings. Admins only manage the server (users, currencies, backups); they cannot see anyone else's invoices.</p>
      </Section>}
    </div>
  )
}

// ---------- Backup ----------
function Backup() {
  const { user } = useApp()
  if (user?.role !== 'admin') {
    return (
      <div className="stack loose">
        <Section title="Export your data">
          <p>Download everything that belongs to you (clients, invoices, payments, expenses, templates and settings, without passwords or tokens) as JSON:</p>
          <div className="row mb"><a className="btn primary" href={`${V1}/export.json`}>Export JSON</a></div>
        </Section>
      </div>
    )
  }
  return (
    <div className="stack loose">
      <Section title="Download backup">
        <p>Everything lives in one SQLite file plus the <code>uploads/</code> folder (logo). Download a consistent snapshot any time:</p>
        <div className="row mb"><a className="btn primary" href={`${V1}/backup`}>Download database (.db)</a><a className="btn" href={`${V1}/export.json`}>Export JSON (your data)</a></div>
        <p className="muted small">The JSON export is human-readable and handy for migrations or your own scripts. The .db file is a byte-exact copy made with <code>VACUUM INTO</code>.</p>
      </Section>
      <Section title="Restore / automate">
        <pre>{`# restore: stop the container, replace the file, start again
docker compose stop
cp backup.db ./data/pocket-invoicing.db
docker compose start

# scripted backups from the host (cron)
docker compose exec app pocket-invoicing backup -o /data/backups/nightly.db

# or via API
curl -H "Authorization: Bearer pi_…" -o backup.db \\
  ${window.location.origin}/api/v1/backup`}</pre>
      </Section>
    </div>
  )
}

// ---------- System ----------
function System() {
  const { data } = useAsync(() => api.get<Record<string, string | number | boolean>>(`${V1}/settings/system`))
  const toast = useToast()
  if (!data) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Runtime">
        <dl className="kv">{Object.entries(data).map(([k, v]) => <div key={k} style={{ display: 'contents' }}><dt>{k}</dt><dd className="mono">{String(v)}</dd></div>)}</dl>
      </Section>
      <Section title="Maintenance">
        <p className="small">The scheduler generates recurring invoices, marks overdue invoices, sends reminders and refreshes exchange rates. It runs automatically every <code>SCHEDULER_INTERVAL</code>; you can trigger it now:</p>
        <button type="button" className="btn" onClick={async () => { const r = await api.post<Record<string, number>>(`${V1}/scheduler/run`); toast(`Scheduler: ${Object.entries(r).map(([k, v]) => `${k}=${v}`).join(', ')}`, 'success') }}>Run scheduler now</button>
        <p className="muted small mt">Health: <code>/healthz</code> · readiness: <code>/readyz</code> · metrics: <code>/metrics</code></p>
      </Section>
    </div>
  )
}

// ---------- Pocket Documents ----------
const connBadge = (mode: ConnMode, via?: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge accent">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge partial">not connected</span>

/** Pocket Core | Off for one sibling app, with Direct as an advanced option
 * when the box has a Pocket Core (always shown while it is the current mode, and
 * shown normally without a Core); the switch saves at once. */
function Connection({ app, mode, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via?: string; detail?: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(false)
  const showDirect = !core || mode === 'direct' || advanced
  const ok = !!via
  const line = mode === 'off' ? `Off: Pocket Invoicing does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through a Pocket Core app link, as you. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} API token; use Test connection to check it.` : `Not connected: ${detail || 'add an API token'}.`)
  return (
    <div className="conn-block">
      <div className="segmented" role="radiogroup">
        {([['core', 'Pocket Core', core ? 'Through Pocket Core: acts as you there, nothing to paste' : 'There is no Pocket Core on this box'], ['direct', 'Direct', 'A URL and your own API token'], ['off', 'Off', '']] as const).filter(([v]) => v !== 'direct' || showDirect).map(([v, label, title]) =>
          <button type="button" key={v} role="radio" aria-checked={mode === v} title={title} className={mode === v ? 'active' : ''} onClick={() => onMode(v)}>{label}</button>)}
      </div>
      {!showDirect && <div><button type="button" className="link-btn small" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and token</button></div>}
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row">{test}{hasToken && <button type="button" className="btn ghost sm danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row">{test}</div>}
    </div>
  )
}

function PocketDocuments() {
  const { s, set, put, save, busy, setS } = useSettingsForm()
  const { refresh } = useApp()
  const toast = useToast()
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; document_count?: number; via?: string; url?: string } | null>(null)
  if (!s) return <Loading />
  const configured = !!s.documents_via
  const typed = s.documents_mode === 'direct' && !!s.documents_token
  const test = async () => {
    setTesting(true); setResult(null)
    try {
      const r = await api.post<{ ok: boolean; document_count: number; via: string; url: string }>(`${V1}/settings/test-documents`, typed ? { url: s.documents_url, token: s.documents_token } : {})
      setResult(r)
      toast(`Connected to Pocket Documents (${r.document_count} documents)${r.via === 'core' ? ' through Pocket Core' : ''}`, 'success')
    } catch (e) { toast((e as Error).message, 'error') } finally { setTesting(false) }
  }
  // the switch and Disconnect are saved right away, like in the other Pocket apps
  const saveNow = async (patch: Partial<S>, msg: string) => {
    try { const r = await api.put<S>(`${V1}/settings`, { ...s, ...patch }); setS(r); setResult(null); await refresh(); toast(msg, 'success') } catch (e) { toast((e as Error).message, 'error') }
  }
  return (
    <div className="stack loose">
      <Section title="Connection" actions={connBadge(s.documents_mode, s.documents_via)}>
        <p className="muted small">Optional. Connected, issued invoices and company documents are filed in <b>your</b> Pocket Documents, under the client as correspondent; each client page lists that correspondent's documents and Documents gets a tab to search your archive. With Pocket Core on the box nothing needs to be pasted; without it, use the Pocket Documents URL and an API token of yours (Pocket Documents → Settings → API tokens).</p>
        <Connection app="Pocket Documents" mode={s.documents_mode} via={s.documents_via} detail={s.documents_detail} core={!!s.core_configured}
          hasToken={!!s.documents_token_set || !!s.documents_url}
          onMode={m => saveNow({ documents_mode: m }, m === 'off' ? 'Pocket Documents switched off' : m === 'core' ? 'Pocket Documents through Pocket Core' : 'Direct Pocket Documents connection')}
          onDisconnect={() => saveNow({ documents_url: '', documents_public_url: '', documents_token: '', documents_clear_token: true }, 'Pocket Documents URL and token removed')}
          test={<><button type="button" className="btn" disabled={testing} onClick={test}>{testing ? 'Testing…' : 'Test connection'}</button>
            {result && <span className="muted small">{result.document_count} documents · {result.url}</span>}</>}
          direct={<div className="form-grid">
            <Field label="Pocket Documents URL" help="As reached from this container, e.g. http://pocket-documents:8080; empty = ask Pocket Core or DOCUMENTS_URL"><input value={s.documents_url} onChange={set('documents_url')} placeholder="http://pocket-documents:8080" /></Field>
            <Field label="Browser URL (optional)" help="Used for links you click, when different from the URL above"><input value={s.documents_public_url} onChange={set('documents_public_url')} placeholder="https://documents.example.org" /></Field>
            <Field label="API token" help={s.documents_token_set ? 'A token is stored. Leave blank to keep it; Save changes below to store a new one.' : 'Pocket Documents → Settings → API tokens (pd_…); Save changes below to store it'}><input type="password" value={s.documents_token} onChange={set('documents_token')} autoComplete="new-password" placeholder={s.documents_token_set ? '••••••••' : 'pd_…'} /></Field>
          </div>} />
      </Section>
      <Section title="What gets filed">
        <div className="form-grid">
          <Field label="File invoices automatically" help="Uses the same files as email attachments (Settings → Invoicing → Email attachment): generated PDF, your uploaded files, or both">
            <Select value={s.documents_archive_invoices} onChange={put('documents_archive_invoices')} options={[{ value: 'off', label: 'Never', hint: 'manual button only' }, { value: 'sent', label: 'When marked as sent', hint: 'or paid' }, { value: 'paid', label: 'When paid' }]} />
          </Field>
          <Field label="Document type for invoices" help="Created in Pocket Documents if missing"><input value={s.documents_invoice_type} onChange={set('documents_invoice_type')} placeholder="Invoice" /></Field>
          <Field label="Tags" help="Comma separated; added to everything filed from here (created if missing)"><input value={s.documents_tags} onChange={set('documents_tags')} placeholder="pocket-invoicing, invoices" /></Field>
        </div>
        <div className="mt">
          <SettingRow title="File under the client's name" help="When the client has no correspondent; created if missing"><Switch checked={s.documents_create_correspondents} onChange={put('documents_create_correspondents')} label="File under the client's name" /></SettingRow>
          <SettingRow title="Category as document type" help="Company documents use their category as the document type"><Switch checked={s.documents_category_as_type} onChange={put('documents_category_as_type')} label="Use the category as document type" /></SettingRow>
        </div>
      </Section>
      {configured && <Section title="Clients ↔ correspondents">
        <p className="muted small">Each client can be linked to a correspondent in Pocket Documents (Clients → edit), by name. The client page then shows that correspondent's documents, and everything filed for the client goes under it. This matches unlinked clients to existing correspondents by name; optionally creates the missing ones.</p>
        <SyncCorrespondents />
      </Section>}
      {configured && <Section title="Where it shows up">
        <p className="muted small" style={{ marginTop: 0 }}>Each invoice page has a "File in Documents" button and each uploaded file a "To Documents" button; Company documents have "To Documents" and the Documents page a Pocket Documents tab to search and copy documents back. Filing the same record again finds the first copy (it carries the id invoicing:&lt;kind&gt;:&lt;id&gt;), and links are stored here, so deleting something here never deletes it there.</p>
      </Section>}
      <SaveBar onSave={save} busy={busy} />
    </div>
  )
}

function SyncCorrespondents() {
  const toast = useToast()
  const [create, setCreate] = useState(true)
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<{ matched: string[]; created: string[]; unmatched: string[] } | null>(null)
  const run = async () => {
    setBusy(true)
    try { const r = await api.post<{ matched: string[]; created: string[]; unmatched: string[] }>(`${V1}/pocket-documents/correspondents/sync`, { create }); setRes(r); toast(`${r.matched.length} matched, ${r.created.length} created, ${r.unmatched.length} left`, 'success') } catch (e) { toast((e as Error).message, 'error') } finally { setBusy(false) }
  }
  return <>
    <div className="row" style={{ gap: 14, flexWrap: 'wrap' }}>
      <button type="button" className="btn" disabled={busy} onClick={run}>{busy ? 'Linking…' : 'Link clients to correspondents'}</button>
      <label className="check"><input type="checkbox" checked={create} onChange={e => setCreate(e.target.checked)} /> Create missing correspondents in Pocket Documents</label>
    </div>
    {res && <div className="muted small" style={{ marginTop: 10, whiteSpace: 'pre-wrap' }}>
      {res.matched.length > 0 && <>Matched: {res.matched.join('; ')}{'\n'}</>}
      {res.created.length > 0 && <>Created: {res.created.join(', ')}{'\n'}</>}
      {res.unmatched.length > 0 && <>Not linked: {res.unmatched.join(', ')}</>}
      {!res.matched.length && !res.created.length && !res.unmatched.length && 'All clients were already linked.'}
    </div>}
  </>
}
