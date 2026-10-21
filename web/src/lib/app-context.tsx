import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import { registerCurrencies, registerNumberFormat } from './format'
import type { Currency, Settings, User } from './types'

interface AuthStatus { needs_setup: boolean; setup_complete: boolean; auth_mode: string; authenticated: boolean; user: User | null; version: string; company_name: string; oidc?: boolean; oidc_name?: string }

export type ThemeMode = 'system' | 'light' | 'dark'

interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  currencies: Currency[]
  themeMode: ThemeMode
  theme: 'light' | 'dark'
  setThemeMode: (m: ThemeMode) => void
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
  // The theme follows the system unless set to light or dark (the old 'pi-theme' key was
  // written on every visit, so it can't tell a choice from the default and is dropped).
  const [themeMode, setThemeModeRaw] = useState<ThemeMode>(() => { localStorage.removeItem('pi-theme'); return (localStorage.getItem('pi-theme-mode') as ThemeMode) || 'system' })
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => setSystemDark(mq.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])
  const theme: 'light' | 'dark' = themeMode === 'system' ? (systemDark ? 'dark' : 'light') : themeMode
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.querySelectorAll('meta[name="theme-color"]').forEach(m => m.setAttribute('content', theme === 'dark' ? '#111113' : '#ffffff'))
  }, [theme])
  const setThemeMode = useCallback((m: ThemeMode) => { setThemeModeRaw(m); if (m === 'system') localStorage.removeItem('pi-theme-mode'); else localStorage.setItem('pi-theme-mode', m) }, [])
  useEffect(() => { document.title = settings?.brand_name?.trim() || 'Pocket Invoicing' }, [settings?.brand_name])

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

  return <Ctx.Provider value={{ status, user, settings, currencies, themeMode, theme, setThemeMode, toggleTheme: () => setThemeMode(theme === 'dark' ? 'light' : 'dark'), refresh, setUser }}>{children}</Ctx.Provider>
}
