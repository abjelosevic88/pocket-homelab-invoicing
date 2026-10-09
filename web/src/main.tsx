import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Clients from './pages/Clients'
import ClientDetail from './pages/ClientDetail'
import Invoices from './pages/Invoices'
import InvoiceEditor from './pages/InvoiceEditor'
import InvoiceDetail from './pages/InvoiceDetail'
import Recurring from './pages/Recurring'
import TimeTracking from './pages/TimeTracking'
import Expenses from './pages/Expenses'
import Documents from './pages/Documents'
import Payments from './pages/Payments'
import Reports from './pages/Reports'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="login-wrap"><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route element={<Gate><Layout /></Gate>}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/clients" element={<Clients />} />
        <Route path="/clients/:id" element={<ClientDetail />} />
        <Route path="/invoices" element={<Invoices />} />
        <Route path="/invoices/new" element={<InvoiceEditor />} />
        <Route path="/invoices/:id" element={<InvoiceDetail />} />
        <Route path="/invoices/:id/edit" element={<InvoiceEditor />} />
        <Route path="/recurring" element={<Recurring />} />
        <Route path="/time" element={<TimeTracking />} />
        <Route path="/expenses" element={<Expenses />} />
        <Route path="/documents" element={<Documents />} />
        <Route path="/payments" element={<Payments />} />
        <Route path="/reports" element={<Reports />} />
        <Route path="/settings/*" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ToastProvider>
        <AppProvider>
          <App />
        </AppProvider>
      </ToastProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
