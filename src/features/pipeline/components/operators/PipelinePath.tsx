import clsx from 'clsx'
import { Cable, Database, DoorOpen, Funnel, Merge, type LucideIcon } from 'lucide-react'
import { Fragment, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import StateChip from '@/features/pipeline/components/operators/StateChip'
import { MeterBar, type BarTone } from '@/components/ui/MeterBar'
import { ICON_MD } from '@/components/ui/primitives'
import { lastDataText, type Hop, type Kind } from '@/features/pipeline/lib/operatorsView'
import { Arrow } from '@/features/pipeline/components/operators/FlowArrow'
import { EXPECTED_MS, freshness, hopChip, LINK_TONE as TONE, linkState, type FreshnessLevel, type LinkState } from '@/features/pipeline/lib/pipelineFlow'

const ICON: Record<Hop['key'], LucideIcon> = { agent: Cable, local: Funnel, regional: Merge, central: DoorOpen, fusion: Database }

/** What the count says: how many there are. FUSION is one thing, on or off. */
function figure(h: Hop): string {
  if (h.key === 'fusion') return h.total === 0 ? 'Off' : h.state === 'unknown' ? 'Starting' : 'On'
  return String(h.total)
}

const BAR: Record<FreshnessLevel, BarTone> = { fresh: 'ok', late: 'warn', stale: 'bad', none: 'neutral' }
const AGE_TEXT: Record<LinkState, string> = { flowing: 'hidden text-nb-500 group-hover:block group-focus-within:block', stalled: 'text-warn', broken: 'text-bad', unknown: 'text-nb-500' }

function Node({ h, selected, onSelect }: { h: Hop; selected: boolean; onSelect: (kind: Kind) => void }) {
  const Icon = ICON[h.key]
  const chip = hopChip(h)
  const tile = (
    <div className={clsx('flex h-full flex-wrap items-center gap-x-2 gap-y-1 rounded-xl border bg-nb-925 px-3 py-2.5 transition-colors xl:flex-col xl:items-start', selected ? 'border-accent/50 ring-1 ring-accent/40' : 'border-nb-850 hover:border-nb-800', h.total === 0 && 'text-nb-500')}>
      <span className="flex items-center gap-2">
        <Icon size={ICON_MD} className="shrink-0 text-nb-500" aria-hidden />
        <span className={clsx('text-xl font-medium tabular-nums', h.total === 0 ? 'text-nb-500' : 'text-nb-300')}>{figure(h)}</span>
      </span>
      <span className="min-w-0 flex-1 truncate text-xs text-nb-500 xl:w-full xl:flex-none">{h.label}</span>
      {chip && <StateChip state={chip.state} count={chip.count} compact className="xl:mt-0.5" />}
      {/* New ones are counted beside the health chip, never in it: a component that has not said anything yet is not a problem. */}
      {h.settingUp > 0 && <StateChip state="setup" count={h.settingUp} className="xl:mt-0.5" />}
    </div>
  )
  const focus = 'block h-full w-full rounded-xl text-left'
  return h.key === 'fusion' ? (
    <Link to="/fusion" className={focus} aria-label="FUSION: open its section">{tile}</Link>
  ) : (
    <button type="button" className={focus} aria-pressed={selected} aria-label={`${h.label}: show them in the table`} onClick={() => onSelect(h.key as Kind)}>{tile}</button>
  )
}

/** The arrow from one hop to the next carries the sender's state: it moves while data is arriving and is still, amber, red or grey dashed
 *  otherwise. Under it a thin freshness bar says how recent the data is; the age itself shows on hover or focus, and always when not fine. */
function Connector({ from, to, now }: { from: Hop; to: Hop; now: number }) {
  // Local operators that send straight to the central operator have a line of their own (DirectArc): this one is about the rest.
  const sender = from.onward ?? from
  // Data into FUSION is judged by FUSION's own last data; every other line by the newest data its sender reports.
  const lastData = to.key === 'fusion' ? to.lastData : sender.lastData
  const f = freshness(lastData, now, EXPECTED_MS[from.key])
  const link = linkState(sender.state, sender.total, f)
  const measured = sender.total > 0 && to.total > 0
  const age = lastDataText(lastData, now)
  return (
    <li className={clsx('group relative h-10 xl:h-auto xl:min-w-14 xl:flex-1', TONE[link])} data-testid={`link-${from.key}`} data-link={link} aria-hidden={measured ? undefined : true}>
      <Arrow link={link} v />
      <Arrow link={link} v={false} />
      {measured && (
        <div className="absolute left-10 top-1/2 flex -translate-y-1/2 items-center gap-2 xl:left-1/2 xl:top-8 xl:-translate-x-1/2 xl:translate-y-0 xl:flex-col xl:gap-1">
          <MeterBar pct={f.fraction * 100} tone={BAR[f.level]} label={`Data from ${from.label} to ${to.label}`} valueText={`Last data ${age}`} title={`Last data ${age}`} tabIndex={0} className="w-10" />
          <span className={clsx('whitespace-nowrap text-[11px] tabular-nums', AGE_TEXT[link])}>{age}</span>
        </div>
      )}
    </li>
  )
}

/** Half of a node's width (`xl:w-36`): the arc starts and ends at the middle of its tiles. */
const HALF_NODE = 'mx-[4.5rem]'

/**
 * Local operators that send straight to the central operator, without a regional one: a second route that leaves the Local tile, passes over
 * the Regional one and enters the Central tile from above. It is coloured, counted and aged for its own senders, so a failure on the direct
 * route shows on this line and not on the regional one; clicking it lists those senders in the table.
 */
function DirectArc({ hop, now, active, onSelect, style }: { hop: Hop; now: number; active: boolean; onSelect: () => void; style: CSSProperties }) {
  const g = hop.direct!
  const f = freshness(g.lastData, now, EXPECTED_MS.local)
  const link = linkState(g.state, g.total, f)
  const age = lastDataText(g.lastData, now)
  const text = `${g.total} ${g.total === 1 ? 'sends' : 'send'} direct to central`
  return (
    <div className={clsx('group relative h-12', HALF_NODE, TONE[link])} style={style} data-testid="link-direct" data-link={link}>
      <svg viewBox="0 0 100 100" preserveAspectRatio="none" className="absolute inset-0 h-full w-full overflow-visible" aria-hidden focusable="false">
        <path d="M0 100L0 0L100 0L100 100" fill="none" stroke="currentColor" strokeWidth={2} strokeLinejoin="round" vectorEffect="non-scaling-stroke" className={link === 'flowing' ? 'flow-dash' : undefined} strokeDasharray={link === 'unknown' ? '5 4' : undefined} />
      </svg>
      <svg viewBox="0 0 10 10" className="absolute -bottom-px -right-[5px] size-2.5" aria-hidden focusable="false">
        <path d="M1 2L5 8L9 2" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <button
        type="button"
        onClick={onSelect}
        aria-pressed={active}
        aria-label={`${text}: show them in the table`}
        className={clsx('absolute left-1/2 top-0 flex -translate-x-1/2 -translate-y-1/2 items-center gap-2 whitespace-nowrap rounded-full border bg-nb-910 px-3 py-1 text-xs text-nb-300 transition-colors', active ? 'border-accent/50 ring-1 ring-accent/40' : 'border-nb-850 hover:border-nb-800')}
        data-testid="pipeline-direct"
      >
        <span>{text}</span>
        {g.state && g.state !== 'healthy' && <StateChip state={g.state} count={g.worst} compact />}
        {g.total > 0 && <MeterBar pct={f.fraction * 100} tone={BAR[f.level]} label="Data sent direct to the central operator" valueText={`Last data ${age}`} title={`Last data ${age}`} className="w-10" />}
        <span className={clsx('text-[11px] tabular-nums', AGE_TEXT[link])}>{age}</span>
      </button>
    </div>
  )
}

/**
 * The path telemetry takes, as one connected strip: discovery agents, local operators, regional operators, the central operator, FUSION. Each
 * node says how many there are and the worst state among them, and chooses just its components in the table below (FUSION's opens its
 * section); each line between two carries the sender's state and how fresh its data is. Local operators that skip the regional hop get a
 * line of their own over it. Stacks vertically on a narrow screen.
 */
export default function PipelinePath({ hops, now, selected, onSelect, directActive = false, onSelectDirect }: { hops: Hop[]; now: number; selected: string; onSelect: (kind: Kind) => void; /** The table is already listing the direct senders. */ directActive?: boolean; onSelectDirect?: () => void }) {
  const localAt = hops.findIndex((h) => h.key === 'local')
  const centralAt = hops.findIndex((h) => h.key === 'central')
  const local = localAt >= 0 ? hops[localAt] : undefined
  // One grid for the strip and for the arc above it, so the arc's ends line up with the middle of its two tiles whatever the width.
  const columns = hops.flatMap((_, i) => (i === 0 ? ['9rem'] : ['minmax(3.5rem,1fr)', '9rem'])).join(' ')
  const direct = local?.direct && centralAt > localAt ? local : undefined
  return (
    <div className="mb-6" data-testid="pipeline-path-wrap">
      {direct && (
        <div className="mb-1 hidden xl:grid" style={{ gridTemplateColumns: columns }}>
          <DirectArc hop={direct} now={now} active={directActive} onSelect={() => onSelectDirect?.()} style={{ gridColumn: `${2 * localAt + 1} / ${2 * centralAt + 2}` }} />
        </div>
      )}
      <ol className="flex flex-col xl:grid" style={{ gridTemplateColumns: columns }} aria-label="Data path" data-testid="pipeline-path">
        {hops.map((h, i) => (
          <Fragment key={h.key}>
            {i > 0 && <Connector from={hops[i - 1]} to={h} now={now} />}
            <li className="min-w-0 xl:w-36 xl:shrink-0" data-testid={`hop-${h.key}`}>
              <Node h={h} selected={selected === h.key} onSelect={onSelect} />
            </li>
          </Fragment>
        ))}
      </ol>
      {direct?.direct && (
        // On a narrow screen the strip is a column and has no room for an arc: the same fact, as one line under it.
        <button type="button" onClick={() => onSelectDirect?.()} aria-pressed={directActive} className="mt-3 text-xs text-nb-400 underline-offset-2 hover:text-nb-300 hover:underline xl:hidden" data-testid="pipeline-direct-narrow">
          {direct.direct.total} {direct.direct.total === 1 ? 'local operator sends' : 'local operators send'} direct to central
        </button>
      )}
    </div>
  )
}
