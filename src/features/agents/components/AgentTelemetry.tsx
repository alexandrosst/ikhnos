import { ArrowRight } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { buttonClass } from '@/components/ui/buttonClass'
import { ICON_SM } from '@/components/ui/primitives'
import { HealthWord } from '@/features/pipeline/components/telemetry/ArrivalCheck'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { AgentDiagnostics } from '@/lib/consent'
import type { Agent } from '@/lib/types'

/**
 * What an agent's own page says of its telemetry: whether it sends any and how that is going, and the way to the Pipeline page, which is where
 * the picture of what is collected and where it goes lives, and where it is set up and changed. Nothing is changed from here, so each thing has
 * one place: this page is for who the agent is and what it may read, Pipeline is for what is sent.
 */
export function AgentTelemetry({ agent: a, diagnostics: d, canConsent }: { agent: Agent; diagnostics?: AgentDiagnostics; canConsent: boolean }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 15_000)
    return () => clearInterval(t)
  }, [])
  const signals = d?.installedTelemetry ?? []
  const health = signals.length > 0 ? operatorPicture({ installed: signals, diagnostics: d, connected: a.connected, now, cluster: a.name, namespace: a.namespace }).health : undefined
  const link = (to: string, label: string, testId: string) => (
    <Link to={to} className={buttonClass('secondary', 'sm', 'mt-2 inline-flex items-center gap-1.5')} data-testid={testId}>
      {label} <ArrowRight size={ICON_SM} aria-hidden />
    </Link>
  )
  return (
    <div data-testid="agent-telemetry">
      <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-nb-500">Telemetry</div>
      {signals.length > 0 ? (
        <>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            {health && health !== 'unknown' && <HealthWord health={health} />}
            <span className="text-nb-300" data-testid="agent-telemetry-count">{signals.length} signal{signals.length === 1 ? '' : 's'}</span>
          </div>
          {link(`/pipeline?open=${encodeURIComponent(a.id)}`, 'Open in Pipeline', `open-in-pipeline-${a.id}`)}
        </>
      ) : (
        <>
          <p className="text-nb-500">{d ? 'Nothing is sent from this cluster yet.' : 'Not known until the agent has reported.'}</p>
          {canConsent && link(`/pipeline?setup=${encodeURIComponent(a.id)}`, 'Set up in Pipeline', `setup-in-pipeline-${a.id}`)}
        </>
      )}
    </div>
  )
}
