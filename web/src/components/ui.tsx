import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'

// ---------- Toasts ----------
type Toast = { id: number; msg: string; kind: 'info' | 'error' | 'success' }
const ToastCtx = createContext<(msg: string, kind?: Toast['kind']) => void>(() => {})
export function useToast() { return useContext(ToastCtx) }
export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const push = useCallback((msg: string, kind: Toast['kind'] = 'info') => {
    const id = Date.now() + Math.random()
    setToasts(t => [...t, { id, msg, kind }])
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), kind === 'error' ? 6000 : 3500)
  }, [])
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div className="toasts">{toasts.map(t => <div key={t.id} className={`toast ${t.kind}`}>{t.msg}</div>)}</div>
    </ToastCtx.Provider>
  )
}

// ---------- Basic building blocks ----------
export function PageHeader({ title, sub, actions }: { title: React.ReactNode; sub?: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="page-header">
      <div><h1>{title}</h1>{sub && <div className="sub">{sub}</div>}</div>
      {actions && <div className="actions">{actions}</div>}
    </div>
  )
}

export function Card({ title, actions, children, flush, className = '' }: { title?: React.ReactNode; actions?: React.ReactNode; children: React.ReactNode; flush?: boolean; className?: string }) {
  return (
    <div className={`card ${className}`}>
      {(title || actions) && <div className="card-head"><h2>{title}</h2>{actions}</div>}
      <div className={`card-body ${flush ? 'flush' : ''}`}>{children}</div>
    </div>
  )
}

export function Field({ label, help, children, className = '' }: { label: React.ReactNode; help?: React.ReactNode; children: React.ReactNode; className?: string }) {
  return <label className={`field ${className}`}><span>{label}</span>{children}{help && <span className="help">{help}</span>}</label>
}

export function Badge({ status, children }: { status: string; children?: React.ReactNode }) {
  return <span className={`badge ${status}`}>{children ?? status.charAt(0).toUpperCase() + status.slice(1)}</span>
}

export function Loading() { return <div className="loading"><span className="spinner" /> Loading…</div> }

export function Empty({ title, children }: { title: string; children?: React.ReactNode }) {
  return <div className="empty"><h3>{title}</h3>{children}</div>
}

export function Modal({ title, onClose, children, size = '' }: { title: React.ReactNode; onClose: () => void; children: React.ReactNode; size?: '' | 'lg' | 'xl' }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <div className="modal-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className={`modal ${size}`} role="dialog" aria-modal="true">
        <div className="modal-head"><h2>{title}</h2><button className="btn ghost icon" onClick={onClose} aria-label="Close">✕</button></div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  )
}

export function Confirm({ title, message, onConfirm, onCancel, danger = true }: { title: string; message: React.ReactNode; onConfirm: () => void; onCancel: () => void; danger?: boolean }) {
  return (
    <Modal title={title} onClose={onCancel}>
      <p>{message}</p>
      <div className="form-actions">
        <button className="btn" onClick={onCancel}>Cancel</button>
        <button className={`btn ${danger ? 'danger' : 'primary'}`} onClick={onConfirm}>Confirm</button>
      </div>
    </Modal>
  )
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: { id: T; label: string }[]; value: T; onChange: (t: T) => void }) {
  return <div className="tabs">{tabs.map(t => <button key={t.id} className={t.id === value ? 'active' : ''} onClick={() => onChange(t.id)}>{t.label}</button>)}</div>
}

// ---------- hooks ----------
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let alive = true
    setLoading(true)
    fn().then(d => { if (alive) { setData(d); setError(null) } }).catch(e => { if (alive) setError(e.message) }).finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])
  return { data, error, loading, reload: () => setTick(t => t + 1), setData }
}

export function useDebounce<T>(value: T, ms = 300): T {
  const [v, setV] = useState(value)
  useEffect(() => { const t = setTimeout(() => setV(value), ms); return () => clearTimeout(t) }, [value, ms])
  return v
}

export function numberInput(v: string): number {
  const n = parseFloat(v.replace(',', '.'))
  return isNaN(n) ? 0 : n
}

/** Click-or-drop file picker. Dropped files are appended to the current selection. */
export function DropZone({ files, onFiles, multiple = true, accept, hint }: { files: File[]; onFiles: (files: File[]) => void; multiple?: boolean; accept?: string; hint?: React.ReactNode }) {
  const [over, setOver] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const add = (list: FileList | File[] | null) => {
    const incoming = Array.from(list || [])
    if (!incoming.length) return
    onFiles(multiple ? [...files, ...incoming.filter(n => !files.some(f => f.name === n.name && f.size === n.size))] : incoming.slice(0, 1))
  }
  return (
    <div
      className={`dropzone ${over ? 'over' : ''}`}
      onClick={() => inputRef.current?.click()}
      onDragOver={e => { e.preventDefault(); e.stopPropagation(); setOver(true) }}
      onDragEnter={e => { e.preventDefault(); setOver(true) }}
      onDragLeave={e => { e.preventDefault(); setOver(false) }}
      onDrop={e => { e.preventDefault(); e.stopPropagation(); setOver(false); add(e.dataTransfer.files) }}
      role="button" tabIndex={0}
      onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); inputRef.current?.click() } }}
    >
      <input ref={inputRef} type="file" multiple={multiple} accept={accept} style={{ display: 'none' }} onChange={e => { add(e.target.files); e.target.value = '' }} />
      {files.length === 0 ? (
        <div className="dz-empty"><div className="dz-icon">⇪</div><div><b>Drop files here</b> or click to choose</div>{hint && <div className="muted small">{hint}</div>}</div>
      ) : (
        <ul className="dz-list" onClick={e => e.stopPropagation()}>
          {files.map((f, i) => <li key={f.name + f.size}><span className="dz-name">{f.name}</span><span className="muted small">{f.size >= 1048576 ? `${(f.size / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(f.size / 1024))} KB`}</span><button type="button" className="btn ghost sm" title="Remove" onClick={() => onFiles(files.filter((_, j) => j !== i))}>✕</button></li>)}
          {multiple && <li className="dz-more"><button type="button" className="link-btn" onClick={() => inputRef.current?.click()}>+ Add more</button><span className="muted small"> or drop them here</span></li>}
        </ul>
      )}
    </div>
  )
}
