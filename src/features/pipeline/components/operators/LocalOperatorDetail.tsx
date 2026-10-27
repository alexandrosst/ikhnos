import { Cable, Settings2 } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import StateChip from '@/features/pipeline/components/operators/StateChip'
import { HealthWord } from '@/features/pipeline/components/telemetry/ArrivalCheck'
import LocalOperatorPicture from '@/features/pipeline/components/telemetry/LocalOperatorPicture'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { ComponentRow } from '@/features/pipeline/lib/operatorsView'
import { buttonClass } from '@/components/ui/buttonClass'
import { Button, ICON_SM } from '@/components/ui/primitives'
import type { AgentExtras } from '@/lib/consent'
import { TELEMETRY_SIGNALS } from '@/lib/install'
import { toolLabel } from '@/lib/toolVersions'

/**
 * What one local operator does, in the Pipeline page, where the rest of the path is: its state and what to do about it, whether data is
 * arriving, the picture of what its collectors take in and where it goes out, and what is installed. Drawn from what the agent reports
 * (the same numbers as the table's row), so it never says anything the row does not.
 */
export default function LocalOperatorDetail({ row, extras, connected, namespace, now, canConsent, onChange }: {
  row: ComponentRow
  extras: AgentExtras
  connected?: boolean
  namespace?: string
  now: number
  canConsent: boolean
  /** Opens the guided change of what it sends. */
  onChange: () => void
}) {
  const d = extras.diagnostics
  const installed = d?.installedTelemetry ?? []
  const tools = d?.installedTelemetryConfig?.tools ?? []
  const pic = operatorPicture({ installed, diagnostics: d, connected, now, cluster: row.cluster?.name ?? 'the cluster', namespace })
  const { state, reason } = row.verdict
  const box = useRef<HTMLDivElement>(null)
  // Opened from a link or a click far down a long list: bring it into view once.
  useEffect(() => { box.current?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' }) }, [])
  return (
    <div ref={box} className="space-y-5 border-y border-nb-850 bg-nb-950/60 px-5 py-5" data-testid="local-operator-detail" data-state={state}>
        <h2 className="text-sm font-medium text-nb-200">{row.name} <span className="font-normal text-nb-500">· what it collects, and where it goes</span></h2>
        <section className="flex flex-wrap items-start gap-x-3 gap-y-2" data-testid="local-operator-state">
          <StateChip state={state} />
          <p className="min-w-0 flex-1 basis-60 text-sm text-nb-400">{reason ?? (state === 'healthy' ? 'Its collectors are reading what they are asked to and the data is leaving.' : '')}</p>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" onClick={onChange} disabled={!canConsent} title={canConsent ? undefined : 'Changing telemetry needs permission to edit'} data-testid="local-operator-change">
              <Settings2 size={ICON_SM} aria-hidden /> Change what it sends…
            </Button>
            {row.agentId && <Link to={`/agents?open=${encodeURIComponent(row.agentId)}`} className={buttonClass('secondary', 'sm')} data-testid="local-operator-agent"><Cable size={ICON_SM} aria-hidden /> Its discovery agent</Link>}
          </div>
        </section>

        {installed.length > 0 && (
          <section aria-labelledby="lod-arrive" data-testid="local-operator-arrival">
            <h3 id="lod-arrive" className="mb-1 text-sm font-medium text-nb-300">Is data arriving?</h3>
            <div className="flex flex-wrap items-start gap-x-2 text-sm text-nb-400">
              <HealthWord health={pic.arrival.health} />
              <span className="min-w-0 flex-1 basis-60">{pic.arrival.headline}{pic.arrival.todo ? ` ${pic.arrival.todo}` : ''}</span>
            </div>
          </section>
        )}

        <section aria-label="What it collects, and where it goes">

          <LocalOperatorPicture bare installed={installed} diagnostics={d} connected={connected} cluster={row.cluster?.name ?? 'the cluster'} namespace={namespace} keptByController={extras.keptByController} testId="operator-picture" />
        </section>

        {installed.length > 0 && (
          <section aria-labelledby="lod-installed">
            <h3 id="lod-installed" className="mb-1.5 text-sm font-medium text-nb-300">Installed now</h3>
            <ul className="flex flex-wrap gap-1.5" data-testid="local-operator-installed">
              {installed.map((id) => {
                const s = TELEMETRY_SIGNALS.find((x) => x.id === id)
                return <li key={id} title={s?.what} className="rounded border border-nb-800 bg-nb-930 px-1.5 py-0.5 text-xs text-nb-300">{s?.label ?? id}</li>
              })}
            </ul>
            {tools.length > 0 && (
              <div className="mt-3" data-testid="local-operator-tools">
                <h4 className="mb-1 text-xs font-medium uppercase tracking-wide text-nb-500">Tools, as installed</h4>
                <ul className="flex flex-wrap gap-1.5">
                  {tools.map((t) => {
                    const l = toolLabel(t)
                    return (
                      <li key={t.name} className="rounded border border-nb-800 bg-nb-930 px-1.5 py-0.5 text-xs text-nb-300" data-tool={t.name}>
                        <span className="text-nb-500">{l.name}</span> <span className="font-mono">{l.version}</span>{l.engine && <span className="text-nb-500"> · {l.engine}</span>}
                      </li>
                    )
                  })}
                </ul>
                <p className="mt-1 text-xs text-nb-500">The versions its release was installed with; they are not read from the running pods.</p>
              </div>
            )}
          </section>
        )}
    </div>
  )
}
