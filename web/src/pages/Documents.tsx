import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { fileSize, fmtDate, today } from '../lib/format'
import type { Client, Document, PaperlessDoc, PaperlessLink } from '../lib/types'
import { Card, Confirm, DropZone, Empty, Field, Loading, Modal, PageHeader, Tabs, useAsync, useDebounce, useToast } from '../components/ui'
import { PaperlessBadge, PaperlessOffNote, usePaperless } from '../components/paperless'

type Meta = { categories: Record<string, number>; expiring: Document[] }

function expiryState(d: Document): 'expired' | 'soon' | '' {
  if (!d.expires_at) return ''
  const days = (new Date(d.expires_at).getTime() - Date.now()) / 86400000
  if (days < 0) return 'expired'
  if (days <= 30) return 'soon'
  return ''
}

export function DocForm({ initial, categories, clients, onSaved, onClose, paperlessOn, defaultClientId = 0 }: { initial?: Document; categories: string[]; clients: Client[]; onSaved: () => void; onClose: () => void; paperlessOn: boolean; defaultClientId?: number }) {
  const toast = useToast()
  const [files, setFiles] = useState<File[]>([])
  const [f, setF] = useState({ title: initial?.title || '', category: initial?.category || '', client_id: initial?.client_id || defaultClientId || 0, doc_date: initial?.doc_date || (initial ? '' : today()), expires_at: initial?.expires_at || '', notes: initial?.notes || '' })
  const [sendPaperless, setSendPaperless] = useState(false)
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>) => setF(x => ({ ...x, [k]: k === 'client_id' ? Number(e.target.value) : e.target.value }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!initial && files.length === 0) { toast('Choose at least one file', 'error'); return }
    setBusy(true)
    try {
      if (initial) {
        await api.put(`${V1}/documents/${initial.id}`, { ...f, client_id: f.client_id || null })
        toast('Document updated', 'success')
      } else {
        const fd = new FormData()
        files.forEach(x => fd.append('file', x))
        Object.entries(f).forEach(([k, v]) => fd.append(k, String(v ?? '')))
        if (sendPaperless) fd.append('paperless', '1')
        await api.post(`${V1}/documents`, fd)
        toast(files.length > 1 ? `${files.length} documents uploaded` : 'Document uploaded', 'success')
      }
      onSaved()
    } catch (err) { toast((err as Error).message, 'error') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={submit}>
      {!initial && <Field label="Files">
        <DropZone files={files} hint="Any type, up to 50 MB each. With several files the title is taken from each file name." onFiles={fs => { setFiles(fs); if (fs.length === 1 && !f.title) setF(x => ({ ...x, title: fs[0].name.replace(/\.[^.]+$/, '').replace(/[_\-.]+/g, ' ') })) }} />
      </Field>}
      <div className="form-grid">
        <Field label="Title" className="full"><input value={f.title} onChange={set('title')} placeholder={files.length > 1 ? 'From file names' : 'e.g. Rješenje o registraciji'} disabled={files.length > 1} /></Field>
        <Field label="Category"><input list="doc-categories" value={f.category} onChange={set('category')} placeholder="Contracts, Tax, Bank…" /><datalist id="doc-categories">{categories.map(c => <option key={c} value={c} />)}</datalist></Field>
        <Field label="Client (optional)"><select value={f.client_id} onChange={set('client_id')}><option value={0}>— none —</option>{clients.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field>
        <Field label="Document date"><input type="date" value={f.doc_date} onChange={set('doc_date')} /></Field>
        <Field label="Expires (optional)" help="Shown with a warning 30 days before"><input type="date" value={f.expires_at} onChange={set('expires_at')} /></Field>
        <Field label="Notes" className="full"><textarea rows={3} value={f.notes} onChange={set('notes')} /></Field>
        {!initial && paperlessOn && <label className="check full"><input type="checkbox" checked={sendPaperless} onChange={e => setSendPaperless(e.target.checked)} /> Also send to Paperless-ngx</label>}
      </div>
      <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{initial ? 'Save' : 'Upload'}</button></div>
    </form>
  )
}

function LocalDocuments({ paperlessOn }: { paperlessOn: boolean }) {
  const toast = useToast()
  const { data: clientList } = useAsync(() => api.get<Client[]>(`${V1}/clients`), [])
  const clients = clientList || []
  const [q, setQ] = useState('')
  const [cat, setCat] = useState('')
  const [params] = useSearchParams()
  const [clientId, setClientId] = useState(Number(params.get('client_id')) || 0)
  const dq = useDebounce(q)
  const meta = useAsync(() => api.get<Meta>(`${V1}/documents/categories`), [])
  const list = useAsync(() => api.get<Document[]>(`${V1}/documents?q=${encodeURIComponent(dq)}&category=${encodeURIComponent(cat)}&client_id=${clientId || ''}`), [dq, cat, clientId])
  const [modal, setModal] = useState<'new' | Document | null>(null)
  const [del, setDel] = useState<Document | null>(null)
  const reload = () => { list.reload(); meta.reload() }
  const categories = Object.keys(meta.data?.categories || {}).sort()
  const send = async (d: Document) => {
    try { const l = await api.post<PaperlessLink>(`${V1}/documents/${d.id}/paperless`); toast(l.paperless_id ? `Archived as Paperless #${l.paperless_id}` : 'Sent to Paperless, consuming…', 'success'); reload() } catch (e) { toast((e as Error).message, 'error') }
  }
  const expiring = meta.data?.expiring || []
  return (
    <>
      {expiring.length > 0 && <div className="callout warn mb"><b>Expiring soon:</b> {expiring.map((d, i) => <span key={d.id}>{i > 0 && ', '}<button className="link-btn" onClick={() => setModal(d)}>{d.title}</button> ({fmtDate(d.expires_at)})</span>)}</div>}
      <div className="filters">
        <input className="search" placeholder="Search title, file, notes…" value={q} onChange={e => setQ(e.target.value)} />
        <select value={cat} onChange={e => setCat(e.target.value)}><option value="">All categories</option>{Object.entries(meta.data?.categories || {}).filter(([, n]) => n > 0).sort().map(([c, n]) => <option key={c} value={c}>{c} ({n})</option>)}</select>
        <select value={clientId} onChange={e => setClientId(Number(e.target.value))}><option value={0}>All clients</option>{clients.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select>
        <span className="spacer" />
        <button className="btn primary" onClick={() => setModal('new')}>+ Upload</button>
      </div>
      {list.loading && !list.data ? <Loading /> : !list.data?.length ? <Empty title={q || cat || clientId ? 'No documents match' : 'No documents yet'}>Keep contracts, registration papers, tax decisions, bank letters and certificates in one place. Upload your first file to get started.</Empty> :
        <Card flush><div className="table-wrap"><table className="table">
          <thead><tr><th>Title</th><th>Category</th><th>Client</th><th>Date</th><th>Expires</th><th className="num">Size</th><th></th></tr></thead>
          <tbody>{list.data.map(d => {
            const ex = expiryState(d)
            return <tr key={d.id}>
              <td><a className="bold" href={`${V1}/documents/${d.id}/file`} target="_blank" rel="noreferrer">{d.title}</a><div className="muted small">{d.filename}{d.notes ? ` · ${d.notes}` : ''} {paperlessOn && <PaperlessBadge link={d.paperless} />}</div></td>
              <td>{d.category && <span className="badge">{d.category}</span>}</td>
              <td className="muted">{d.client_id ? <Link to={`/clients/${d.client_id}`}>{d.client_name}</Link> : ''}</td>
              <td className="muted">{fmtDate(d.doc_date)}</td>
              <td>{d.expires_at && <span className={`badge ${ex === 'expired' ? 'overdue' : ex === 'soon' ? 'partial' : ''}`}>{fmtDate(d.expires_at)}</span>}</td>
              <td className="num muted">{fileSize(d.size)}</td>
              <td className="actions">
                <a className="btn ghost sm" href={`${V1}/documents/${d.id}/file?download=1`} title="Download">↓</a>
                <button className="btn ghost sm" onClick={() => setModal(d)} title="Edit">✎</button>
                <label className="btn ghost sm" title="Replace file">⇄<input type="file" style={{ display: 'none' }} onChange={async e => { const f = e.target.files?.[0]; if (!f) return; const fd = new FormData(); fd.append('file', f); try { await api.post(`${V1}/documents/${d.id}/file`, fd); toast('File replaced', 'success'); reload() } catch (err) { toast((err as Error).message, 'error') } }} /></label>
                {paperlessOn && !(d.paperless && d.paperless.paperless_id > 0) && <button className="btn ghost sm" onClick={() => send(d)} title="Send to Paperless">⇪</button>}
                <button className="btn ghost sm" onClick={() => setDel(d)} title="Delete">✕</button>
              </td>
            </tr>
          })}</tbody>
        </table></div></Card>}
      {modal && <Modal title={modal === 'new' ? 'Upload documents' : 'Edit document'} size="lg" onClose={() => setModal(null)}>
        <DocForm initial={modal === 'new' ? undefined : modal} categories={categories} clients={clients} paperlessOn={paperlessOn} onSaved={() => { setModal(null); reload() }} onClose={() => setModal(null)} />
      </Modal>}
      {del && <Confirm title="Delete document" message={<>Delete <b>{del.title}</b>? The file is removed from this app{del.paperless?.paperless_id ? ' (it stays in Paperless)' : ''}.</>} onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/documents/${del.id}`); setDel(null); toast('Deleted', 'success'); reload() }} />}
    </>
  )
}

function PaperlessBrowser() {
  const toast = useToast()
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const dq = useDebounce(q)
  useEffect(() => { setPage(1) }, [dq])
  const res = useAsync(() => api.get<{ count: number; page: number; pages: number; results: PaperlessDoc[]; linked: Record<number, PaperlessLink> }>(`${V1}/paperless/documents?q=${encodeURIComponent(dq)}&page=${page}`), [dq, page])
  const [importing, setImporting] = useState<number | null>(null)
  const importDoc = async (d: PaperlessDoc) => {
    setImporting(d.id)
    try { await api.post(`${V1}/paperless/documents/${d.id}/import`, {}); toast(`"${d.title}" copied into Documents`, 'success'); res.reload() } catch (e) { toast((e as Error).message, 'error') } finally { setImporting(null) }
  }
  return (
    <>
      <div className="filters">
        <input className="search" placeholder="Full-text search in Paperless…" value={q} onChange={e => setQ(e.target.value)} />
        <span className="muted small">{res.data ? `${res.data.count} document${res.data.count === 1 ? '' : 's'}` : ''}</span>
        <span className="spacer" />
        {res.data && res.data.pages > 1 && <span className="row"><button className="btn sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>‹</button><span className="muted small">{page} / {res.data.pages}</span><button className="btn sm" disabled={page >= res.data.pages} onClick={() => setPage(p => p + 1)}>›</button></span>}
      </div>
      {res.error ? <div className="callout warn">{String(res.error)}</div> : res.loading && !res.data ? <Loading /> : !res.data?.results.length ? <Empty title="Nothing found in Paperless" /> :
        <Card flush><div className="table-wrap"><table className="table">
          <thead><tr><th></th><th>Title</th><th>Correspondent</th><th>Type</th><th>Tags</th><th>Created</th><th></th></tr></thead>
          <tbody>{res.data.results.map(d => {
            const linked = res.data!.linked?.[d.id]
            return <tr key={d.id}>
              <td style={{ width: 48 }}><a href={`${V1}/paperless/documents/${d.id}/file`} target="_blank" rel="noreferrer"><img src={`${V1}/paperless/documents/${d.id}/thumb`} alt="" style={{ width: 40, height: 52, objectFit: 'cover', borderRadius: 4, border: '1px solid var(--border)', background: 'var(--surface-2)' }} loading="lazy" /></a></td>
              <td><a className="bold" href={`${V1}/paperless/documents/${d.id}/file`} target="_blank" rel="noreferrer">{d.title}</a><div className="muted small">#{d.id}{d.archive_serial_number ? ` · ASN ${d.archive_serial_number}` : ''}{d.original_file_name ? ` · ${d.original_file_name}` : ''}{linked && <> · <span className="badge paid">{linked.kind === 'document' ? 'in Documents' : linked.kind === 'invoice' ? 'invoice' : 'invoice file'}</span></>}</div></td>
              <td className="muted">{d.correspondent}</td>
              <td className="muted">{d.document_type}</td>
              <td>{d.tags.map(t => <span key={t} className="badge" style={{ marginRight: 4 }}>{t}</span>)}</td>
              <td className="muted">{fmtDate(d.created)}</td>
              <td className="actions">
                <a className="btn ghost sm" href={d.url} target="_blank" rel="noreferrer" title="Open in Paperless">↗</a>
                <a className="btn ghost sm" href={`${V1}/paperless/documents/${d.id}/file?download=1`} title="Download">↓</a>
                {linked?.kind === 'document' ? <span className="muted small">copied</span> : linked?.kind === 'invoice' || linked?.kind === 'attachment' ? <Link className="btn ghost sm" to={linked.kind === 'invoice' ? `/invoices/${linked.ref_id}` : '/invoices'} title="Open invoice">▤</Link> : <button className="btn ghost sm" disabled={importing === d.id} onClick={() => importDoc(d)} title="Copy into Documents">⇩</button>}
              </td>
            </tr>
          })}</tbody>
        </table></div></Card>}
    </>
  )
}

export { expiryState }

export default function Documents() {
  const pl = usePaperless()
  const [tab, setTab] = useState<'local' | 'paperless'>('local')
  const on = !!pl?.configured
  return (
    <>
      <PageHeader title="Documents" sub="Company papers in one place: contracts, registrations, tax decisions, bank letters, certificates." />
      {pl && !on && <PaperlessOffNote />}
      {on && pl?.ok === false && <div className="callout warn mb">Paperless-ngx is configured but unreachable: {pl.error}</div>}
      {on && <Tabs tabs={[{ id: 'local' as const, label: 'Company documents' }, { id: 'paperless' as const, label: `Paperless-ngx${pl?.document_count ? ` (${pl.document_count})` : ''}` }]} value={tab} onChange={setTab} />}
      {tab === 'local' ? <LocalDocuments paperlessOn={on} /> : <PaperlessBrowser />}
    </>
  )
}
