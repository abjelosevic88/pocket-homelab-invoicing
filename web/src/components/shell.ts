// Small pieces the shell and the pages share: change/crumb events and the running timer.
import { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { TimeEntry } from '../lib/types'

/** Something was created or changed from the shell (a client, an invoice): lists reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pi:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pi:changed', h)
    return () => window.removeEventListener('pi:changed', h)
  }, [])
}

/** A page names the record on screen (an invoice number, a client) for the top bar's breadcrumb. */
export function useCrumb(label: string | undefined) {
  useEffect(() => {
    window.dispatchEvent(new CustomEvent('pi:crumb', { detail: label || '' }))
  }, [label])
}

/** The running timer: here, or the person's one timer in Pocket Time when it is connected
 * (wherever it was started). One poll for the whole page: every 20 s while visible, on focus,
 * and when something starts or stops it (the `pi:timer` event). */
export interface TimerState { running: TimeEntry | null; shared: boolean; time_error?: string }
let timerState: TimerState = { running: null, shared: false }
const timerSubs = new Set<(s: TimerState) => void>()
let timerPoll: ReturnType<typeof setInterval> | undefined
const loadTimer = () => api.get<TimerState>(`${V1}/time/running`).then(r => { timerState = { running: r.running || null, shared: !!r.shared, time_error: r.time_error }; timerSubs.forEach(f => f(timerState)) }).catch(() => {})
const onVisible = () => { if (document.visibilityState === 'visible') loadTimer() }
export function useTimer() {
  const [state, setState] = useState<TimerState>(timerState)
  useEffect(() => {
    timerSubs.add(setState)
    if (timerSubs.size === 1) {
      loadTimer()
      timerPoll = setInterval(() => { if (document.visibilityState === 'visible') loadTimer() }, 20000)
      window.addEventListener('pi:timer', loadTimer)
      window.addEventListener('focus', loadTimer)
      document.addEventListener('visibilitychange', onVisible)
    } else setState(timerState)
    return () => {
      timerSubs.delete(setState)
      if (timerSubs.size === 0) {
        clearInterval(timerPoll)
        window.removeEventListener('pi:timer', loadTimer)
        window.removeEventListener('focus', loadTimer)
        document.removeEventListener('visibilitychange', onVisible)
      }
    }
  }, [])
  return state
}
export const useRunningTimer = () => useTimer().running
/** Ask every timer display to look again (after starting or stopping one). */
export const timerChanged = () => window.dispatchEvent(new Event('pi:timer'))
/** What a running timer is called in one line. */
export const timerLabel = (t: TimeEntry) => t.ours === false ? [t.description, t.started_in && `from ${t.started_in}`].filter(Boolean).join(' · ') : [t.client_name, t.description || t.project].filter(Boolean).join(' · ')

export function elapsed(since: string, now: number) {
  const secs = Math.max(0, Math.floor((now - new Date(since).getTime()) / 1000))
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60), s = secs % 60
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

export type CreateKind = 'invoice' | 'client'

