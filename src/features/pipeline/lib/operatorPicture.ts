import { uptimeWords, type AgentDiagnostics } from '../../../lib/consent'
import type * as W from '../../../lib/generated/wire'
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
  /** What the collectors' own counters say about reading it, when the agent reports it for this source (not every source has a receiver of its own). */
  read?: SourceRead
}

/** How one source is being read, in the product's words, and the sentence that says what is wrong and what to check when something is. */
export interface SourceRead {
  state: W.SourceState
  word: string
  health: Health
  sentence?: string
}

const READ_WORD: Record<W.SourceState, string> = { waiting: 'Waiting', receiving: 'Reading', silent: 'Quiet', degraded: 'Partly reading', failing: 'Not reading' }
/** Worst first. A source several receivers serve (the kubelet's) is as bad as its worst one. */
const READ_WORST_FIRST: W.SourceState[] = ['failing', 'degraded', 'silent', 'waiting', 'receiving']

/**
 * What to check when a source cannot be read, from what the chart does for it. Said as a thing to look at, never as a cause: the counters
 * say that values could not be read, not why.
 */
const CHECK: Record<string, string> = {
  resourceUsage: "The kubelet's certificate is verified: where a node's kubelet serves one of its own (kubeadm's default, AKS), the install needs telemetry.kubelet.insecureSkipVerify=true. Look at the node collector pods' logs for the error.",
  nodeRuntime: "It reads the kubelet, which is verified by its certificate: see the same note as Resource usage, and the node collector pods' logs.",
  kubernetesState: "The collector reads the Kubernetes API with the agent's read-only access: look at the cluster collector pod's logs for the error.",
  kubernetesEvents: "The collector reads the Kubernetes API with the agent's read-only access: look at the cluster collector pod's logs for the error.",
  systemLogs: "The collector reads the node's log files: look at the node collector pods' logs for the error, and that the log directory is mounted.",
}

/** What to check when a tool the collector scrapes does not answer on some or all of its nodes. */
const CHECK_TOOL: Record<string, string> = {
  energy: "Look at the Kepler pods on those nodes: a node Kepler cannot run on holds its pod back, and one that started but serves nothing gives no energy data.",
  accelerators: "Look at the dcgm-exporter pods on those nodes: a node without a GPU runs none, and one that started but serves nothing gives no GPU data.",
  clusterLinks: "The collector reads the agent's own link service: look at the agent pod's logs.",
}

const rank = (s: W.SourceState) => READ_WORST_FIRST.indexOf(s)
const agoWords = (iso: string | undefined, now: number) => (iso ? `${uptimeWords(Math.max(0, (now - Date.parse(iso)) / 1000))} ago` : undefined)

/** How the collectors read one signal's source, from the receivers that serve it; undefined when no receiver of the agent's report serves it. */
export function sourceRead(id: string, sources: readonly W.SourceHealthDoc[] | undefined, now: number): SourceRead | undefined {
  const mine = (sources ?? []).filter((r) => r.signals.includes(id))
  if (mine.length === 0) return undefined
  const worst = mine.reduce((a, b) => (rank(b.state) < rank(a.state) ? b : a))
  const state = worst.state
  const tool = (worst.targets ?? 0) > 0
  // A tool the collector scrapes that answers on only some of its nodes has data missing, which is more than a read that fails in part.
  const health: Health = state === 'failing' || (tool && state === 'degraded') ? 'attention' : state === 'waiting' ? 'unknown' : 'healthy'
  let sentence: string | undefined
  if (tool && (state === 'failing' || state === 'degraded')) {
    const targets = worst.targets ?? 0
    const down = worst.down ?? []
    const where = down.length > 0 ? ` Not answering: ${down.join(', ')}${targets - (worst.targetsUp ?? 0) > down.length ? ' and others' : ''}.` : ''
    sentence = `${state === 'failing' ? `None of its ${targets} ${targets === 1 ? 'target' : 'targets'} answered the collector with data.` : `It answered the collector with data on ${worst.targetsUp ?? 0} of ${targets}.`}${where} ${CHECK_TOOL[id] ?? ''}`.trim()
  } else if (state === 'failing') {
    sentence = `${worst.failed.toLocaleString()} values could not be read (the last ${agoWords(worst.lastFailedAt, now) ?? 'recently'}) and nothing has come in since. ${CHECK[id] ?? "Look at the collector pods' logs for the error."}`
  } else if (state === 'degraded') {
    sentence = `Some values cannot be read (the last ${agoWords(worst.lastFailedAt, now) ?? 'recently'}) while the rest come in. The cluster owner can see which in the collector pods' logs.`
  } else if (state === 'silent') {
    sentence = 'Nothing has come in for a few minutes. That is normal while nothing here is producing anything.'
  }
  return { state, word: READ_WORD[state], health, sentence }
}

/**
 * The signals among `installed` whose source needs a look, with the sentence for each: `partial` is a tool that answers on some of its nodes
 * and not on others, otherwise the source is not being read at all. What the Pipeline page says about a collector.
 */
export function failingSources(installed: readonly string[], d: AgentDiagnostics | undefined, now: number): { id: string; label: string; sentence: string; partial: boolean }[] {
  const out: { id: string; label: string; sentence: string; partial: boolean }[] = []
  for (const sig of TELEMETRY_SIGNALS) {
    if (!installed.includes(sig.id)) continue
    const r = sourceRead(sig.id, d?.exportHealth?.sources, now)
    if (r && r.health === 'attention') out.push({ id: sig.id, label: sig.label, sentence: r.sentence ?? '', partial: r.state === 'degraded' })
  }
  return out
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
    sources: TELEMETRY_SIGNALS.filter((s) => s.scope === g.scope && installed.includes(s.id)).map((s) => ({ id: s.id, label: s.label, modality: s.modality, what: s.what, read: sourceRead(s.id, d?.exportHealth?.sources, o.now) })),
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
  // A source that is not being read is a thing to look at even while the data of the others flows; one that is read in part is shown, not raised.
  const reads = groups.flatMap((g) => g.sources.map((x) => x.read?.health ?? 'healthy'))
  const health: Health = installed.length === 0 ? 'unknown' : worstOf([a.health, collectors.health === 'unknown' ? 'healthy' : collectors.health, ...reads.map((h) => (h === 'unknown' ? 'healthy' : h))])
  const kinds = (['metrics', 'logs', 'traces'] as Modality[]).filter((m) => groups.some((g) => g.sources.some((s) => s.modality === m)))
  const destination = d?.installedTelemetryConfig?.exportEndpoint || undefined
  const count = groups.reduce((n, g) => n + g.sources.length, 0)
  const unread = groups.reduce((n, g) => n + g.sources.filter((x) => x.read?.health === 'attention').length, 0)
  const summary =
    installed.length === 0
      ? 'Nothing is collected here yet'
      : `${count} ${count === 1 ? 'source' : 'sources'} · ${kinds.join(', ')}${destination ? ` · to ${destination}` : ''}${unread > 0 ? ` · ${unread} to look at` : ''}`
  return { health, summary, groups, collectors, lanes: a.rows, destination, needsAttention: health === 'attention' || health === 'down' }
}
