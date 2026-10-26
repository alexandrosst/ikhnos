import { uptimeWords, type AgentDiagnostics } from '../../../lib/consent'
import type * as W from '../../../lib/generated/wire'
import { exportReach } from '../../../lib/exportHealth'
import { TELEMETRY_SIGNALS, type Modality } from '../../../lib/install'
import { type Health, reportIsStale, worstOf } from '../../../lib/status'
import { arrivalCheck, type ArrivalRow } from './telemetrySetup'
import type { LinkState } from './pipelineFlow'

/**
 * The local operator in one picture: what its collectors take in, the collectors themselves, and where each kind of data goes out, with the
 * state of each stage. It is drawn only from what the agent already reports: the signals the chart runs, the collectors' own counters per signal
 * type and per source, whether each scraped tool answers on each node, and, where a controller keeps the release, how many pods of each
 * workload are ready. The state sits on the stage that is known; a stage nothing is known about is not a fault.
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
  energy: "Look at the Kepler pods on those nodes: one that started but serves nothing gives no energy data.",
  accelerators: "Look at the dcgm-exporter pods on those nodes: one that started but serves nothing gives no GPU data.",
  clusterLinks: "The collector reads the agent's own link service: look at the agent pod's logs.",
}

/** The bundled tools whose pods the controller counts, by the component the chart labels them with, and how the picture names them. */
const TOOL_PODS: Record<string, { component: string; name: string }> = {
  energy: { component: 'telemetry-kepler', name: 'Kepler' },
  accelerators: { component: 'telemetry-dcgm', name: 'dcgm-exporter' },
}

/** How long a workload is seen with pods missing before it is held back rather than starting: the same as the agent's (WorkloadsHeldBackAfter). */
export const HELD_BACK_SECONDS = 180

const rank = (s: W.SourceState) => READ_WORST_FIRST.indexOf(s)
const agoWords = (iso: string | undefined, now: number) => (iso ? `${uptimeWords(Math.max(0, (now - Date.parse(iso)) / 1000))} ago` : undefined)

const heldBack = (w: W.WorkloadDoc) => w.ready < w.wanted && !w.rollingOut && (w.notReadySeconds ?? 0) >= HELD_BACK_SECONDS

/**
 * Whether the pods of the tool behind a source are all there, from what the controller counted: undefined when they are (or nothing is known),
 * waiting while the missing ones are starting, degraded or failing when they have been missing long enough to be held back.
 */
function podsRead(id: string, workloads: readonly W.WorkloadDoc[] | undefined): SourceRead | undefined {
  const t = TOOL_PODS[id]
  const w = t && workloads?.find((x) => x.component === t.component)
  if (!t || !w || w.ready >= w.wanted) return undefined
  if (!heldBack(w)) return { state: 'waiting', word: READ_WORD.waiting, health: 'unknown', sentence: `${t.name}: ${w.ready} of ${w.wanted} pods are ready, the others are starting.` }
  const since = uptimeWords(w.notReadySeconds ?? 0)
  const state: W.SourceState = w.ready === 0 ? 'failing' : 'degraded'
  const lead = w.ready === 0 ? `None of ${t.name}'s ${w.wanted} pods is ready` : `${t.name} runs on ${w.ready} of the ${w.wanted} nodes it is asked to`
  return { state, word: READ_WORD[state], health: 'attention', sentence: `${lead}, for ${since} now. A node it cannot run on, a taint or an image that does not start holds the others back: the events of those pods say which.` }
}

/**
 * How the collectors read one signal's source, from the receivers that serve it, and whether the pods of the tool behind it are there; undefined
 * when neither is known. The source is as bad as the worse of the two, and says both.
 */
export function sourceRead(id: string, sources: readonly W.SourceHealthDoc[] | undefined, now: number, workloads?: readonly W.WorkloadDoc[]): SourceRead | undefined {
  const scraped = scrapeRead(id, sources, now)
  const pods = podsRead(id, workloads)
  if (!pods) return scraped
  if (!scraped) return pods
  // Pods that are only starting do not make a source that is being read any less read.
  if (pods.state === 'waiting') return { ...scraped, sentence: [scraped.sentence, pods.sentence].filter(Boolean).join(' ') }
  if (rank(pods.state) < rank(scraped.state)) return { ...pods, sentence: [pods.sentence, scraped.sentence].filter(Boolean).join(' ') }
  if (rank(pods.state) === rank(scraped.state) && pods.health === 'attention') return { ...scraped, sentence: [scraped.sentence, pods.sentence].filter(Boolean).join(' ') }
  return scraped
}

