import type { AgentDiagnostics, AgentExportHealth, AgentExportRoute, ExportState } from './consent'
import type { ReceiverHealthDoc } from './generated/wire'
import { TELEMETRY_SIGNALS, type Modality } from './install'

/**
 * What the agent's export counters say about each signal type this install sends: one row per type that has
 * a signal turned on, in the order the wizard shows them. A type that has a signal on but no route in the
 * report is `waiting` - the collector counts a route only once something has gone through it, so "absent"
 * is exactly "nothing has gone out yet".
 */
export interface ExportRow {
  modality: Modality
  /** The signals behind it, by label: Resource usage, Kubernetes state... */
  signals: string[]
  state: ExportState
  /** The exporters its data goes through (normally one). */
  exporters: string[]
  sent: number
  failed: number
  lastSentAt?: string
  lastFailedAt?: string
  /** Some signal behind it is one that applications push (OTLP), so what the collector took in is part of the path. */
  pushed: boolean
  /** What the collectors' OTLP receiver took in for this type: the stage before the exporters. Absent: nothing has been seen arriving. */
  received?: { state: ReceiverHealthDoc['state']; accepted: number; refused: number; lastAcceptedAt?: string; lastRefusedAt?: string }
}

/**
 * Where applications send their telemetry in the cluster: the Service the agent's cluster collector sits behind, on the namespace the agent
 * is installed in. gRPC on 4317 and HTTP on 4318; the chart's Service is called ikhnos-telemetry-cluster whatever the release is called.
 */
export function otlpAddress(namespace: string): { grpc: string; http: string } {
  const host = `ikhnos-telemetry-cluster.${namespace}.svc`
  return { grpc: `${host}:4317`, http: `${host}:4318` }
}

/** How alarming each state is, so that several exporters for one signal type show the worst. */
const RANK: Record<ExportState, number> = { waiting: 0, exporting: 1, silent: 2, failing: 3 }

const ORDER: Modality[] = ['metrics', 'logs', 'traces']

/** The receiver's states by how alarming they are, as RANK is for the exporters. */
const RECEIVER_RANK: Record<ReceiverHealthDoc['state'], number> = { waiting: 0, receiving: 1, silent: 2, refusing: 3 }

const later = (a?: string, b?: string) => (a && b ? (Date.parse(a) >= Date.parse(b) ? a : b) : (a ?? b))

export function exportRows(installed: string[], health: AgentExportHealth): ExportRow[] {
  const rows: ExportRow[] = []
  for (const modality of ORDER) {
    const on = TELEMETRY_SIGNALS.filter((s) => s.modality === modality && installed.includes(s.id))
    const signals = on.map((s) => s.label)
    if (signals.length === 0) continue
    const routes = health.routes.filter((r) => r.signal === modality)
    const row: ExportRow = { modality, signals, state: 'waiting', exporters: [], sent: 0, failed: 0, pushed: on.some((s) => s.layer === 'application') }
    for (const r of (health.receivers ?? []).filter((x) => x.signal === modality)) {
      const got = row.received ?? { state: 'waiting' as const, accepted: 0, refused: 0 }
      got.accepted += r.accepted
      got.refused += r.refused
      if (RECEIVER_RANK[r.state] > RECEIVER_RANK[got.state]) got.state = r.state
      got.lastAcceptedAt = later(got.lastAcceptedAt, r.lastAcceptedAt)
      got.lastRefusedAt = later(got.lastRefusedAt, r.lastRefusedAt)
      row.received = got
    }
    for (const r of routes) {
      row.exporters.push(r.exporter)
      row.sent += r.sent
      row.failed += r.failed
      if (RANK[r.state] > RANK[row.state]) row.state = r.state
      row.lastSentAt = later(row.lastSentAt, r.lastSentAt)
      row.lastFailedAt = later(row.lastFailedAt, r.lastFailedAt)
    }
    rows.push(row)
  }
  return rows
}

/** Whether the agent could read the collectors' counters at all. */
export type ExportReach = 'not-reported' | 'unreadable' | 'ok'

export function exportReach(d: AgentDiagnostics | undefined): ExportReach {
  const h = d?.exportHealth
  if (!h) return 'not-reported'
  return h.podsReached === 0 && h.podsFailed > 0 ? 'unreadable' : 'ok'
}

export type { AgentExportRoute }

/** One dot's worth of a whole agent's export health, for a table row: the worst state of any signal type it sends, in words. */
export type ExportSummary = { kind: 'online' | 'offline' | 'starting' | 'idle'; text: string }
