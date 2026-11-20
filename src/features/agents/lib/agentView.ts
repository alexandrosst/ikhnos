import { diagnoseAgent } from '@/lib/diagnose'
import type { AgentExtras } from '@/lib/consent'
import type { AgentHealth } from '@/features/agents/lib/agentHealth'
import type { LiveKind } from '@/components/ui/primitives'
import { ACCESS_TIERS, type Agent } from '@/lib/types'

export const HEALTH: Record<AgentHealth, { label: string; dot: LiveKind; text: string }> = {
  ok: { label: 'Connected', dot: 'online', text: 'text-ok' },
  late: { label: 'Heartbeat late', dot: 'late', text: 'text-warn' },
  offline: { label: 'Disconnected', dot: 'offline', text: 'text-bad' },
  waiting: { label: 'Waiting for approval', dot: 'starting', text: 'text-warn' },
  expired: { label: 'Request expired', dot: 'idle', text: 'text-nb-400' },
  gone: { label: 'Revoked', dot: 'idle', text: 'text-nb-500' },
  unknown: { label: 'Not live', dot: 'idle', text: 'text-nb-400' },
}
/** Milliseconds until a certificate expires (negative once it has). */
/** An agent renews its short-lived certificate well before this, so under it, and still connected, is worth a look (the same line the diagnosis draws). */
export const CERT_SOON = 2 * 3_600_000
export const msUntil = (iso?: string, now = Date.now()) => (iso ? new Date(iso).getTime() - now : undefined)
/** Agent certificates are short-lived and renewed by the agent itself, so hours are the honest unit. */
export const inWords = (ms: number) => (ms < 3_600_000 ? `${Math.max(1, Math.round(ms / 60_000))} min` : ms < 48 * 3_600_000 ? `${Math.round(ms / 3_600_000)} h` : `${Math.round(ms / 86_400_000)} d`)
export const tierOf = (a: Agent) => ACCESS_TIERS.find((t) => t.value === a.accessTier)?.label ?? `Tier ${a.accessTier}`
/** "now", "5m", "3h", "2d": for places with no room for a sentence. */
export const shortAge = (iso: string, now = Date.now()) => {
  const s = Math.max(0, (now - new Date(iso).getTime()) / 1000)
  return s < 90 ? 'now' : s < 5400 ? `${Math.round(s / 60)}m ago` : s < 129600 ? `${Math.round(s / 3600)}h ago` : `${Math.round(s / 86400)}d ago`
}
export const num = (n: number) => n.toLocaleString()
/** The tone beside the Diagnose button: nothing for an agent that is well, so a quiet table stays quiet. */
export const diagnosisTone = (a: Agent, extras: AgentExtras, now: number) => diagnoseAgent(a, extras, now).verdict
