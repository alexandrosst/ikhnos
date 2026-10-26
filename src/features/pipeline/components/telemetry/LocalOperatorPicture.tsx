import clsx from 'clsx'
import { AlertTriangle, CheckCircle2, ChevronRight, HelpCircle } from 'lucide-react'
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { ICON_SM } from '@/components/ui/primitives'
import type { AgentDiagnostics } from '@/lib/consent'
import { operatorPicture, sourceLink } from '@/features/pipeline/lib/operatorPicture'
import type { SourceRead } from '@/features/pipeline/lib/operatorPicture'
import { LINK_TONE, type LinkState } from '@/features/pipeline/lib/pipelineFlow'
import type { Health } from '@/lib/status'
import { HealthWord } from './ArrivalCheck'

/** How a source is being read, in its own words: the glyph and colour of its health, never colour alone. */
function ReadWord({ read, testId }: { read: SourceRead; testId: string }) {
  const Icon = read.health === 'attention' ? AlertTriangle : read.health === 'unknown' ? HelpCircle : CheckCircle2
  const tone = read.health === 'attention' ? 'text-warn' : read.health === 'unknown' ? 'text-nb-500' : 'text-ok'
  return (
    <span className={clsx('inline-flex shrink-0 items-center gap-1 text-xs font-medium', tone)} data-testid={testId}>
      <Icon size={ICON_SM} aria-hidden />
      {read.word}
    </span>
  )
}

/** The line a lane of what goes out is drawn with: the lane's own health, in the pipeline's words for a link. */
const laneLink = (h: Health): LinkState => (h === 'healthy' ? 'flowing' : h === 'down' ? 'broken' : h === 'attention' ? 'stalled' : 'unknown')

interface EdgeSpec {
  /** The data-edge value of the card the arrow starts at (a source) or ends at (a lane). */
  key: string
  testId: string
  link: LinkState
  /** A source's arrow runs into the collectors; a lane's runs out of them. */
  side: 'in' | 'out'
  /** Which of the `count` arrows on its side this is, top to bottom: each lands (or leaves) at a point of its own on the box. */
  index: number
  count: number
}

const HEAD = 5 // half the width of an arrowhead, and its length
const TIP_GAP = 3 // how far the tip stops short of the box it points at
const STEP = 12 // the distance between the points where neighbouring arrows meet the collectors' box

/**
 * The arrows between the cards and the collectors' box, drawn over the grid: each one leaves the side of its card, bends, and arrives level at
 * the box with an arrowhead that shares its stroke. They are measured from the cards as they are laid out, so they follow whatever height a
 * source's sentence gives it; below a wide screen the cards are stacked and nothing is drawn.
 */
