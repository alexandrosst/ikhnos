import clsx from 'clsx'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { EmptyState, TableSkeleton } from '@/components/ui/primitives'
import { buildFeed, groupByDay, type FeedFilter, type FeedItem } from '@/features/history/lib/feed'
import type { ChangeEvent } from '@/lib/history'
import { useHistoryView } from '@/store/history'
import { useConn } from '@/store/server'
import type { AuditEvent } from '@/lib/types'

const PAGE = 20
const SEVERITY: Record<ChangeEvent['severity'], { dot: string; label: string }> = {
  info: { dot: 'border-nb-500 bg-nb-925', label: 'Info' },
  notice: { dot: 'border-info bg-nb-925', label: 'Notice' },
  warning: { dot: 'border-warn bg-warn', label: 'Warning' },
}
const clock = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })

/**
 * What changed and what people did, on one vertical line, newest first and grouped by day: a ring for something Ikhnos noticed (filled when
 * it needs a look), a square for a person. Twenty at a time; "Show earlier" adds twenty more.
 */
export default function Feed({ events, audit, filter, clusterId, since, hours }: { events: ChangeEvent[] | null; audit: AuditEvent[]; filter: FeedFilter; clusterId: string; since: number; hours: number }) {
  const [shown, setShown] = useState(PAGE)
  if (events === null) return <TableSkeleton colCount={3} />
  const items = buildFeed(events, audit, { filter, clusterId: clusterId || undefined, since })
  if (items.length === 0) {
    const window = hours < 48 ? `${hours} hour${hours === 1 ? '' : 's'}` : `${Math.round(hours / 24)} days`
    return <EmptyState title="Nothing in this window" description={`No ${filter === 'people' ? 'actions by people' : filter === 'changes' ? 'changes' : 'changes or actions'} were recorded in the last ${window}${clusterId ? ' for that cluster' : ''}. Try a wider window.`} />
  }
  const visible = items.slice(0, shown)
  const days = groupByDay(visible)
  return (
    <div data-testid="feed">
      {days.map((d) => (
        <section key={d.label} aria-label={d.label}>
          <h3 className="mb-3 ml-7 mt-1 text-[11px] font-medium uppercase tracking-wider text-nb-500">{d.label}</h3>
          <ol className="relative mb-2 before:absolute before:bottom-2 before:left-[5px] before:top-2 before:w-px before:bg-nb-800">
            {d.items.map((it) => (
              <Entry key={it.id} item={it} />
            ))}
          </ol>
        </section>
      ))}
      {items.length > visible.length && (
        <button onClick={() => setShown((n) => n + PAGE)} className="ml-7 text-sm text-accent hover:underline" data-testid="feed-more">
          Show earlier · {Math.min(PAGE, items.length - visible.length)} of {items.length - visible.length} more
        </button>
      )}
    </div>
  )
}

function Entry({ item }: { item: FeedItem }) {
  const navigate = useNavigate()
  const conn = useConn()
  const view = useHistoryView((s) => s.view)
  const change = item.type === 'change'
  return (
    <li className="relative pb-5 pl-7" data-testid={change ? 'event-row' : 'audit-row'}>
      <span
        className={clsx('absolute left-0 top-1 size-[11px] border-2', change ? clsx('rounded-full', SEVERITY[item.severity].dot) : 'rounded-[3px] border-nb-500 bg-nb-925')}
        aria-hidden
      />
      <div className="flex items-baseline justify-between gap-4">
        <div className="min-w-0 text-sm">
          {change ? (
            <>
              <span className="sr-only">{SEVERITY[item.severity].label}: </span>
              <span className="font-medium text-nb-300">{item.name}</span>
              <span className="text-nb-500"> · {item.what}{item.where ? ` · ${item.where}` : ''}</span>
            </>
          ) : (
            <>
              <span className="font-medium text-nb-300">{item.actor}</span>
              <span className="text-nb-500"> {item.what}</span>
            </>
          )}
        </div>
        <time className="shrink-0 text-xs text-nb-500" dateTime={item.at} title={new Date(item.at).toLocaleString()}>{clock(item.at)}</time>
      </div>
      {item.detail && <p className="mt-0.5 text-[13px] text-nb-400">{item.detail}</p>}
      {change && item.cause && <p className="mt-0.5 text-xs italic text-nb-500">Likely cause: {item.cause}</p>}
      {change && (
        <button
          onClick={async () => (await view(conn, item.at)) && navigate('/topology')}
          className="mt-1 text-xs text-accent hover:underline"
          title="Show the landscape at the moment of this change"
        >
          View then
        </button>
      )}
    </li>
  )
}
