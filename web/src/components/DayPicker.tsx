import { fmtDate } from '../lib/format'

/** Calendar of one or more months where days are simply on or off. */

const pad = (n: number) => String(n).padStart(2, '0')
export const iso = (y: number, m: number, d: number) => `${y}-${pad(m)}-${pad(d)}`

/** Months (YYYY-MM) covered by [from, to], at most `max`. */
export function monthsBetween(from: string, to: string, max = 3): string[] {
  const out: string[] = []
  let [y, m] = from.slice(0, 7).split('-').map(Number)
  const end = to.slice(0, 7)
  while (out.length < max) {
    const ym = `${y}-${pad(m)}`
    out.push(ym)
    if (ym >= end) break
    m++; if (m > 12) { m = 1; y++ }
  }
  return out
}

/** Every work-week day inside [from, to] that is not a holiday. */
export function defaultDays(from: string, to: string, workWeek: string, holidays: string[]): string[] {
  const week = new Set((workWeek || '1,2,3,4,5').split(',').map(Number))
  const hol = new Set(holidays)
  const out: string[] = []
  const d = new Date(from + 'T00:00:00')
  const end = new Date(to + 'T00:00:00')
  for (; d <= end; d.setDate(d.getDate() + 1)) {
    const key = iso(d.getFullYear(), d.getMonth() + 1, d.getDate())
    const dow = d.getDay() || 7
    if (week.has(dow) && !hol.has(key)) out.push(key)
  }
  return out
}

/** ISO-8601 week number. */
function isoWeek(date: Date): number {
  const d = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()))
  const day = d.getUTCDay() || 7
  d.setUTCDate(d.getUTCDate() + 4 - day)
  const yearStart = new Date(Date.UTC(d.getUTCFullYear(), 0, 1))
  return Math.ceil(((d.getTime() - yearStart.getTime()) / 86400000 + 1) / 7)
}

/**
 * `defaults` are the working days of the range: a week-number click turns the whole
 * week off when any of its days is on, otherwise it turns the week's working days on.
 */
export function DayPicker({ from, to, value, holidays = {}, defaults, onChange }: { from: string; to: string; value: string[]; holidays?: Record<string, string>; defaults?: string[]; onChange: (v: string[]) => void }) {
  const sel = new Set(value)
  const inRange = (key: string) => key >= from && key <= to
  const def = new Set(defaults || [])
  const toggle = (keys: string[]) => {
    const next = new Set(sel)
    if (keys.length === 1) {
      if (next.has(keys[0])) next.delete(keys[0]); else next.add(keys[0])
    } else {
      const anyOn = keys.some(k => next.has(k))
      const work = keys.filter(k => def.has(k))
      for (const k of keys) next.delete(k)
      if (!anyOn) for (const k of (work.length ? work : keys)) next.add(k)
    }
    onChange(Array.from(next).sort())
  }
  const months = monthsBetween(from, to)
  return (
    <div className="row wrap" style={{ gap: 20, alignItems: 'flex-start' }}>
      {months.map(ym => {
        const [y, m] = ym.split('-').map(Number)
        const last = new Date(y, m, 0).getDate()
        const offset = (new Date(y, m - 1, 1).getDay() || 7) - 1
        const weeks: { wk: number; days: (number | null)[] }[] = []
        let cur: (number | null)[] = Array(offset).fill(null)
        for (let d = 1; d <= last; d++) {
          cur.push(d)
          if (cur.length === 7) { weeks.push({ wk: isoWeek(new Date(y, m - 1, d)), days: cur }); cur = [] }
        }
        if (cur.length) { while (cur.length < 7) cur.push(null); weeks.push({ wk: isoWeek(new Date(y, m - 1, last)), days: cur }) }
        return (
          <div key={ym}>
            {months.length > 1 && <div className="small bold" style={{ marginBottom: 4 }}>{new Date(y, m - 1, 1).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</div>}
            <div className="daypick">
              <div className="dow wk">wk</div>{['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'].map(d => <div key={d} className="dow">{d}</div>)}
              {weeks.map(w => {
                const keys = w.days.filter((d): d is number => d !== null).map(d => iso(y, m, d)).filter(inRange)
                return [
                  <button key={`w${w.wk}`} type="button" className={`wk${keys.length && keys.some(k => sel.has(k)) ? ' on' : ''}`} disabled={!keys.length} title={`Toggle week ${w.wk}`} onClick={() => toggle(keys)}>{w.wk}</button>,
                  ...w.days.map((d, i) => {
                    if (d === null) return <button key={`b${i}`} type="button" className="blank" tabIndex={-1} />
                    const key = iso(y, m, d)
                    const hol = holidays[key]
                    const out = !inRange(key)
                    return <button key={key} type="button" disabled={out} className={`${sel.has(key) ? 'on' : ''}${hol ? ' hol' : ''}${out ? ' out' : ''}`} title={`${fmtDate(key)}${hol ? ` · ${hol}` : ''}`} onClick={() => toggle([key])}>{d}</button>
                  }),
                ]
              })}
            </div>
          </div>
        )
      })}
    </div>
  )
}
