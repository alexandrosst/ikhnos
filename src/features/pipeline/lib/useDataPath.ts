import { useEffect, useMemo, useState } from 'react'
import { buildComponents, buildHops, type Hop } from '@/features/pipeline/lib/operatorsView'
import { useOperators, useTelemetryIntents } from '@/lib/useOperators'
import { useServer } from '@/store/server'
import { useTopology } from '@/store/topology'

/** The hops of the data path as the Pipeline page counts them, so that the picture on the FUSION page (which is given them) and the Pipeline
 *  page cannot say different things about the operators that send in. */
export function useDataPath(enabled: boolean): Hop[] {
  const rawAgents = useServer((s) => s.state?.agents)
  const { agents, clusters } = useTopology()
  const { operators } = useOperators(enabled)
  const { intents } = useTelemetryIntents(enabled)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 10_000)
    return () => clearInterval(t)
  }, [])
  return useMemo(() => buildHops(buildComponents({ agents, clusters, rawAgents, intents, operators, known: operators, now }), undefined), [agents, clusters, rawAgents, intents, operators, now])
}
