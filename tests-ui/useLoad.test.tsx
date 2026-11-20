import { act, render, renderHook, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { DataBoundary } from '@/components/ui/DataBoundary'
import { ApiError } from '@/lib/api'
import { useLoad } from '@/lib/useLoad'
import { useServer } from '@/store/server'

const CONN = { url: '', org: 'o1' }

beforeEach(() => {
  useServer.setState({ conn: () => CONN, orgId: 'o1' } as never)
})
afterEach(() => {
  vi.useRealTimers()
})

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('useLoad', () => {
  test('is not loaded until the first answer, then holds it', async () => {
    const { result } = renderHook(() => useLoad(async () => ['a'], { failure: 'nope' }))
    expect(result.current.loaded).toBe(false)
    expect(result.current.data).toBeUndefined()
    await waitFor(() => expect(result.current.loaded).toBe(true))
    expect(result.current.data).toEqual(['a'])
    expect(result.current.error).toBe('')
  })

  test('an API error keeps its own message, anything else gets the stated failure', async () => {
    const api = renderHook(() => useLoad(async () => { throw new ApiError(500, 'server said no') }, { failure: 'fallback' }))
    await waitFor(() => expect(api.result.current.loaded).toBe(true))
    expect(api.result.current.error).toBe('server said no')
    const other = renderHook(() => useLoad(async () => { throw new Error('boom') }, { failure: 'fallback' }))
    await waitFor(() => expect(other.result.current.loaded).toBe(true))
    expect(other.result.current.error).toBe('fallback')
  })

  test('reads nothing while disabled, and counts as loaded', async () => {
    const read = vi.fn(async () => 1)
    const { result } = renderHook(() => useLoad(read, { enabled: false, failure: 'x' }))
    expect(result.current.loaded).toBe(true)
    await act(async () => { await result.current.reload() })
    expect(read).not.toHaveBeenCalled()
  })

  test('only the newest request writes, so a slow earlier one cannot bring back what it replaced', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    const answers = [first, second]
    const read = vi.fn(() => answers.shift()!.promise)
    const { result } = renderHook(() => useLoad(read, { failure: 'x' }))
    await waitFor(() => expect(read).toHaveBeenCalledTimes(1))
    let reloading!: Promise<void>
    act(() => { reloading = result.current.reload() })
    await act(async () => { second.resolve('new'); await reloading })
    expect(result.current.data).toBe('new')
    await act(async () => { first.resolve('old'); await first.promise })
    expect(result.current.data).toBe('new')
  })

  test('an answer equal to the held one keeps the same object', async () => {
    const read = vi.fn(async () => ({ n: 1 }))
    const { result } = renderHook(() => useLoad(read, { failure: 'x' }))
    await waitFor(() => expect(result.current.data).toEqual({ n: 1 }))
    const held = result.current.data
    await act(async () => { await result.current.reload() })
    expect(read).toHaveBeenCalledTimes(2)
    expect(result.current.data).toBe(held)
  })

  test('a change of key drops the old answer and reads again', async () => {
    const read = vi.fn(async (_c: unknown) => 'x')
    let subject = 'a'
    const { result, rerender } = renderHook(() => useLoad(async (c) => `${subject}:${await read(c)}`, { failure: 'x', key: subject }))
    await waitFor(() => expect(result.current.data).toBe('a:x'))
    subject = 'b'
    rerender()
    await waitFor(() => expect(result.current.data).toBe('b:x'))
  })

  test('a change of organisation starts over', async () => {
    const d = deferred<string>()
    let n = 0
    const read = vi.fn(() => (n++ === 0 ? Promise.resolve('org one') : d.promise))
    const { result } = renderHook(() => useLoad(read, { failure: 'x' }))
    await waitFor(() => expect(result.current.data).toBe('org one'))
    act(() => { useServer.setState({ orgId: 'o2' } as never) })
    await waitFor(() => expect(result.current.data).toBeUndefined())
    expect(result.current.loaded).toBe(false)
    await act(async () => { d.resolve('org two') })
    await waitFor(() => expect(result.current.data).toBe('org two'))
  })

  test('polls while asked to', async () => {
    vi.useFakeTimers()
    const read = vi.fn(async () => 1)
    renderHook(() => useLoad(read, { failure: 'x', pollMs: 1000 }))
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(read).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(2100) })
    expect(read.mock.calls.length).toBeGreaterThanOrEqual(3)
  })

  test('writes nothing after unmount', async () => {
    const d = deferred<string>()
    const { result, unmount } = renderHook(() => useLoad(() => d.promise, { failure: 'x' }))
    unmount()
    await act(async () => { d.resolve('late') })
    expect(result.current.data).toBeUndefined()
  })
})

describe('DataBoundary', () => {
  const view = (load: Parameters<typeof DataBoundary<string[]>>[0]['load']) =>
    render(
      <DataBoundary load={load} loading={<p>reading</p>} isEmpty={(d) => d.length === 0} empty="nothing here">
        {(d) => <p>{d.join(',')}</p>}
      </DataBoundary>,
    )
  const load = (p: Partial<Parameters<typeof DataBoundary<string[]>>[0]['load']>) => ({ data: undefined, loaded: false, error: '', reload: async () => {}, ...p })

  test('says it is being read until it has been', () => {
    view(load({}))
    expect(screen.getByText('reading')).toBeTruthy()
  })
  test('shows the error alone when there is nothing else', () => {
    view(load({ loaded: true, error: 'could not' }))
    expect(screen.getByText('could not')).toBeTruthy()
    expect(screen.queryByText('reading')).toBeNull()
  })
  test('says there is nothing, only once it knows', () => {
    view(load({ loaded: true, data: [] }))
    expect(screen.getByText('nothing here')).toBeTruthy()
  })
  test('shows the data, with the error above it when a later read failed', () => {
    view(load({ loaded: true, data: ['a', 'b'], error: 'stale' }))
    expect(screen.getByText('a,b')).toBeTruthy()
    expect(screen.getByText('stale')).toBeTruthy()
  })
})
