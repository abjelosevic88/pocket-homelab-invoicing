import type { Currency } from './types'

const currencyCache = new Map<string, Currency>()
export function registerCurrencies(list: Currency[]) {
  for (const c of list) currencyCache.set(c.code, c)
}

const LOCALE_FOR_FORMAT: Record<string, { locale: string; grouping: boolean }> = {
  '1,234.56': { locale: 'en-US', grouping: true },
  '1.234,56': { locale: 'de-DE', grouping: true },
  '1 234,56': { locale: 'fr-FR', grouping: true },
  "1'234.56": { locale: 'de-CH', grouping: true },
  '1234.56': { locale: 'en-US', grouping: false },
}
let numberStyle = LOCALE_FOR_FORMAT['1,234.56']
export function registerNumberFormat(fmt: string | undefined) {
  numberStyle = LOCALE_FOR_FORMAT[fmt || ''] || LOCALE_FOR_FORMAT['1,234.56']
}

export function money(amount: number | undefined | null, code: string, opts: { code?: boolean } = {}): string {
  const v = amount ?? 0
  const c = currencyCache.get(code)
  const decimals = c?.decimals ?? 2
  const num = new Intl.NumberFormat(numberStyle.locale, { minimumFractionDigits: decimals, maximumFractionDigits: decimals, useGrouping: numberStyle.grouping }).format(Math.abs(v))
  const sign = v < 0 ? '-' : ''
  if (!c || opts.code) return `${sign}${num} ${code}`
  const sym = c.symbol
  if (sym.endsWith(' ')) return `${sign}${num} ${sym.trim()}`
  return `${sign}${sym}${num}`
}

export function fmtDate(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s.length === 10 ? s + 'T00:00:00' : s)
  if (isNaN(d.getTime())) return s
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

export function fmtDateTime(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  return d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function today(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function addDays(date: string, days: number): string {
  const d = new Date(date + 'T00:00:00')
  d.setDate(d.getDate() + days)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function hours(minutes: number): string {
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return `${h}h ${String(m).padStart(2, '0')}m`
}

export function hoursDecimal(minutes: number): string {
  return (minutes / 60).toFixed(2)
}

export function num(v: number, max = 2): string {
  return new Intl.NumberFormat(numberStyle.locale, { maximumFractionDigits: max, useGrouping: numberStyle.grouping }).format(v)
}

export function fileSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function statusLabel(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export const UNITS = [
  { value: 'hour', label: 'Hour' },
  { value: 'day', label: 'Day' },
  { value: 'month', label: 'Month' },
  { value: 'unit', label: 'Unit' },
  { value: 'fixed', label: 'Fixed' },
]

export const BILLING_MODES = [
  { value: 'hourly', label: 'Hourly' },
  { value: 'daily', label: 'Daily' },
  { value: 'monthly', label: 'Monthly retainer' },
  { value: 'fixed', label: 'Fixed price' },
]

export const PAYMENT_METHODS = ['bank_transfer', 'card', 'paypal', 'cash', 'crypto', 'stripe', 'other']

export function monthLabel(ym: string): string {
  const [y, m] = ym.split('-').map(Number)
  return new Date(y, m - 1, 1).toLocaleDateString(undefined, { month: 'short', year: '2-digit' })
}

export function unitForBilling(mode: string): string {
  switch (mode) {
    case 'hourly': return 'hour'
    case 'daily': return 'day'
    case 'monthly': return 'month'
    default: return 'unit'
  }
}
