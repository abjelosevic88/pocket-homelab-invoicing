import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import { registerCurrencies, registerNumberFormat } from './format'
import type { Currency, Settings, User } from './types'

interface AuthStatus { needs_setup: boolean; setup_complete: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; company_name: string }

interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  currencies: Currency[]
  theme: 'light' | 'dark'
  toggleTheme: () => void
  refresh: () => Promise<void>
  setUser: (u: User | null) => void
}

const Ctx = createContext<AppState>(null!)
export function useApp() { return useContext(Ctx) }

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [currencies, setCurrencies] = useState<Currency[]>([])
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (localStorage.getItem('pi-theme') as 'light' | 'dark') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'))

  useEffect(() => { document.documentElement.dataset.theme = theme; localStorage.setItem('pi-theme', theme) }, [theme])

  const refresh = useCallback(async () => {
    const st = await api.get<AuthStatus>(`${V1}/auth/status`)
    setStatus(st)
    setUser(st.user)
    if (st.authenticated) {
      const [s, c] = await Promise.all([api.get<Settings>(`${V1}/settings`), api.get<Currency[]>(`${V1}/currencies`)])
      setSettings(s)
      registerNumberFormat(s.number_format)
      setCurrencies(c)
      registerCurrencies(c)
    }
  }, [])

  useEffect(() => { refresh().catch(() => setStatus({ needs_setup: false, setup_complete: false, auth_mode: 'local', authenticated: false, user: null, version: '', company_name: '' })) }, [refresh])
  useEffect(() => {
    const h = () => { setUser(null); setStatus(s => s ? { ...s, authenticated: false, user: null } : s) }
    window.addEventListener('pi:unauthorized', h)
    return () => window.removeEventListener('pi:unauthorized', h)
  }, [])

  return <Ctx.Provider value={{ status, user, settings, currencies, theme, toggleTheme: () => setTheme(t => t === 'dark' ? 'light' : 'dark'), refresh, setUser }}>{children}</Ctx.Provider>
}
