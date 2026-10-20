import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { DocsLink, DocsStatus } from '../lib/types'

/** Loads /pocket-documents/status once per mount; `configured` is false when the connection is off or not working. */
export function usePocketDocs() {
  const [status, setStatus] = useState<DocsStatus | null>(null)
  useEffect(() => { api.get<DocsStatus>(`${V1}/pocket-documents/status`).then(setStatus).catch(() => setStatus({ configured: false })) }, [])
  return status
}

/** Small inline indicator for a record's Pocket Documents state. */
export function DocsBadge({ link }: { link?: DocsLink | null }) {
  if (!link) return null
  if (link.documents_id > 0) return link.url
    ? <a className="badge paid" href={link.url} target="_blank" rel="noreferrer" title={`Open in Pocket Documents (#${link.documents_id})`}>Documents #{link.documents_id}</a>
    : <span className="badge paid" title="Filed in Pocket Documents">Documents #{link.documents_id}</span>
  if (link.error) return <span className="badge overdue" title={link.error}>Not filed</span>
  return null
}

export function DocsOffNote({ status }: { status: DocsStatus }) {
  if (status.mode === 'off') return null
  return <div className="callout mb">Pocket Documents is not connected{status.detail ? `: ${status.detail}` : ''}. Set it up under <Link to="/settings/documents">Settings → Pocket Documents</Link> to file invoices and documents there and to see each client's documents here.</div>
}
