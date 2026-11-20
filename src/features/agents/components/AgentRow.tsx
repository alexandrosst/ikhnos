import { ChevronRight } from 'lucide-react'
import { Link } from 'react-router-dom'
import { DiscoveryChip, HealthChip } from '@/features/agents/components/AgentInsight'
import { DiagnoseButton } from '@/features/agents/components/DiagnoseDrawer'
import { ApprovalStatus, when } from '@/features/discovery/components/AgentParts'
import { DistroIcon, WithIcon } from '@/components/ui/brand'
import { LiveDot, ICON_SM, TierBadge, Td } from '@/components/ui/primitives'
import { StatusBadge } from '@/components/ui/Status'
import { HEALTH, diagnosisTone, num, tierOf } from '@/features/agents/lib/agentView'
import type { AgentHealth } from '@/features/agents/lib/agentHealth'
import { skewLabel, skewWarning } from '@/lib/clock'
import type { AgentExtras } from '@/lib/consent'
import { ageOf } from '@/lib/history'
import { bytesTotal } from '@/lib/observed'
import type { Agent, Cluster, Site, Tier } from '@/lib/types'

/**
 * One agent as a row of the table: its name and version, the cluster it runs in, whether it is alive and when it last said so, what it has
 * sent, its access, its approval, and the Diagnose button. Pressing the row opens its detail (the page draws that, under the row).
 */
export function AgentRow({ agent: a, health: h, cluster: c, site: s, extras, expanded, now, onToggle, onDiagnose }: {
  agent: Agent
  health: AgentHealth
  cluster: Cluster | undefined
  site: Site | undefined
  extras: AgentExtras
  expanded: boolean
  now: number
  onToggle: () => void
  onDiagnose: () => void
}) {
  const st = HEALTH[h]
  return (
  <tr
    id={`agent-${a.id}`}
    className="group cursor-pointer hover:bg-nb-930/60"
    onClick={onToggle}
    role="button"
    tabIndex={0}
    aria-expanded={expanded}
    onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onToggle() } }}
    data-testid="agent-row"
  >
    <Td valign="top">
      <div className="flex items-center gap-2 whitespace-nowrap font-medium text-nb-300">
        <ChevronRight size={ICON_SM} className="chevron-turn text-nb-500" data-open={expanded} aria-hidden />
        {a.name}
      </div>
      <div className="pl-[22px] text-xs text-nb-500">v{a.version}{a.kubernetesVersion ? ` · Kubernetes ${a.kubernetesVersion}` : ''}</div>
    </Td>
    <Td valign="top">
      {c ? (
        <>
          <Link to={`/topology?sel=${encodeURIComponent(`cluster:${c.id}`)}`} onClick={(e) => e.stopPropagation()} className="hover:text-nb-300 hover:underline">
            <WithIcon icon={<DistroIcon distribution={c.distribution} size={ICON_SM} />}>{c.name}</WithIcon>
          </Link>
          <div className="mt-0.5 flex items-center gap-2 text-xs text-nb-500"><TierBadge tier={c.tier as Tier} />{s?.name ?? c.region}</div>
          {a.scope && a.scope.inScope < a.scope.namespaces && (
            <div className="mt-0.5 text-xs text-warn" title={a.scope.description} data-testid="scope-badge">{a.scope.inScope} of {a.scope.namespaces} namespaces</div>
          )}
        </>
      ) : (
        <span className="text-nb-500">no cluster yet</span>
      )}
    </Td>
    <Td valign="top">
      <div className="flex items-center gap-2 whitespace-nowrap">
        <LiveDot kind={st.dot} />
        <span className={st.text}>{st.label}</span>
      </div>
      <div className="text-xs text-nb-500" title={a.lastHeartbeat ? when(a.lastHeartbeat) : undefined}>
        {a.lastHeartbeat ? `heartbeat ${ageOf(a.lastHeartbeat)}` : 'no heartbeat yet'}
        {a.link?.connectedSince && ` · up ${ageOf(a.link.connectedSince).replace(' ago', '')}`}
      </div>
      {a.status === 'approved' && extras.diagnostics && (
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          <HealthChip diagnostics={extras.diagnostics} connected={a.connected} />
          <DiscoveryChip diagnostics={extras.diagnostics} />
        </div>
      )}
      {skewWarning(a.clockSkewMs) && a.clockSkewMs !== undefined && (
        <StatusBadge tone="attention" label={skewLabel(a.clockSkewMs)} title={skewWarning(a.clockSkewMs)} data-testid="clock-chip" className="mt-1" />
      )}
    </Td>
    <Td valign="top" className="whitespace-nowrap tabular-nums">
      {a.link ? (
        <>
          <div className="text-nb-300">{bytesTotal(a.link.bytes)}</div>
          <div className="text-xs text-nb-500">{num(a.link.syncs + a.link.flows + a.link.measurements + a.link.heartbeats)} messages</div>
        </>
      ) : (
        <span className="text-nb-600">—</span>
      )}
    </Td>
    <Td valign="top" className="whitespace-nowrap" title={`Tier ${a.accessTier}`}>{tierOf(a)}</Td>
    <Td valign="top">
      <ApprovalStatus status={a.status} />
    </Td>
    <Td valign="top" className="whitespace-nowrap text-right">
      <DiagnoseButton onClick={onDiagnose} tone={diagnosisTone(a, extras, now)} />
    </Td>
  </tr>
  )
}
