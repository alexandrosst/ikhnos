import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError, type Conn } from '@/lib/api'
import { useVisiblePolling } from '@/lib/usePolling'
import { useServer } from '@/store/server'

export interface LoadOptions {
  /** Read nothing while false (a page the person may not see, a panel that is closed). Default true. */
  enabled?: boolean
  /** What to say when the read fails with something that is not an API error (those carry their own message). */
  failure: string
  /** Read again every this many ms while the tab is visible. Omit for a single read. */
  pollMs?: number
  /** Anything the read depends on that is not the organisation (an id the panel is about). A change starts over: the old answer
   *  is dropped rather than shown under the new subject until the new one arrives. */
  key?: string | number
  /** Read again, keeping what is shown, whenever this changes (a counter a parent bumps when something happened elsewhere on the page).
   *  Unlike `key`, the answer held stays on screen until the new one arrives. */
  reloadOn?: unknown
}

export interface Load<T> {
  /** The last answer, or undefined until the first one has come back. */
  data: T | undefined
  /** True once the first answer (or the first failure) has come back - until then no data means "not known yet", not "nothing". */
  loaded: boolean
  error: string
  reload: () => Promise<void>
}

/**
 * One server read that stays correct however it is used: read once (and again every `pollMs` while the tab is visible), the newest
 * request alone may write its answer (so a slow one that left before a reload cannot land after it and bring back what it replaced),
 * nothing is written after unmount, switching organisation or `key` starts over instead of showing the previous one's data, and an
 * answer equal to the one already held keeps the same object (so a poll that found nothing new makes nothing built from it start over).
 *
 * The hand-written version of this is a `let live = true` effect with its own loading and error state; it is easy to get subtly wrong
 * (no guard against a slower earlier request, no reset on organisation change), which is why it lives here once.
 */
export function useLoad<T>(read: (c: Conn) => Promise<T>, { enabled = true, failure, pollMs, key, reloadOn }: LoadOptions): Load<T> {
  const conn = useServer((s) => s.conn)
  // `conn` is one stable function that reads the organisation in use when it is called, so a switch of organisation is invisible to it:
  // the organisation itself is what makes the read start over, instead of showing the last one's data until the next poll.
  const org = useServer((s) => s.orgId)
  const [data, setData] = useState<T>()
  const [loaded, setLoaded] = useState(!enabled)
  const [error, setError] = useState('')
  const newest = useRef(0)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const readRef = useRef(read)
  useEffect(() => {
    readRef.current = read
  })
  const shown = useRef({ org, key })
  useEffect(() => {
    if (shown.current.org === org && shown.current.key === key) return
    shown.current = { org, key }
    setData(undefined)
    setError('')
    setLoaded(!enabled)
  }, [org, key, enabled])

  const reload = useCallback(async () => {
    const c = conn()
    if (!c || !enabled) return
    const mine = ++newest.current
    try {
      const next = await readRef.current(c)
      if (!alive.current || mine !== newest.current) return
      setData((prev) => (prev !== undefined && JSON.stringify(prev) === JSON.stringify(next) ? prev : next))
      setError('')
    } catch (e) {
      if (!alive.current || mine !== newest.current) return
      setError(e instanceof ApiError ? e.message : failure)
    } finally {
      if (alive.current && mine === newest.current) setLoaded(true)
    }
  // org, key and reloadOn are not read in here (conn() reads the organisation at call time, the read closure reads the key); they are
  // dependencies so that a change makes a new callback, and with it a fresh read.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conn, enabled, failure, org, key, reloadOn])
  useEffect(() => {
    void reload()
  }, [reload])
  useVisiblePolling(() => void reload(), enabled && pollMs ? pollMs : null)
  return { data, loaded, error, reload }
}
