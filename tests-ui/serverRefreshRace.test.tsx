import { beforeEach, describe, expect, test, vi } from 'vitest'

const apiMock = { stateIfChanged: vi.fn() }

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, api: apiMock }
})
// The merge itself is not under test here; only which response the store applies.
vi.mock('@/lib/discovered', async () => {
  const actual = await vi.importActual<typeof import('@/lib/discovered')>('@/lib/discovered')
  return { ...actual, mergeDiscovered: (raw: unknown) => raw }
})

// Imported after the mocks so the store closes over the mocked `api`.
const { useServer } = await import('@/store/server')

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const stateFor = (tag: string) => ({ tag, tombstones: [], topology: { dependencies: [], externalEndpoints: [], paths: [], clusterLinks: [], clusterPairConnectivity: [], agents: [] } })

function connectTo(orgId: string) {
  useServer.setState({ url: 'https://a.example', status: 'connected', user: { name: 'ann' } as never, orgId, state: undefined, error: undefined })
}

beforeEach(() => {
  vi.clearAllMocks()
  // let any refresh left over from the previous test settle before the next starts
  connectTo('org-a')
})

describe('useServer.refresh in-flight sharing', () => {
  test('two calls for the same organisation share one request', async () => {
    const a = deferred<unknown>()
    apiMock.stateIfChanged.mockReturnValueOnce(a.promise)
    const first = useServer.getState().refresh()
    const second = useServer.getState().refresh()
    expect(apiMock.stateIfChanged).toHaveBeenCalledTimes(1)
    a.resolve({ state: stateFor('a'), etag: '"a"' })
    await Promise.all([first, second])
    expect(useServer.getState().state).toMatchObject({ tag: 'a' })
  })

  test('a request still pending for the organisation just left is not handed to the one just entered', async () => {
    const a = deferred<unknown>()
    apiMock.stateIfChanged.mockReturnValueOnce(a.promise)
    const forA = useServer.getState().refresh()

    // Switch organisation while A's request is still out, as activate() does before it awaits refresh().
    connectTo('org-b')
    const b = deferred<unknown>()
    apiMock.stateIfChanged.mockReturnValueOnce(b.promise)
    const forB = useServer.getState().refresh()
    expect(apiMock.stateIfChanged).toHaveBeenCalledTimes(2)
    expect(apiMock.stateIfChanged.mock.calls[1][0]).toMatchObject({ org: 'org-b' })

    b.resolve({ state: stateFor('b'), etag: '"b"' })
    await forB
    expect(useServer.getState().state).toMatchObject({ tag: 'b' })

    // A's slow answer arrives last and must change nothing.
    a.resolve({ state: stateFor('a'), etag: '"a"' })
    await forA
    expect(useServer.getState().state).toMatchObject({ tag: 'b' })

    // ... and its finishing must not have released the slot while B's own request is out: a later call for B shares it.
    const b2 = deferred<unknown>()
    apiMock.stateIfChanged.mockReturnValueOnce(b2.promise)
    const p1 = useServer.getState().refresh()
    const p2 = useServer.getState().refresh()
    expect(apiMock.stateIfChanged).toHaveBeenCalledTimes(3)
    b2.resolve({ state: stateFor('b2'), etag: '"b2"' })
    await Promise.all([p1, p2])
  })

  test('a late failure for the organisation just left does not set an error on the current one', async () => {
    const a = deferred<unknown>()
    apiMock.stateIfChanged.mockReturnValueOnce(a.promise)
    const forA = useServer.getState().refresh()
    connectTo('org-b')
    a.reject(new Error('A timed out'))
    await forA
    expect(useServer.getState().error).toBeUndefined()
  })
})

describe('useServer.refresh asks whether the state changed', () => {
  test('sends the tag of the state it holds, and keeps that state when the server says nothing changed', async () => {
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('one'), etag: '"one"' })
    await useServer.getState().refresh()
    expect(apiMock.stateIfChanged.mock.calls[0][1]).toBeUndefined() // nothing held yet: a full answer
    const held = useServer.getState().state
    const before = useServer.getState().polledAt

    apiMock.stateIfChanged.mockResolvedValueOnce(undefined) // 304
    await new Promise((r) => setTimeout(r, 5))
    await useServer.getState().refresh()
    expect(apiMock.stateIfChanged.mock.calls[1][1]).toBe('"one"')
    expect(useServer.getState().state).toBe(held) // the very same object: nothing downstream starts over
    expect(useServer.getState().polledAt).toBeGreaterThan(before!) // but the confirmation is recorded
    expect(useServer.getState().error).toBeUndefined()
  })

  test('a new answer replaces the state and its tag is the next one sent', async () => {
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('one'), etag: '"one"' })
    await useServer.getState().refresh()
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('two'), etag: '"two"' })
    await useServer.getState().refresh()
    expect(useServer.getState().state).toMatchObject({ tag: 'two' })
    apiMock.stateIfChanged.mockResolvedValueOnce(undefined)
    await useServer.getState().refresh()
    expect(apiMock.stateIfChanged.mock.calls[2][1]).toBe('"two"')
  })

  test('a cleared state is never "unchanged": no tag is sent for it, so a 304 cannot leave the page empty', async () => {
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('one'), etag: '"one"' })
    await useServer.getState().refresh()
    connectTo('org-a') // as a sign-out and back in does: the state is cleared, the module still remembers the old tag
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('again'), etag: '"again"' })
    await useServer.getState().refresh()
    expect(apiMock.stateIfChanged.mock.calls[1][1]).toBeUndefined()
    expect(useServer.getState().state).toMatchObject({ tag: 'again' })
  })

  test('a tag held for one organisation is not sent for another', async () => {
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('a'), etag: '"a"' })
    await useServer.getState().refresh()
    connectTo('org-b')
    useServer.setState({ state: stateFor('a') as never }) // something is held, but it is org-a's
    apiMock.stateIfChanged.mockResolvedValueOnce({ state: stateFor('b'), etag: '"b"' })
    await useServer.getState().refresh()
    expect(apiMock.stateIfChanged.mock.calls[1][1]).toBeUndefined()
  })
})