function Edges({ specs }: { specs: EdgeSpec[] }) {
  // The grid is the svg's parent: it is what the arrows are measured against, and it is there when this runs (a ref to it from the caller is not yet).
  const svg = useRef<SVGSVGElement>(null)
  const [drawn, setDrawn] = useState<Record<string, { d: string; x: number; y: number }>>({})
  const signature = specs.map((e) => `${e.key}:${e.side}:${e.index}/${e.count}`).join('|')
  useLayoutEffect(() => {
    const root = svg.current?.parentElement
    if (!root) return
    const measure = () => {
      const o = root.getBoundingClientRect()
      const box = root.querySelector('[data-edge-box]')?.getBoundingClientRect()
      if (!box) return
      const out: Record<string, { d: string; x: number; y: number }> = {}
      for (const e of specs) {
        const el = root.querySelector(`[data-edge="${e.key}"]`)?.getBoundingClientRect()
        if (!el) continue
        const span = Math.min(box.height - 24, STEP * (e.count - 1))
        const meet = (box.top + box.bottom) / 2 - o.top - span / 2 + (e.count > 1 ? (span * e.index) / (e.count - 1) : 0)
        const [x1, y1, x2, y2] = e.side === 'in'
          ? [el.right - o.left + 2, (el.top + el.bottom) / 2 - o.top, box.left - o.left - TIP_GAP, meet]
          : [box.right - o.left + 2, meet, el.left - o.left - TIP_GAP, (el.top + el.bottom) / 2 - o.top]
        const dx = Math.max(24, (x2 - x1) * 0.55)
        out[e.key] = { d: `M${x1} ${y1}C${x1 + dx} ${y1} ${x2 - dx} ${y2} ${x2} ${y2}`, x: x2, y: y2 }
      }
      setDrawn(out)
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(root)
    return () => ro.disconnect()
    // The specs are said by their signature: a new array every render must not measure every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature])
  return (
    <svg ref={svg} className="pointer-events-none absolute inset-0 hidden h-full w-full overflow-visible md:block" aria-hidden focusable="false">
      {specs.map((e) => {
        const g = drawn[e.key]
        const moving = e.link === 'flowing'
        return (
          <g key={`${e.side}-${e.key}`} className={LINK_TONE[e.link]} data-testid={e.testId} data-link={e.link} fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            {g && (
              <>
                <path d={g.d} className={moving ? 'flow-dash' : undefined} strokeDasharray={e.link === 'unknown' ? '5 4' : undefined} />
                <path d={`M${g.x - HEAD} ${g.y - HEAD + 1}L${g.x} ${g.y}L${g.x - HEAD} ${g.y + HEAD - 1}`} />
              </>
            )}
          </g>
        )
      })}
    </svg>
  )
}

/**
 * The local operator of one agent as a picture: what its collectors take in, the collectors, and where each kind of data goes out with its
 * state. Closed by default, a line says what it is; it opens by itself when something along the path needs a look, and a person's own
 * click is never fought after that. Drawn from what the agent reports (see operatorPicture), so it is the same state as "Is data arriving?".
 */
export default function LocalOperatorPicture({ installed, diagnostics, connected, cluster, namespace, keptByController, openOnArrival = false, testId = 'operator-picture', now: fixedNow }: {
  installed: string[]
  diagnostics?: AgentDiagnostics
  connected?: boolean
  cluster: string
  namespace?: string
  /** A controller keeps this agent's release: said under the picture, since it is who applies a change. */
  keptByController?: boolean
  /** Open at first, because this is what a link asked to see ("What it collects here"); a person's click still wins afterwards. */
  openOnArrival?: boolean
  testId?: string
  /** Pins the clock, for a test. */
  now?: number
}) {
  const [clock, setClock] = useState(() => Date.now())
  useEffect(() => {
    if (fixedNow !== undefined) return
    const t = setInterval(() => setClock(Date.now()), 15_000)
    return () => clearInterval(t)
  }, [fixedNow])
  const pic = operatorPicture({ installed, diagnostics, connected, now: fixedNow ?? clock, cluster, namespace })
  // A person's own choice wins once made; until then it follows whether something needs a look.
  const [chosen, setChosen] = useState<boolean | undefined>(openOnArrival ? true : undefined)
  const open = chosen ?? pic.needsAttention
  const body = `${testId}-body`
  const specs = useMemo<EdgeSpec[]>(() => {
    const sources = pic.groups.flatMap((g) => g.sources)
    return [
      ...sources.map((x, i): EdgeSpec => ({ key: `src-${x.id}`, testId: `${testId}-source-${x.id}-edge`, link: sourceLink(x.read), side: 'in', index: i, count: sources.length })),
      ...pic.lanes.map((r, i): EdgeSpec => ({ key: `lane-${r.modality}`, testId: `${testId}-lane-${r.modality}-edge`, link: laneLink(r.health), side: 'out', index: i, count: pic.lanes.length })),
    ]
  }, [pic, testId])
  return (
    <div className="rounded-xl border border-nb-850 bg-nb-925" data-testid={testId} data-open={open} data-health={pic.health}>
      <button
        type="button"
        className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 text-left"
        aria-expanded={open}
        aria-controls={body}
        onClick={() => setChosen(!open)}
        data-testid={`${testId}-toggle`}
      >
        <ChevronRight size={ICON_SM} className={clsx('shrink-0 text-nb-500 transition-transform', open && 'rotate-90')} aria-hidden />
        <span className="text-sm font-medium text-nb-300">Local operator</span>
        <span className="min-w-0 flex-1 basis-48 truncate text-xs text-nb-400" data-testid={`${testId}-summary`}>{pic.summary}</span>
        {installed.length > 0 && <HealthWord health={pic.health} testId={`${testId}-state`} />}
      </button>
      {open && (
        <div id={body} className="space-y-3 border-t border-nb-850 px-4 py-3" data-testid={body}>
          {installed.length === 0 ? (
            <p className="text-xs text-nb-500">No telemetry signal is enabled in this install, so there is nothing for the collectors to take in. Turn signals on under Change telemetry.</p>
          ) : (
            <div
              className="relative grid gap-2 md:grid-cols-[minmax(0,1.5fr)_minmax(3.5rem,.5fr)_minmax(0,.9fr)_minmax(3.5rem,.5fr)_minmax(0,1.1fr)] md:gap-x-0 md:gap-y-1"
              style={{ '--rows': pic.groups.reduce((n, g) => n + 1 + g.sources.length, 0) } as CSSProperties}
            >
              <Edges specs={specs} />
              {/* On a wide screen every source is a row of its own and the collectors sit in the middle of all of them; the arrows are drawn over the gaps. */}
              <div className="contents" data-testid={`${testId}-sources`}>
                {pic.groups.map((g, gi) => (
                  <div key={g.scope} className="contents" data-testid={`${testId}-group-${g.scope}`}>
                    <div className={clsx('text-[11px] font-semibold uppercase tracking-wide text-nb-400 md:col-start-1', gi > 0 && 'md:mt-2')}>{g.title} <span className="font-normal normal-case tracking-normal text-nb-500">· {g.sub}</span></div>
                    <ul className="contents">
                      {g.sources.map((s) => (
                        <li key={s.id} title={s.what} data-edge={`src-${s.id}`} className="rounded-lg border border-nb-850 bg-nb-950 px-2.5 py-1.5 md:col-start-1 md:self-start" data-testid={`${testId}-source-${s.id}`} data-read={s.read?.state}>
                          <div className="flex items-center justify-between gap-2 text-sm text-nb-300">
                            <span>{s.label} <span className="text-xs text-nb-500">{s.modality}</span></span>
                            {s.read && <ReadWord read={s.read} testId={`${testId}-source-${s.id}-state`} />}
                          </div>
                          {s.read?.sentence && <p className="mt-0.5 text-xs text-nb-400" data-testid={`${testId}-source-${s.id}-why`}>{s.read.sentence}</p>}
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </div>
              <div className="flex items-center md:col-start-3 md:row-start-1 md:[grid-row-end:span_var(--rows)]">
                <div className="w-full rounded-lg border border-nb-800 bg-nb-950 px-3 py-2.5 md:min-h-[var(--box-min)]" style={{ '--box-min': `${Math.max(0, specs.filter((e) => e.side === 'in').length - 1) * (STEP + 2) + 24}px` } as CSSProperties} data-edge-box data-testid={`${testId}-collectors`} data-health={pic.collectors.health}>
                  <div className="text-sm font-medium text-nb-300">Collectors</div>
                  <div className="text-xs text-nb-500">OpenTelemetry, installed with this agent</div>
                  <div className="mt-1.5 flex items-start gap-1.5 text-xs text-nb-400">
                    <HealthWord health={pic.collectors.health} />
                  </div>
                  <div className="mt-0.5 text-xs text-nb-400" data-testid={`${testId}-collectors-text`}>{pic.collectors.text}</div>
                </div>
              </div>
              <div className="self-center md:col-start-5 md:row-start-1 md:[grid-row-end:span_var(--rows)]" data-testid={`${testId}-lanes`}>
                <div className="text-[11px] font-semibold uppercase tracking-wide text-nb-400">Goes out to <span className="font-normal normal-case tracking-normal text-nb-300" data-testid={`${testId}-destination`}>{pic.destination ?? 'its destination'}</span></div>
                {pic.lanes.length === 0 ? (
                  <p className="mt-1 text-xs text-nb-500" data-testid={`${testId}-no-lanes`}>What goes out is not known: {pic.collectors.text.toLowerCase()}.</p>
                ) : (
                  <ul className="mt-1 flex flex-col gap-1">
                    {pic.lanes.map((r) => (
                      <li key={r.modality} data-edge={`lane-${r.modality}`} className="rounded-lg border border-nb-850 bg-nb-950 px-2.5 py-1.5" data-testid={`${testId}-lane-${r.modality}`} data-health={r.health}>
                        <div className="flex flex-wrap items-center gap-x-2 text-sm text-nb-300">{r.label}<HealthWord health={r.health} /></div>
                        <div className="text-xs text-nb-400">{r.lastData}</div>
                        <div className="text-xs text-nb-500">{r.sentence}</div>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>
          )}
          <p className="text-xs text-nb-500" data-testid={`${testId}-note`}>
            {keptByController ? 'Kept by the Ikhnos controller in this cluster. ' : ''}
            State comes from the collectors&apos; own counters: per kind of data going out, and per source where the collectors read it themselves (the kubelet, the host, the Kubernetes API, the logs), for what applications push, whether the collector has taken any in, and for Kepler, DCGM and the cluster links whether each node's target answers the collector with data. That shows the tool is serving, not that its values are right. Where a controller keeps the release, it also counts the pods of each tool and of the collectors, which is how a pod held back on a node is seen. Which tool reads each source is the cluster owner&apos;s to set in Helm.
          </p>
        </div>
      )}
    </div>
  )
}
