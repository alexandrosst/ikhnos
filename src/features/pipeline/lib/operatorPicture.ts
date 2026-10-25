import type { AgentDiagnostics } from '../../../lib/consent'
import { exportReach } from '../../../lib/exportHealth'
import { TELEMETRY_SIGNALS, type Modality } from '../../../lib/install'
import { type Health, worstOf } from '../../../lib/status'
import { arrivalCheck, type ArrivalRow } from './telemetrySetup'

/**
 * The local operator in one picture: what its collectors take in, the collectors themselves, and where each kind of data goes out, with the
 * state of each stage. It is drawn only from what the agent already reports (the signals the chart runs, and the collectors' own counters per
 * signal type), so it says nothing about a single tool: a source is shown on or off, and the state sits on the stage that is known.
 */

export type PictureScope = 'node' | 'cluster' | 'application'

export interface PictureSource {
  id: string
  label: string
  modality: Modality
  /** One sentence on what it collects, for the hover text. */
  what: string
}

export interface PictureGroup {
  scope: PictureScope
  title: string
  /** Where these collectors run, in the picture's words. */
  sub: string
  sources: PictureSource[]
}

export interface PictureCollectors {
  health: Health
  /** What is known about the collectors, in one line. */
  text: string
}

export interface OperatorPicture {
  health: Health
  /** The one line shown while the picture is closed. */
  summary: string
  groups: PictureGroup[]
  collectors: PictureCollectors
  lanes: ArrivalRow[]
  destination?: string
  /** Whether the picture should be open without being asked: something along the path needs a look. */
  needsAttention: boolean
}

const GROUPS: { scope: PictureScope; title: string; sub: string }[] = [
  { scope: 'node', title: 'On every node', sub: 'read by the node collectors' },
  { scope: 'cluster', title: 'Once per cluster', sub: 'read by the cluster collector' },
  { scope: 'application', title: 'From your applications', sub: 'sent to the cluster collector (OTLP)' },
]

/** The picture of one install, from what it runs and what its collectors report. `connected` and `now` are as in arrivalCheck. Pure. */
export function operatorPicture(o: { installed: string[]; diagnostics?: AgentDiagnostics; connected?: boolean; now: number; cluster: string; namespace?: string }): OperatorPicture {
  const { installed, diagnostics: d } = o
  const groups: PictureGroup[] = GROUPS.map((g) => ({
    ...g,
    sources: TELEMETRY_SIGNALS.filter((s) => s.scope === g.scope && installed.includes(s.id)).map((s) => ({ id: s.id, label: s.label, modality: s.modality, what: s.what })),
  })).filter((g) => g.sources.length > 0)

  // The lanes and their health are the ones "Is data arriving?" shows, so the two never disagree.
  const a = arrivalCheck({ wanted: installed, diagnostics: d, connected: o.connected, waited: 0, now: o.now, cluster: o.cluster, namespace: o.namespace })
  const reach = exportReach(d)
  const h = d?.exportHealth
  const collectors: PictureCollectors =
    reach === 'ok' && h
      ? h.podsFailed > 0
        ? { health: 'attention', text: `${h.podsReached} collector ${h.podsReached === 1 ? 'pod' : 'pods'} read, ${h.podsFailed} could not be read` }
        : { health: 'healthy', text: `${h.podsReached} collector ${h.podsReached === 1 ? 'pod' : 'pods'} read` }
      : reach === 'unreadable'
        ? // The error itself is in the sentence above the picture ("Is data arriving?"); here it would be said a third time.
        { health: 'attention', text: 'The agent cannot read their counters' }
        : { health: 'unknown', text: d ? 'This agent does not report its collectors' : 'The agent has not reported yet' }

  // Nothing known about a stage is not a fault; the picture's own state is the worst of the stages that are known.
  const health: Health = installed.length === 0 ? 'unknown' : worstOf([a.health, collectors.health === 'unknown' ? 'healthy' : collectors.health])
  const kinds = (['metrics', 'logs', 'traces'] as Modality[]).filter((m) => groups.some((g) => g.sources.some((s) => s.modality === m)))
  const destination = d?.installedTelemetryConfig?.exportEndpoint || undefined
  const count = groups.reduce((n, g) => n + g.sources.length, 0)
  const summary =
    installed.length === 0
      ? 'Nothing is collected here yet'
      : `${count} ${count === 1 ? 'source' : 'sources'} · ${kinds.join(', ')}${destination ? ` · to ${destination}` : ''}`
  return { health, summary, groups, collectors, lanes: a.rows, destination, needsAttention: health === 'attention' || health === 'down' }
}
