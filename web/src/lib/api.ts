// Minimal fetch wrapper for the JSON API.
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, url: string, body?: unknown, raw = false): Promise<T> {
  const init: RequestInit = { method, headers: {}, credentials: 'same-origin' }
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  const res = await fetch(url, init)
  if (res.status === 401 && !url.includes('/auth/')) {
    window.dispatchEvent(new CustomEvent('pi:unauthorized'))
  }
  if (!res.ok) {
    let msg = res.statusText
    try {
      const j = await res.json()
      msg = j.error || msg
    } catch {}
    throw new ApiError(res.status, msg)
  }
  if (raw) return (await res.text()) as unknown as T
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  get: <T>(url: string) => request<T>('GET', url),
  getText: (url: string) => request<string>('GET', url, undefined, true),
  post: <T>(url: string, body?: unknown) => request<T>('POST', url, body),
  put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body),
  del: <T>(url: string) => request<T>('DELETE', url),
}

export const V1 = '/api/v1'
