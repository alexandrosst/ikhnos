import { Settings2 } from 'lucide-react'
import { Link } from 'react-router-dom'
import StateChip from '@/features/pipeline/components/operators/StateChip'
import { HealthWord } from '@/features/pipeline/components/telemetry/ArrivalCheck'
import LocalOperatorPicture from '@/features/pipeline/components/telemetry/LocalOperatorPicture'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { ComponentRow } from '@/features/pipeline/lib/operatorsView'
import { Button, Modal, ICON_SM } from '@/components/ui/primitives'
import type { AgentExtras } from '@/lib/consent'
import { TELEMETRY_SIGNALS } from '@/lib/install'

/**
 * What one local operator does, in the Pipeline page, where the rest of the path is: its state and what to do about it, whether data is
 * arriving, the picture of what its collectors take in and where it goes out, and what is installed. Drawn from what the agent reports
 * (the same numbers as the table's row), so it never says anything the row does not.
 */
export default function LocalOperatorDetail({ row, extras, connected, namespace, now, canConsent, onChange, onClose }: {
  row: ComponentRow
  extras: AgentExtras
  connected?: boolean
  namespace?: string
  now: number
  canConsent: boolean
  /** Opens the guided change of what it sends. */
  onChange: () => void
  onClose: () => void
}) {
  const d = extras.diagnostics
  const installed = d?.installedTelemetry ?? []
  const pic = operatorPicture({ installed, diagnostics: d, connected, now, cluster: row.cluster?.name ?? 'the cluster', namespace })
  const { state, reason } = row.verdict
  return (
    <Modal open side onClose={onClose} title={row.name} description={row.cluster ? `Local operator · ${row.cluster.name}` : 'Local operator'} width="max-w-4xl">
      <div className="space-y-5" data-testid="local-operator-detail" data-state={state}>
        <section className="flex flex-wrap items-start gap-x-3 gap-y-2" data-testid="local-operator-state">
          <StateChip state={state} />
          <p className="min-w-0 flex-1 basis-60 text-sm text-nb-400">{reason ?? (state === 'healthy' ? 'Its collectors are reading what they are asked to and the data is leaving.' : '')}</p>
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm" onClick={onChange} disabled={!canConsent} title={canConsent ? undefined : 'Changing telemetry needs permission to edit'} data-testid="local-operator-change">
              <Settings2 size={ICON_SM} aria-hidden /> Change what it sends…
            </Button>
            {row.agentId && <Link to={`/agents?open=${encodeURIComponent(row.agentId)}`} className="text-xs text-accent hover:underline" data-testid="local-operator-agent">Its agent</Link>}
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

        <section aria-labelledby="lod-picture">
          <h3 id="lod-picture" className="mb-2 text-sm font-medium text-nb-300">What it collects, and where it goes</h3>
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
          </section>
        )}
      </div>
    </Modal>
  )
}
