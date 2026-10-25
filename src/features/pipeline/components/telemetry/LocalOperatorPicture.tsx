import clsx from 'clsx'
import { AlertTriangle, CheckCircle2, ChevronRight, HelpCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { ICON_SM } from '@/components/ui/primitives'
import type { AgentDiagnostics } from '@/lib/consent'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { SourceRead } from '@/features/pipeline/lib/operatorPicture'
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

const Arrow = () => (
  <div className="hidden items-center justify-center text-nb-500 md:flex" aria-hidden>
    <svg width="26" height="12" viewBox="0 0 40 14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M2 7h34" /><path d="M30 2l6 5-6 5" /></svg>
  </div>
)

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
            <div className="grid gap-3 md:grid-cols-[minmax(0,1.5fr)_auto_minmax(0,1fr)_auto_minmax(0,1.2fr)]">
              <div className="space-y-3" data-testid={`${testId}-sources`}>
                {pic.groups.map((g) => (
                  <div key={g.scope} data-testid={`${testId}-group-${g.scope}`}>
                    <div className="text-[11px] font-semibold uppercase tracking-wide text-nb-400">{g.title} <span className="font-normal normal-case tracking-normal text-nb-500">· {g.sub}</span></div>
                    <ul className="mt-1 flex flex-col gap-1">
                      {g.sources.map((s) => (
                        <li key={s.id} title={s.what} className="rounded-lg border border-nb-850 bg-nb-950 px-2.5 py-1.5" data-testid={`${testId}-source-${s.id}`} data-read={s.read?.state}>
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
              <Arrow />
              <div className="self-center rounded-lg border border-nb-800 bg-nb-950 px-3 py-2.5" data-testid={`${testId}-collectors`} data-health={pic.collectors.health}>
                <div className="text-sm font-medium text-nb-300">Collectors</div>
                <div className="text-xs text-nb-500">OpenTelemetry, installed with this agent</div>
                <div className="mt-1.5 flex items-start gap-1.5 text-xs text-nb-400">
                  <HealthWord health={pic.collectors.health} />
                </div>
                <div className="mt-0.5 text-xs text-nb-400" data-testid={`${testId}-collectors-text`}>{pic.collectors.text}</div>
              </div>
              <Arrow />
              <div className="self-center" data-testid={`${testId}-lanes`}>
                <div className="text-[11px] font-semibold uppercase tracking-wide text-nb-400">Goes out to <span className="font-normal normal-case tracking-normal text-nb-300" data-testid={`${testId}-destination`}>{pic.destination ?? 'its destination'}</span></div>
                {pic.lanes.length === 0 ? (
                  <p className="mt-1 text-xs text-nb-500" data-testid={`${testId}-no-lanes`}>What goes out is not known: {pic.collectors.text.toLowerCase()}.</p>
                ) : (
                  <ul className="mt-1 flex flex-col gap-1">
                    {pic.lanes.map((r) => (
                      <li key={r.modality} className="rounded-lg border border-nb-850 bg-nb-950 px-2.5 py-1.5" data-testid={`${testId}-lane-${r.modality}`} data-health={r.health}>
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
            State comes from the collectors&apos; own counters: per kind of data going out, and per source where the collectors read it themselves (the kubelet, the host, the Kubernetes API, the logs). Energy and GPU tools are not read per tool yet. Which tool reads each source is the cluster owner&apos;s to set in Helm.
          </p>
        </div>
      )}
    </div>
  )
}
