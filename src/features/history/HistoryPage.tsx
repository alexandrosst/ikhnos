import clsx from 'clsx'
import { CheckCircle2, Clock, Settings2, TriangleAlert } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Button, EmptyState, ErrorBanner, ICON_SM, PageHeader, PulseDot, Segmented, Select } from '@/components/ui/primitives'
import Feed from '@/features/history/components/Feed'
import People from '@/features/history/components/People'
import { words } from '@/features/history/lib/audit'
import RecordingPanel from '@/features/history/components/RecordingPanel'
import Scrubber from '@/features/history/components/Scrubber'
import type { FeedFilter } from '@/features/history/lib/feed'
import { api, atLeast, type AuditRow, type Conn, type StorageInfo } from '@/lib/api'
import type { ChangeEvent, HistoryIndex, TrafficRate } from '@/lib/history'
import { ago, bytesPerSec } from '@/lib/observed'
import { useConn, useServer } from '@/store/server'
import { useSettings } from '@/store/settings'
import { useTopology } from '@/store/topology'

const FILTERS: { value: FeedFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'changes', label: 'Detected' },
  { value: 'people', label: 'By people' },
]

export default function HistoryPage() {
  const status = useServer((s) => s.status)
  const admin = useServer((s) => atLeast(s.role, 'admin'))
  const conn = useConn()
  if (status !== 'connected') {
    return (
      <>
        <PageHeader title="History" description="What changed, when, and what it looked like." />
        <EmptyState
          title="History is recorded by an Ikhnos server"
          description="Connect to a server and it records the landscape as it changes: every scaling, migration, node joining or leaving and image change, with a snapshot you can scrub back through. This browser alone cannot see what happened while it was closed."
        />
      </>
    )
  }
  return <Connected conn={conn} admin={!!admin} />
}

type View = 'changes' | 'people'
const VIEWS: { value: View; label: string }[] = [
  { value: 'changes', label: 'Changes' },
  { value: 'people', label: 'People' },
]

/** Changes is what happened to the landscape; People is what people did, for administrators, and it has its own address (`?view=people`). */
function Connected({ conn, admin }: { conn: Conn; admin: boolean }) {
  const [params, setParams] = useSearchParams()
  const view: View = admin && params.get('view') === 'people' ? 'people' : 'changes'
  const switcher = admin ? (
    <div className="mb-5">
      <Segmented label="View" value={view} onChange={(v) => setParams(v === 'people' ? { view: 'people' } : {}, { replace: true })} options={VIEWS} />
    </div>
  ) : null
  if (view === 'people') {
    return (
      <>
        <PageHeader title="History" description="What changed, when, and what it looked like, with what people did alongside it." />
        {switcher}
        <People />
      </>
    )
  }
  return <Changes conn={conn} admin={admin} switcher={switcher} onSeeAll={() => setParams({ view: 'people' }, { replace: true })} />
}

