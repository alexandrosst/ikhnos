import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { HealthWord } from '@/features/pipeline/components/telemetry/ArrivalCheck'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { AgentDiagnostics } from '@/lib/consent'

/**
 * The one line an agent's own page keeps about its telemetry's data path: how it is doing, in a sentence, and the way to the Pipeline page,
 * where the picture of what is collected and where it goes lives with the rest of the path.
 */
export default function DataPathLine({ installed, diagnostics, connected, cluster, namespace, agentId }: {
  installed: string[]
  diagnostics?: AgentDiagnostics
  connected?: boolean
  cluster: string
  namespace?: string
  agentId: string
}) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 15_000)
    return () => clearInterval(t)
  }, [])
  const pic = operatorPicture({ installed, diagnostics, connected, now, cluster, namespace })
  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-nb-400" data-testid="data-path-line" data-health={pic.health}>
      <HealthWord health={pic.health} />
      <span className="min-w-0 flex-1 basis-48">{pic.summary}</span>
      <Link to={`/pipeline?open=${encodeURIComponent(agentId)}`} className="text-accent hover:underline" data-testid="data-path-link">See its data path</Link>
    </div>
  )
}
