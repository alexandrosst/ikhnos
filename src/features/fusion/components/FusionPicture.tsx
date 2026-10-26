import clsx from 'clsx'
import { useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { StatusChip } from '@/features/fusion/components/StatusChip'
import { DiskMeter, FillsIn } from '@/features/fusion/components/StoreBars'
import { type FusionPictureModel, type PictureEdge, type PictureNode } from '@/features/fusion/lib/fusionPicture'
import { LINK_TONE } from '@/lib/linkTone'

const HEAD = 5 // half the width of an arrowhead, and its length
const TIP_GAP = 3 // how far the tip stops short of the box it points at

/** The arrows: measured from the boxes' own rectangles, so they follow wherever the layout puts them, and drawn only where there is room
 *  for columns (below `md` the boxes stack and the list under the picture says all of it). */
function Edges({ edges }: { edges: PictureEdge[] }) {
  const svg = useRef<SVGSVGElement>(null)
  const [drawn, setDrawn] = useState<Record<string, { d: string; x: number; y: number }>>({})
  const signature = edges.map((e) => `${e.from}>${e.to}`).join('|')
  useLayoutEffect(() => {
    const root = svg.current?.parentElement
    if (!root) return
    const measure = () => {
      const o = root.getBoundingClientRect()
      const out: Record<string, { d: string; x: number; y: number }> = {}
      for (const e of edges) {
        const a = root.querySelector(`[data-node="${e.from}"]`)?.getBoundingClientRect()
        const b = root.querySelector(`[data-node="${e.to}"]`)?.getBoundingClientRect()
        if (!a || !b) continue
        const [x1, y1, x2, y2] = [a.right - o.left + 2, (a.top + a.bottom) / 2 - o.top, b.left - o.left - TIP_GAP, (b.top + b.bottom) / 2 - o.top]
        const dx = Math.max(24, (x2 - x1) * 0.55)
        out[`${e.from}>${e.to}`] = { d: `M${x1} ${y1}C${x1 + dx} ${y1} ${x2 - dx} ${y2} ${x2} ${y2}`, x: x2, y: y2 }
      }
      setDrawn(out)
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(root)
    return () => ro.disconnect()
    // The edges are said by their signature: a new array every render must not measure every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature])
  return (
    <svg ref={svg} className="pointer-events-none absolute inset-0 hidden h-full w-full overflow-visible md:block" aria-hidden focusable="false">
      {edges.map((e) => {
        const key = `${e.from}>${e.to}`
        const g = drawn[key]
        return (
          <g key={key} className={LINK_TONE[e.link]} data-testid={`fusion-edge-${e.from}-${e.to}`} data-link={e.link} fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            {g && (
              <>
                <path d={g.d} className={e.link === 'flowing' ? 'flow-dash' : undefined} strokeDasharray={e.link === 'unknown' ? '5 4' : undefined} />
                <path d={`M${g.x - HEAD} ${g.y - HEAD + 1}L${g.x} ${g.y}L${g.x - HEAD} ${g.y + HEAD - 1}`} />
              </>
            )}
          </g>
        )
      })}
    </svg>
  )
}

function Box({ n, big }: { n: PictureNode; big?: boolean }) {
  return (
    <div className={clsx('rounded-xl border bg-nb-925 px-3.5 py-2.5', big ? 'border-nb-800 py-3' : 'border-nb-850')} data-node={n.id} data-testid={`fusion-node-${n.id}`} data-health={n.health}>
      <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
        <span className="min-w-0 text-sm font-medium text-nb-300">{n.to ? <Link to={n.to} className="hover:text-accent hover:underline">{n.title}</Link> : n.title}</span>
        <StatusChip status={n.health} />
      </div>
      <p className="mt-0.5 text-xs text-nb-500">{n.note}</p>
      {n.more && <p className="mt-1 break-words text-xs text-nb-400">{n.more}</p>}
      {n.store && <><DiskMeter s={n.store} idPrefix="fusion-flow" /><div className="mt-1"><FillsIn s={n.store} idPrefix="fusion-flow" /></div></>}
    </div>
  )
}

const COL = 'text-[11px] font-semibold uppercase tracking-wide text-nb-500'

/** How data goes through FUSION, drawn above the list of its parts: the operators that send in, the central operator, the three stores by kind,
 *  and Grafana reading them. The list under it keeps the details; this says in a look which part holds up which. */
export function FusionPicture({ model }: { model: FusionPictureModel }) {
  const cols = model.grafana ? 'md:grid-cols-[minmax(0,1.15fr)_minmax(2.5rem,.3fr)_minmax(0,1.2fr)_minmax(2.5rem,.3fr)_minmax(0,1.3fr)_minmax(2.5rem,.25fr)_minmax(0,.9fr)]' : 'md:grid-cols-[minmax(0,1.15fr)_minmax(2.5rem,.3fr)_minmax(0,1.2fr)_minmax(2.5rem,.3fr)_minmax(0,1.3fr)]'
  const gaps = (id: string) => <div className="hidden md:block" aria-hidden data-gap={id} />
  return (
    <div className="rounded-xl border border-nb-850 bg-nb-925/50 p-4" data-testid="fusion-picture">
      <div className={clsx('relative grid items-center gap-3 md:gap-0', cols)}>
        <div className="space-y-2.5">
          <p className={COL}>Comes in from</p>
          {model.senders.map((n) => <Box key={n.id} n={n} />)}
        </div>
        {gaps('a')}
        <div className="space-y-2.5">
          <p className={COL}>The way in</p>
          <Box n={model.central} big />
        </div>
        {gaps('b')}
        <div className="space-y-2.5">
          <p className={COL}>Where it is kept</p>
          {model.stores.map((n) => <Box key={n.id} n={n} />)}
        </div>
        {model.grafana && (
          <>
            {gaps('c')}
            <div className="space-y-2.5">
              <p className={COL}>Read with</p>
              <Box n={model.grafana} />
            </div>
          </>
        )}
        <Edges edges={model.edges} />
      </div>
      <p className="mt-3 text-xs text-nb-500">
        Green and moving: the part is up and data is arriving. Grey: not known. Amber: held up. Red: not working. The arrows into the stores follow each store's own state; how much went to which is not measured.
      </p>
    </div>
  )
}
