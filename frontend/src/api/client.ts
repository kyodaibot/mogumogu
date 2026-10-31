// Thin wrapper around fetch implementing the conventions in docs/api.md:
// JSON bodies, the unified error format, and CSRF (section 2.4).

export const CSRF_COOKIE = 'mogumogu_csrf'
const CSRF_HEADER = 'X-CSRF-Token'

export interface FieldError {
  field: string
  reason: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: FieldError[]
  /** The user's own daily quota; only on quota_exceeded when that is what ran out. */
  readonly limit?: number

  constructor(status: number, code: string, message: string, details: FieldError[] = [], limit?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
    this.limit = limit
  }
}

export interface ClientDeps {
  fetch: typeof fetch
  /** Returns the current value of a cookie, read fresh on every call. */
  getCookie: (name: string) => string | undefined
  /** Called when an authenticated request gets 401 (session expired or revoked). */
  onUnauthorized?: () => void
}

export interface RequestOptions {
  /** Do not call onUnauthorized on 401 (used for the startup "who am I" check). */
  quiet401?: boolean
}

export type Method = 'GET' | 'POST' | 'PUT' | 'DELETE'

export function createClient(deps: ClientDeps) {
  async function send(method: Method, path: string, body?: unknown): Promise<Response> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (method !== 'GET') {
      // Never cached: login, register and logout replace the token, and the
      // next request must use the new one (docs/api.md 2.4).
      const token = deps.getCookie(CSRF_COOKIE)
      if (token) headers[CSRF_HEADER] = token
    }
    try {
      return await deps.fetch(path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
      })
    } catch {
      throw new ApiError(0, 'network_error', 'Network request failed')
    }
  }

  async function toError(res: Response): Promise<ApiError> {
    const data = await res.json().catch(() => null)
    const e = data?.error
    if (e && typeof e.code === 'string') {
      const limit = typeof e.limit === 'number' ? e.limit : undefined
      return new ApiError(res.status, e.code, e.message ?? '', Array.isArray(e.details) ? e.details : [], limit)
    }
    // Not our JSON, e.g. the dev proxy's 502 when the backend is down.
    return new ApiError(res.status, res.status >= 500 ? 'server_unavailable' : 'unexpected_response', res.statusText)
  }

  /** Fetches a CSRF token (sets the cookie) when none is present yet. */
  async function ensureCsrf(force = false): Promise<void> {
    if (!force && deps.getCookie(CSRF_COOKIE)) return
    const res = await send('GET', '/api/auth/csrf')
    if (!res.ok) throw await toError(res)
  }

  async function request<T>(method: Method, path: string, body?: unknown, opts: RequestOptions = {}): Promise<T> {
    if (method !== 'GET') await ensureCsrf()
    let res = await send(method, path, body)
    let err = res.ok ? null : await toError(res)

    // The token may have expired or belong to an old session: refresh once.
    if (err?.code === 'csrf_failed') {
      await ensureCsrf(true)
      res = await send(method, path, body)
      err = res.ok ? null : await toError(res)
    }
    if (err) {
      if (err.status === 401 && !opts.quiet401) deps.onUnauthorized?.()
      throw err
    }
    if (res.status === 204) return undefined as T
    return (await res.json()) as T
  }

  return { request, ensureCsrf }
}

export type ApiClient = ReturnType<typeof createClient>

/** Reads a cookie from document.cookie. */
export function readDocumentCookie(name: string): string | undefined {
  for (const part of document.cookie.split('; ')) {
    const i = part.indexOf('=')
    if (part.slice(0, i) === name) return decodeURIComponent(part.slice(i + 1))
  }
  return undefined
}