function scrapeRead(id: string, sources: readonly W.SourceHealthDoc[] | undefined, now: number): SourceRead | undefined {
  const mine = (sources ?? []).filter((r) => r.signals.includes(id))
  if (mine.length === 0) return undefined
  const worst = mine.reduce((a, b) => (rank(b.state) < rank(a.state) ? b : a))
  const state = worst.state
  const tool = (worst.targets ?? 0) > 0
  // A tool the collector scrapes that answers on only some of its nodes has data missing, which is more than a read that fails in part.
  const health: Health = state === 'failing' || (tool && state === 'degraded') ? 'attention' : state === 'waiting' ? 'unknown' : 'healthy'
  let sentence: string | undefined
  if (tool && state === 'waiting') {
    sentence = id === 'clusterLinks' ? 'The link service answers and has no link to report yet. It has one once a path is measured.' : 'It answers and has nothing to report yet.'
  } else if (tool && (state === 'failing' || state === 'degraded')) {
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

/** The signals the collectors read from a source of the cluster itself, with a receiver of their own (the Prometheus scrapes of tools are not here). */
const READ_BY_RECEIVER = new Set(['resourceUsage', 'nodeRuntime', 'kubernetesState', 'kubernetesEvents', 'systemLogs'])
const RECEIVER_ORDER: W.ReceiverState[] = ['refusing', 'receiving', 'silent', 'waiting']

/**
 * What can be said of a source whose receiver has not reported yet, or that is pushed to the collectors and not read by them, once the agent
 * does report its collectors. A source read by a receiver of the cluster that has counted nothing yet is waiting (the events of a quiet
 * cluster are the usual case: the receiver has nothing to count until the first one). An application's data is pushed over OTLP, so its state
 * is the OTLP receiver's for that kind of data: receiving once anything has come in, waiting until then. Undefined when the agent says nothing
 * of its collectors, which is not a fault.
 */
function impliedRead(s: { id: string; modality: Modality; layer: 'infrastructure' | 'application' }, d: AgentDiagnostics | undefined, now: number, installed: readonly string[] = []): SourceRead | undefined {
  const h = d?.exportHealth
  if (!h || exportReach(d) !== 'ok') return undefined
  if (s.layer !== 'application') {
    if (!READ_BY_RECEIVER.has(s.id)) return undefined
    const quiet = s.id === 'kubernetesEvents' ? 'No event has come in yet. That is normal while the cluster is quiet.' : 'Nothing has come in yet.'
    return { state: 'waiting', word: READ_WORD.waiting, health: 'unknown', sentence: quiet }
  }
  const mine = (h.receivers ?? []).filter((r) => r.signal === s.modality)
  const state = RECEIVER_ORDER.find((x) => mine.some((r) => r.state === x)) ?? 'waiting'
  // The receiver counts by kind of data, not by who sent it: say so where another source shares it, so "Receiving" is not read as more than it is.
  const shared = s.id === 'applicationMetrics' && installed.includes('networkLatency') ? ' Network latency sends its metrics to the same receiver, so they are counted here too.' : ''
  const accepted = mine.reduce((n, r) => n + r.accepted, 0)
  const last = mine.map((r) => r.lastAcceptedAt).filter((t): t is string => !!t).sort().at(-1)
  switch (state) {
    case 'receiving':
      return { state: 'receiving', word: 'Receiving', health: 'healthy', sentence: `${last ? `${accepted.toLocaleString()} accepted, the last ${agoWords(last, now)}.` : ''}${shared}`.trim() || undefined }
    case 'silent':
      return { state: 'silent', word: 'Quiet', health: 'healthy', sentence: `Nothing has been sent for a few minutes. That is normal while the applications are idle.${shared}` }
    case 'refusing':
      return { state: 'failing', word: 'Turned away', health: 'attention', sentence: `The collector is turning this data away (${mine.reduce((n, r) => n + r.refused, 0).toLocaleString()} refused). It is short of memory, or the pipeline behind its receiver refused it: look at the collector pods' logs.` }
    default:
      return { state: 'waiting', word: READ_WORD.waiting, health: 'unknown', sentence: `No application has sent ${s.modality} to the collector yet.${shared}` }
  }
}

/**
 * The signals among `installed` whose source needs a look, with the sentence for each: `partial` is a tool that answers on some of its nodes
 * and not on others, otherwise the source is not being read at all. What the Pipeline page says about a collector.
 */
export function failingSources(installed: readonly string[], d: AgentDiagnostics | undefined, now: number): { id: string; label: string; sentence: string; partial: boolean }[] {
  const out: { id: string; label: string; sentence: string; partial: boolean }[] = []
  for (const sig of TELEMETRY_SIGNALS) {
    if (!installed.includes(sig.id)) continue
    const r = sourceRead(sig.id, d?.exportHealth?.sources, now, d?.workloads)
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
  // What an agent last said is not what is happening while it is gone: a disconnected agent, or a report that has stopped coming, says nothing
  // of any source now, however well it was reading when it last reported.
  const outdated = !!d && (o.connected === false || reportIsStale((o.now - Date.parse(d.reportedAt)) / 1000))
  const groups: PictureGroup[] = GROUPS.map((g) => ({
    ...g,
    sources: TELEMETRY_SIGNALS.filter((s) => s.scope === g.scope && installed.includes(s.id)).map((s) => ({ id: s.id, label: s.label, modality: s.modality, what: s.what, read: outdated ? undefined : sourceRead(s.id, d?.exportHealth?.sources, o.now, d?.workloads) ?? impliedRead(s, d, o.now, installed) })),
  })).filter((g) => g.sources.length > 0)

  // The lanes and their health are the ones "Is data arriving?" shows, so the two never disagree.
  const a = arrivalCheck({ wanted: installed, diagnostics: d, connected: o.connected, waited: 0, now: o.now, cluster: o.cluster, namespace: o.namespace })
  const reach = exportReach(d)
  const h = d?.exportHealth
  let collectors: PictureCollectors =
    reach === 'ok' && h
      ? h.podsFailed > 0
        ? { health: 'attention', text: `${h.podsReached} collector ${h.podsReached === 1 ? 'pod' : 'pods'} read, ${h.podsFailed} could not be read` }
        : { health: 'healthy', text: `${h.podsReached} collector ${h.podsReached === 1 ? 'pod' : 'pods'} read` }
      : reach === 'unreadable'
        ? // The error itself is in the sentence above the picture ("Is data arriving?"); here it would be said a third time.
        { health: 'attention', text: 'The agent cannot read their counters' }
        : { health: 'unknown', text: d ? 'This agent does not report its collectors' : 'The agent has not reported yet' }

  // The counters of the collector pods that are there say nothing of a pod that is not: where a controller keeps the release, it counts them.
  const missing = (d?.workloads ?? []).filter((w) => (w.component === 'telemetry-host' || w.component === 'telemetry-cluster') && w.ready < w.wanted)
  if (missing.length > 0) {
    const parts = missing.map((w) => `${w.component === 'telemetry-host' ? 'node collectors' : 'cluster collector'}: ${w.ready} of ${w.wanted} pods ready${w.rollingOut ? ' (a change is being rolled out)' : heldBack(w) ? ` for ${uptimeWords(w.notReadySeconds ?? 0)}` : ''}`)
    collectors = { health: missing.some(heldBack) ? 'attention' : collectors.health, text: `${collectors.text} · ${parts.join(', ')}` }
  }

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

/**
 * The line from one source into the collectors, drawn as the pipeline draws the line between two nodes: dashes that move when the collectors
 * are reading it, still and amber when it is partly read, red when it is not read, grey and dashed when it is not known or quiet (nothing is
 * coming in, which is nothing to worry about while nothing produces anything). A source the agent does not report on a receiver of its own is
 * not known, which is not a fault.
 */
export function sourceLink(read: SourceRead | undefined): LinkState {
  switch (read?.state) {
    case 'receiving': return 'flowing'
    case 'degraded': return 'stalled'
    case 'failing': return 'broken'
    default: return 'unknown'
  }
}
