import { describe, expect, it, vi } from 'vitest'
import { ApiError, CSRF_COOKIE, createClient } from './client'

// A fake browser: a cookie store the "server" can change, and a fetch whose
// responses are scripted per call.
function setup(responses: Array<(init: RequestInit, path: string) => Response>) {
  const cookies: Record<string, string> = {}
  const calls: Array<{ path: string; init: RequestInit }> = []
  const fetch = vi.fn(async (path: string, init: RequestInit) => {
    calls.push({ path, init })
    const next = responses.shift()
    if (!next) throw new Error(`unexpected request ${init.method} ${path}`)
    return next(init, path)
  })
  const onUnauthorized = vi.fn()
  const client = createClient({
    fetch: fetch as unknown as typeof globalThis.fetch,
    getCookie: (name) => cookies[name],
    onUnauthorized,
  })
  return { client, cookies, calls, onUnauthorized }
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const header = (init: RequestInit, name: string) => (init.headers as Record<string, string>)[name]

describe('CSRF handling', () => {
  it('uses the latest cookie value on every write, e.g. after login', async () => {
    const sent: Array<string | undefined> = []
    const t = setup([
      (init) => {
        sent.push(header(init, 'X-CSRF-Token'))
        // Login succeeds and the server replaces the CSRF cookie.
        t.cookies[CSRF_COOKIE] = 'session-token'
        return json(200, { user: { id: 1 }, has_profile: true, csrf_token: 'session-token' })
      },
      (init) => {
        sent.push(header(init, 'X-CSRF-Token'))
        return json(201, { id: 7 })
      },
    ])
    t.cookies[CSRF_COOKIE] = 'anon-token'

    await t.client.request('POST', '/api/auth/login', { username: 'a', password: 'b' })
    await t.client.request('POST', '/api/schedule-events', {})
    // The very next write uses the new token, with no csrf_failed retry.
    expect(sent).toEqual(['anon-token', 'session-token'])
    expect(t.calls).toHaveLength(2)
  })

  it('fetches a token first when there is no cookie yet', async () => {
    const t = setup([
      (_init, path) => {
        expect(path).toBe('/api/auth/csrf')
        t.cookies[CSRF_COOKIE] = 'fresh'
        return json(200, { csrf_token: 'fresh' })
      },
      (init) => {
        expect(header(init, 'X-CSRF-Token')).toBe('fresh')
        return new Response(null, { status: 204 })
      },
    ])
    await t.client.request('POST', '/api/auth/logout')
    expect(t.calls.map((c) => c.path)).toEqual(['/api/auth/csrf', '/api/auth/logout'])
  })

  it('refreshes the token and retries exactly once on csrf_failed', async () => {
    const t = setup([
      () => json(403, { error: { code: 'csrf_failed', message: '' } }),
      () => {
        t.cookies[CSRF_COOKIE] = 'renewed'
        return json(200, { csrf_token: 'renewed' })
      },
      (init) => {
        expect(header(init, 'X-CSRF-Token')).toBe('renewed')
        return json(200, { ok: true })
      },
    ])
    t.cookies[CSRF_COOKIE] = 'stale'
    await expect(t.client.request('PUT', '/api/profile', {})).resolves.toEqual({ ok: true })

    const t2 = setup([
      () => json(403, { error: { code: 'csrf_failed', message: '' } }),
      () => json(200, { csrf_token: 'x' }),
      () => json(403, { error: { code: 'csrf_failed', message: '' } }),
    ])
    t2.cookies[CSRF_COOKIE] = 'stale'
    await expect(t2.client.request('PUT', '/api/profile', {})).rejects.toMatchObject({ code: 'csrf_failed' })
    expect(t2.calls).toHaveLength(3)
  })

  it('does not send a CSRF header on GET', async () => {
    const t = setup([(init) => {
      expect(header(init, 'X-CSRF-Token')).toBeUndefined()
      return json(200, { items: [] })
    }])
    t.cookies[CSRF_COOKIE] = 'tok'
    await t.client.request('GET', '/api/meal-records?from=2026-10-01&to=2026-10-31')
  })
})

describe('errors', () => {
  it('parses the unified error format including details', async () => {
    const t = setup([() => json(422, {
      error: { code: 'validation_failed', message: 'x', details: [{ field: 'ends_at', reason: 'must_be_after_starts_at' }] },
    })])
    t.cookies[CSRF_COOKIE] = 'tok'
    const err = await t.client.request('POST', '/api/schedule-events', {}).catch((e) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 422, code: 'validation_failed', details: [{ field: 'ends_at', reason: 'must_be_after_starts_at' }] })
  })

  it('keeps the quota limit when there is one', async () => {
    const quota = (limit?: number) => () => json(429, { error: { code: 'quota_exceeded', message: 'x', limit } })
    const t = setup([quota(30), quota()])
    t.cookies[CSRF_COOKIE] = 'tok'
    await expect(t.client.request('POST', '/api/assistant/messages', {})).rejects.toMatchObject({ code: 'quota_exceeded', limit: 30 })
    const err = await t.client.request('POST', '/api/assistant/messages', {}).catch((e: ApiError) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).limit).toBeUndefined()
  })

  it('reports 401 to onUnauthorized unless quiet', async () => {
    const unauth = () => json(401, { error: { code: 'unauthenticated', message: '' } })
    const t = setup([unauth, unauth])
    await expect(t.client.request('GET', '/api/profile')).rejects.toMatchObject({ status: 401 })
    expect(t.onUnauthorized).toHaveBeenCalledTimes(1)
    await expect(t.client.request('GET', '/api/auth/me', undefined, { quiet401: true })).rejects.toMatchObject({ status: 401 })
    expect(t.onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('maps proxy errors and network failures to readable codes', async () => {
    const t = setup([
      () => new Response('Bad Gateway', { status: 502, statusText: 'Bad Gateway' }),
      () => { throw new TypeError('Failed to fetch') },
    ])
    await expect(t.client.request('GET', '/api/auth/me')).rejects.toMatchObject({ status: 502, code: 'server_unavailable' })
    await expect(t.client.request('GET', '/api/auth/me')).rejects.toMatchObject({ status: 0, code: 'network_error' })
  })

  it('returns undefined for 204', async () => {
    const t = setup([() => new Response(null, { status: 204 })])
    t.cookies[CSRF_COOKIE] = 'tok'
    await expect(t.client.request('DELETE', '/api/meal-records/1')).resolves.toBeUndefined()
  })
})
