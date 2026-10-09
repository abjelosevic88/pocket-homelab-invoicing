import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { PaperlessLink, PaperlessStatus } from '../lib/types'

/** Loads /paperless/status once per mount; `configured` is false when the integration is off. */
export function usePaperless() {
  const [status, setStatus] = useState<PaperlessStatus | null>(null)
  useEffect(() => { api.get<PaperlessStatus>(`${V1}/paperless/status`).then(setStatus).catch(() => setStatus({ configured: false })) }, [])
  return status
}

/** Small inline indicator for a record's Paperless state. */
export function PaperlessBadge({ link }: { link?: PaperlessLink | null }) {
  if (!link) return null
  if (link.paperless_id > 0) return <a className="badge paid" href={link.url} target="_blank" rel="noreferrer" title={`Open in Paperless (#${link.paperless_id})`}>Paperless #{link.paperless_id}</a>
  if (link.error) return <span className="badge overdue" title={link.error}>Paperless failed</span>
  return <span className="badge sent" title="Paperless is still consuming this file">Paperless…</span>
}

export function PaperlessOffNote() {
  return <div className="callout mb">Paperless-ngx is not connected. Enter its URL and an API token under <Link to="/settings/paperless">Settings → Paperless</Link> to archive invoices and documents there and to browse your archive from here.</div>
}