function Changes({ conn, admin, switcher, onSeeAll }: { conn: Conn; admin: boolean; switcher: ReactNode; onSeeAll: () => void }) {
  const [hours, setHours] = useState(24)
  const [filter, setFilter] = useState<FeedFilter>('all')
  const [clusterId, setClusterId] = useState('')
  const [index, setIndex] = useState<HistoryIndex | null>(null)
  const [events, setEvents] = useState<ChangeEvent[] | null>(null)
  const [error, setError] = useState<string>()
  const [recording, setRecording] = useState(false)
  const { clusters, auditLog } = useTopology()
  const [tick, setTick] = useState(0)

  // Only the newest request may land: a slow answer (a poll in flight when a filter changed) must not replace what the current filter asked for.
  const newest = useRef(0)
  const load = useCallback(async () => {
    const mine = ++newest.current
    const since = new Date(Date.now() - hours * 3600_000).toISOString()
    try {
      const [i, e] = await Promise.all([api.history(conn, since), api.events(conn, { since, cluster: clusterId, limit: 400 })])
      if (mine !== newest.current) return
      setIndex(i)
      setEvents(e)
      setError(undefined)
    } catch (err) {
      if (mine !== newest.current) return
      setError(err instanceof Error ? err.message : 'The history could not be read.')
    }
  }, [conn, hours, clusterId])
  useEffect(() => {
    void load()
    // The poll loads here and only then bumps `tick` for the cards beside it; a second effect that reloaded on `tick`
    // as well as on `load` sent every request twice once a filter changed.
    const id = setInterval(() => {
      if (document.visibilityState !== 'visible') return
      void load()
      setTick((t) => t + 1)
    }, 15000)
    return () => clearInterval(id)
  }, [load])

  // "Since" is fixed when the window changes, not on every render, so the list does not shift under the reader.
  const since = useMemo(() => Date.now() - hours * 3600_000, [hours, tick]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <>
      <PageHeader
        title="History"
        description="What changed, when, and what it looked like, with what people did alongside it."
        actions={
          <>
            <Button onClick={() => setRecording(true)} data-testid="open-recording"><Settings2 size={ICON_SM} /> Recording</Button>
            {admin && <RecordNow onDone={() => { void load(); setTick((t) => t + 1) }} conn={conn} />}
          </>
        }
      />
      {switcher}
      {error && <ErrorBanner className="mb-4">{error}</ErrorBanner>}
      <div className="mb-6">
        <Scrubber hours={hours} setHours={setHours} index={index} events={events ?? []} />
      </div>
      <div className="grid gap-8 xl:grid-cols-[1fr_20rem]">
        <div className="min-w-0">
          <div className="mb-5 flex flex-wrap items-center gap-3">
            <Segmented label="Show" value={filter} onChange={setFilter} options={FILTERS} />
            <div className="w-44">
              <Select value={clusterId} onChange={(e) => setClusterId(e.target.value)} aria-label="Cluster">
                <option value="">All clusters</option>
                {clusters.filter((c) => !c.deletedAt).map((c) => (
                  <option key={c.id} value={c.id}>{c.name}</option>
                ))}
              </Select>
            </div>
          </div>
          <Feed events={events} audit={auditLog} filter={filter} clusterId={clusterId} since={since} hours={hours} />
        </div>
        <aside className="space-y-4" aria-label="About this history">
          {admin && <RecentPeople conn={conn} tick={tick} onSeeAll={onSeeAll} />}
          <PictureCheck />
          <BusiestLinks conn={conn} />
          <Stored conn={conn} tick={tick} />
        </aside>
      </div>
      {recording && <RecordingPanel admin={admin} conn={conn} onClose={() => setRecording(false)} />}
    </>
  )
}

function RecordNow({ conn, onDone }: { conn: Conn; onDone: () => void }) {
  const [busy, setBusy] = useState(false)
  return (
    <Button
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        try {
          await api.recordNow(conn)
          onDone()
        } finally {
          setBusy(false)
        }
      }}
    >
      <Clock size={ICON_SM} /> Record now
    </Button>
  )
}

const Card = ({ title, children, label }: { title: string; children: React.ReactNode; label: string }) => (
  <section className="rounded-xl border border-nb-850 bg-nb-925 p-4" aria-label={label}>
    <h2 className="mb-2.5 text-xs font-medium text-nb-300">{title}</h2>
    {children}
  </section>
)

