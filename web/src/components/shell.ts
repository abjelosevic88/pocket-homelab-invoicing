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

/** The running timer, polled every 30 s and refreshed when the time page starts or stops it. */
export function useRunningTimer() {
  const [running, setRunning] = useState<TimeEntry | null>(null)
  useEffect(() => {
    const load = () => api.get<{ running: TimeEntry | null }>(`${V1}/time/running`).then(r => setRunning(r.running)).catch(() => {})
    load()
    const i = setInterval(load, 30000)
    window.addEventListener('pi:timer', load)
    return () => { clearInterval(i); window.removeEventListener('pi:timer', load) }
  }, [])
  return running
}

export function elapsed(since: string, now: number) {
  const secs = Math.max(0, Math.floor((now - new Date(since).getTime()) / 1000))
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60), s = secs % 60
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

export type CreateKind = 'invoice' | 'client'

