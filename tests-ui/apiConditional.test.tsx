import { afterEach, describe, expect, test, vi } from 'vitest'
import { api, ApiError, type Conn } from '@/lib/api'

const c = { url: 'https://s.example', org: 'o1' } as Conn

afterEach(() => vi.unstubAllGlobals())

function answer(init: { status: number; body?: unknown; etag?: string }) {
  const fetchMock = vi.fn(async () => new Response(init.body === undefined ? null : JSON.stringify(init.body), { status: init.status, headers: init.etag ? { ETag: init.etag } : {} }))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('stateIfChanged', () => {
  test('sends the tag it was given, and gets undefined for the server\'s 304', async () => {
    const f = answer({ status: 304 })
    expect(await api.stateIfChanged(c, '"s-1"')).toBeUndefined()
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('https://s.example/api/v1/orgs/o1/state')
    expect((init.headers as Record<string, string>)['If-None-Match']).toBe('"s-1"')
  })
  test('sends no tag when it holds nothing, and returns the normalised state with its new tag', async () => {
    const f = answer({ status: 200, body: { generatedAt: '2026-01-01T00:00:00Z', agents: [] }, etag: '"s-2"' })
    const r = await api.stateIfChanged(c, undefined)
    expect((f.mock.calls[0] as unknown as [string, RequestInit])[1].headers).not.toHaveProperty('If-None-Match')
    expect(r?.etag).toBe('"s-2"')
    expect(r?.state.generatedAt).toBe('2026-01-01T00:00:00Z')
    expect(r?.state.topology.dependencies).toEqual([]) // a missing part is an empty one, as `state` has always made it
  })
  test('a failure is an ApiError with the server\'s own words', async () => {
    answer({ status: 403, body: { error: 'not for you' } })
    await expect(api.stateIfChanged(c, undefined)).rejects.toMatchObject({ status: 403, message: 'not for you' })
    expect(ApiError).toBeDefined()
  })
  test('an unreachable server is status 0', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('network') }))
    await expect(api.stateIfChanged(c, undefined)).rejects.toMatchObject({ status: 0 })
  })
})