/** The last few things people did, for administrators, beside the feed rather than in it: they come from the server's audit trail, a different record from what Ikhnos noticed. The whole trail is the People view. */
function RecentPeople({ conn, tick, onSeeAll }: { conn: Conn; tick: number; onSeeAll: () => void }) {
  const [rows, setRows] = useState<AuditRow[] | null>(null)
  useEffect(() => {
    let live = true
    api.audit(conn, { limit: 5 }).then((a) => live && setRows(a.rows), () => live && setRows([]))
    return () => { live = false }
  }, [conn, tick])
  return (
    <section className="rounded-xl border border-nb-850 bg-nb-925 p-4" aria-label="Recent by people" data-testid="recent-people">
      <div className="mb-2.5 flex items-baseline justify-between">
        <h2 className="text-xs font-medium text-nb-300">Recent by people</h2>
        <button type="button" onClick={onSeeAll} className="text-xs text-accent hover:underline" data-testid="recent-people-all">See all</button>
      </div>
      {rows === null ? null : rows.length === 0 ? (
        <p className="text-sm text-nb-500">Nobody has changed anything yet.</p>
      ) : (
        <ul className="divide-y divide-nb-850">
          {rows.map((r) => (
            <li key={r.id} className="flex items-baseline justify-between gap-3 py-1.5 text-[13px]" data-testid="recent-people-row">
              <span className="min-w-0 text-nb-400"><span className="font-medium text-nb-300">{r.actor}</span> {words(r.action)}{r.targetId ? ` ${r.targetId}` : ''}</span>
              <span className="shrink-0 text-xs text-nb-500">{ago(r.at)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** Does the server's picture match the clusters' own? One line while it does; the agents that differ, or have not been checked, only when they do not. */
function PictureCheck() {
  // no `?? []` inside the selector: a fresh array on every read makes the store report a change every time
  const allAgents = useServer((s) => s.state?.agents)
  const live = useMemo(() => (allAgents ?? []).filter((a) => a.status === 'approved'), [allAgents])
  const minutes = useSettings((s) => s.settings.consistencyMinutes)
  const odd = live.filter((a) => !a.consistency || a.consistency.differences > 0)
  const last = live.map((a) => a.consistency?.lastCheck).filter((x): x is string => !!x).sort().pop()
  return (
    <Card title="Is the picture complete?" label="Consistency checks">
      {live.length === 0 ? (
        <p className="text-sm text-nb-500">No cluster is connected yet, so there is no picture to check.</p>
      ) : (
        <>
          <p className="flex items-center gap-2 text-sm text-nb-300">
            {odd.length === 0 ? <CheckCircle2 size={ICON_SM} className="shrink-0 text-ok" aria-hidden /> : <TriangleAlert size={ICON_SM} className="shrink-0 text-warn" aria-hidden />}
            {odd.length === 0 ? `All ${live.length} agent${live.length === 1 ? '' : 's'} match their cluster${live.length === 1 ? '' : 's'}` : `${odd.length} of ${live.length} need a look`}
          </p>
          <p className="mt-1.5 text-xs text-nb-500" title={`Every ${minutes} minutes each agent re-sends everything it sees and the server compares it with its own picture. A difference means a change was missed; it is corrected and listed here.`}>
            {last ? `Last check ${ago(last)}` : 'Not checked yet'}
          </p>
          {odd.length > 0 && (
            <ul className="mt-3 space-y-2">
              {odd.map((a) => (
                <li key={a.id} className="text-xs" data-testid="consistency-row">
                  <span className="font-medium text-nb-300">{a.name}</span>
                  {a.consistency ? <span className="text-warn/90"> · {a.consistency.differences} difference{a.consistency.differences === 1 ? '' : 's'}: {a.consistency.summary}. Corrected.</span> : <span className="text-nb-500"> · not checked yet</span>}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </Card>
  )
}

/** A link that carries a trickle is not the same as one that carries nothing. */
const rate = (n: number) => (n > 0 && n < 1 ? '<1 B/s' : bytesPerSec(n))

/** The three busiest links over the last day, each with a bar against the busiest. */
function BusiestLinks({ conn }: { conn: Conn }) {
  const [rates, setRates] = useState<TrafficRate[] | null>(null)
  const [snapshots, setSnapshots] = useState(0)
  const { dependencies, services, devices, externalEndpoints } = useTopology()
  useEffect(() => {
    let live = true
    api.traffic(conn, 24).then((r) => {
      if (live) {
        setRates(r.rates)
        setSnapshots(r.snapshots)
      }
    }).catch(() => live && setRates([]))
    return () => {
      live = false
    }
  }, [conn])
  const names = useMemo(() => new Map<string, string>([...services.map((s) => [s.id, s.name] as const), ...devices.map((d) => [d.id, d.name] as const), ...externalEndpoints.map((e) => [e.id, e.name ?? e.host] as const)]), [services, devices, externalEndpoints])
  const byId = useMemo(() => new Map(dependencies.map((d) => [d.id, d])), [dependencies])
  const rows = (rates ?? []).filter((r) => r.avgBytesPerSec > 0).sort((a, b) => b.avgBytesPerSec - a.avgBytesPerSec).slice(0, 3)
  const top = rows[0]?.avgBytesPerSec ?? 1
  return (
    <Card title="Busiest links · 24 h" label="Busiest links">
      {rates === null ? (
        <p className="text-sm text-nb-500">Reading…</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-nb-500">No traffic figures yet. They come from the traffic observer, from at least two recordings; see Discovery to switch it on.</p>
      ) : (
        <ul className="space-y-3" title={`Worked out from ${snapshots} recordings; the average covers the time between them, so a link that was busy for a minute in an hour shows a low average.`}>
          {rows.map((r) => {
            const d = byId.get(r.id)
            return (
              <li key={r.id} className="text-[13px]">
                <div className="flex justify-between gap-3">
                  <span className="min-w-0 truncate text-nb-300">{d ? `${names.get(d.from) ?? d.from} → ${names.get(d.to) ?? d.to}` : r.id}</span>
                  <span className="shrink-0 tabular-nums text-nb-500">{rate(r.avgBytesPerSec)}</span>
                </div>
                <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-nb-850"><div className="h-full rounded-full bg-info" style={{ width: `${Math.max(3, (r.avgBytesPerSec / top) * 100)}%` }} /></div>
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}

/** Where the recordings are kept, in a few words. */
function Stored({ conn, tick }: { conn: Conn; tick: number }) {
  const [info, setInfo] = useState<StorageInfo | null>(null)
  useEffect(() => {
    let live = true
    api.storage(conn).then((i) => live && setInfo(i)).catch(() => live && setInfo(null))
    return () => {
      live = false
    }
  }, [conn, tick])
  if (!info) return null
  const graph = info.backend === 'neo4j'
  const st = info.stats
  return (
    <section className="rounded-xl border border-nb-850 bg-nb-925 p-4" aria-label="Where history is kept" data-testid="storage-card">
      <h2 className="mb-2.5 text-xs font-medium text-nb-300">Stored</h2>
      {graph ? (
        <>
          <span className={clsx('inline-flex items-center gap-2 text-sm', info.connected ? 'text-nb-300' : 'text-warn')} data-testid="storage-state">
            <PulseDot color={info.connected ? 'bg-ok' : 'bg-warn'} pulse={info.connected} />
            {info.connected ? 'In the graph database' : info.ready ? 'Graph database not reachable' : 'Graph database starting'}
          </span>
          {info.buffering && <p className="mt-1.5 text-xs text-nb-500">Catching up: some recordings are waiting to be moved across.</p>}
          {st && <p className="mt-1.5 text-xs text-nb-500">{st.snapshots} recordings{st.oldest ? `, back to ${new Date(st.oldest).toLocaleDateString()}` : ''}.</p>}
          {info.error && !info.connected && <p className="mt-1.5 text-xs text-warn/90" role="status">{info.error}</p>}
        </>
      ) : (
        <p className="text-xs leading-5 text-nb-500">
          In the server’s own database file: it can be scrubbed and searched by time. Questions across time, such as what ran on a node last Tuesday, need the graph database (<code className="text-nb-300">--neo4j-url</code>).
        </p>
      )}
    </section>
  )
}
