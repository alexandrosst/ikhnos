import clsx from 'clsx'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Input, Segmented } from '@/components/ui/primitives'
import { ageOf, pointAt, type ChangeEvent, type HistoryIndex } from '@/lib/history'
import { useHistoryView } from '@/store/history'
import { useConn } from '@/store/server'

export const WINDOWS = [
  { value: 24, label: '24 h' },
  { value: 24 * 7, label: '7 days' },
  { value: 24 * 30, label: '30 days' },
]
const DOT: Record<ChangeEvent['severity'], string> = { info: 'bg-nb-500', notice: 'bg-info', warning: 'bg-warn' }
const short = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

/** Pick a moment on one slim line and see the landscape then. The marks are the changes in the window; the knob lands on the nearest recording. */
export default function Scrubber({ hours, setHours, index, events }: { hours: number; setHours: (h: number) => void; index: HistoryIndex | null; events: ChangeEvent[] }) {
  const conn = useConn()
  const navigate = useNavigate()
  const view = useHistoryView((s) => s.view)
  const loading = useHistoryView((s) => s.loading)
  const viewError = useHistoryView((s) => s.error)
  const [frac, setFrac] = useState(1)
  const [exactOpen, setExactOpen] = useState(false)
  const [exact, setExact] = useState('')
  const end = Date.now()
  const start = end - hours * 3600_000
  const points = index?.points ?? []
  const inWindow = points.filter((p) => new Date(p.at).getTime() >= start)
  const point = pointAt(inWindow.length ? inWindow : points, new Date(start + frac * (end - start)).toISOString())
  const pos = (iso: string) => Math.min(100, Math.max(0, ((new Date(iso).getTime() - start) / (end - start)) * 100))
  const open = async (at: string) => {
    if (await view(conn, at)) navigate('/topology')
  }

  return (
    <section className="rounded-xl border border-nb-850 bg-nb-925 px-5 py-4" aria-label="Timeline">
      {points.length === 0 ? (
        <p className="text-sm text-nb-500">Nothing has been recorded yet. The first recording is made as soon as a cluster is connected and reporting.</p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
            <div className="min-w-36">
              <h2 className="text-sm font-medium text-nb-300">Go back to a moment</h2>
              <p className="mt-0.5 text-xs text-nb-500">{point ? `${short(point.at)} · ${ageOf(point.at)}` : 'Pick a moment on the line.'}</p>
            </div>
            <div className="relative h-8 min-w-48 flex-1" data-testid="timeline">
              <div className="absolute inset-x-0 top-[15px] h-[3px] rounded bg-nb-850" />
              {events.filter((e) => new Date(e.at).getTime() >= start).map((e) => (
                <span key={e.id} className={clsx('absolute top-[11px] size-[9px] -translate-x-1/2 rounded-full', DOT[e.severity])} style={{ left: `${pos(e.at)}%` }} aria-hidden />
              ))}
              <input
                type="range"
                min={0}
                max={1000}
                value={Math.round(frac * 1000)}
                onChange={(e) => setFrac(Number(e.target.value) / 1000)}
                aria-label="Moment to view"
                className="absolute inset-x-0 top-[7px] h-4 w-full cursor-pointer appearance-none bg-transparent accent-[var(--color-accent)]"
              />
            </div>
            <Segmented label="Time window" value={hours} options={WINDOWS} onChange={(h) => { setHours(h); setFrac(1) }} />
            <Button variant="primary" disabled={!point || loading} onClick={() => point && open(point.at)} data-testid="view-moment">
              View {point ? short(point.at) : 'then'}
            </Button>
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-nb-500">
            <span>{points.length} recordings kept, back to {new Date(points[0].at).toLocaleDateString()}.</span>
            <button className="text-accent hover:underline" onClick={() => setExactOpen((v) => !v)} aria-expanded={exactOpen}>Exact time…</button>
          </div>
          {exactOpen && (
            <form
              className="mt-3 flex flex-wrap items-center gap-2 border-t border-nb-850 pt-3"
              onSubmit={(e) => {
                e.preventDefault()
                if (exact) void open(new Date(exact).toISOString())
              }}
            >
              <Input type="datetime-local" value={exact} step={1} onChange={(e) => setExact(e.target.value)} className="w-56" aria-label="Exact moment" data-testid="exact-moment" />
              <Button type="submit" disabled={!exact || loading} data-testid="view-exact">View the landscape then</Button>
              <span className="min-w-0 flex-1 basis-48 text-xs text-nb-500">You get the newest recording at or before that time; the banner says exactly which.</span>
            </form>
          )}
          {viewError && <p className="mt-2 text-sm text-bad" role="alert">{viewError}</p>}
        </>
      )}
    </section>
  )
}
