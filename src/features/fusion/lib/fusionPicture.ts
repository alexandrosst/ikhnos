import type { FusionComponent, FusionRetention, FusionRetentionStore, FusionStatus } from '@/lib/api'
import { NO_DATA_AFTER_MS, fusionSentence, partHealth, type FusionProblem, type Shown } from '@/features/fusion/lib/fusionStatus'
import type { LinkState } from '@/lib/linkTone'
import type { HealthOrSetup } from '@/lib/status'

/** One box of the picture: what it is called, its state in FUSION's own words, and what to say under it. */
export interface PictureNode {
  id: string
  title: string
  health: Shown
  /** Under the title, in a sentence. */
  note: string
  /** A second line, when there is one (why a part is not up, where the central operator listens). */
  more?: string
  /** The store behind a box, for its disk bar. */
  store?: FusionRetentionStore
  /** Where a click goes, when the box is somewhere else (the senders are on the Pipeline page). */
  to?: string
}

export interface PictureEdge {
  from: string
  to: string
  link: LinkState
}

export interface FusionPictureModel {
  senders: PictureNode[]
  central: PictureNode
  stores: PictureNode[]
  grafana?: PictureNode
  edges: PictureEdge[]
}

/** How many send in, and how they are: a group of the Pipeline page's counts, which the shell hands to this page. */
export interface SendingGroup { total: number; state?: HealthOrSetup; worst: number; lastData?: string }
export interface SendingHop extends SendingGroup {
  key: string
  /** Local operators only: those that send straight to the central operator. */
  direct?: SendingGroup
}
type State = HealthOrSetup
type Hop = SendingHop
type HopGroup = SendingGroup

const ORDER: LinkState[] = ['broken', 'stalled', 'unknown', 'flowing']
/** Of two links the one that says less: a flow is only as good as its worst stretch. */
const worse = (a: LinkState, b: LinkState): LinkState => (ORDER.indexOf(a) <= ORDER.indexOf(b) ? a : b)

/** A part's link: down is broken, held up (a disk filling) is stalled, coming up or switched off says nothing, and a healthy part is flowing
 *  only while data has arrived lately (a quiet one is not claimed to be moving). */
function partLink(h: Shown, fresh: boolean): LinkState {
  if (h === 'down') return 'broken'
  if (h === 'attention') return 'stalled'
  if (h === 'healthy') return fresh ? 'flowing' : 'unknown'
  return 'unknown'
}

/** A sender's link: its own state, as the Pipeline page's lines say it. */
function senderLink(state: State | undefined, total: number, fresh: boolean): LinkState {
  if (total === 0 || !state || state === 'unknown' || state === 'setup') return 'unknown'
  if (state === 'down') return 'broken'
  if (state === 'attention') return 'stalled'
  return fresh ? 'flowing' : 'unknown'
}

const SENDER_SHOWN: Record<State, Shown> = { healthy: 'healthy', attention: 'attention', down: 'down', unknown: 'unknown', setup: 'starting' }

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

function senderNode(id: string, title: string, g: HopGroup, what: string): PictureNode {
  return { id, title, health: g.state ? SENDER_SHOWN[g.state] : 'unknown', note: plural(g.total, what), to: '/pipeline' }
}

/**
 * What the FUSION page draws: how data goes in (the operators that send to it), through the central operator, into the stores by kind, and
 * out to Grafana. Everything is what the server already says: each part's own readiness and disk, whether the central operator can be reached
 * from other clusters, and when data last arrived at all. It cannot tell how much went to which store, so the arrows into the stores say what
 * each store's own state says and no more: a store that is down stops its own arrow, and the others go on.
 */
export function fusionPicture({ status, retention, problems, hops, now }: { status: FusionStatus; retention: FusionRetention | null; problems: FusionProblem[]; hops: Hop[]; now: number }): FusionPictureModel | null {
  if (!status.available || status.state === 'off') return null
  const kind = fusionSentence(status, now).kind
  const parts = status.components ?? []
  const byName = (n: FusionComponent['component']) => parts.find((c) => c.component === n && c.desired > 0)
  const centralPart = byName('central')
  const last = status.lastDataAt ? Date.parse(status.lastDataAt) : NaN
  const fresh = Number.isFinite(last) && now - last <= NO_DATA_AFTER_MS
  const health = (c: FusionComponent) => partHealth(c, kind, problems)

  const exposed = status.central?.exposed
  const centralHealth: Shown = centralPart ? health(centralPart) : kind === 'starting' ? 'starting' : 'unknown'
  const central: PictureNode = {
    id: 'central',
    title: 'Central operator',
    health: centralHealth,
    note: 'the way in',
    more: status.central
      ? exposed ? `Reachable from other clusters at ${status.central.endpoint}` : `Inside this cluster only (${status.central.endpoint})`
      : undefined,
  }
  if (centralPart?.reason && centralPart.ready < centralPart.desired) central.more = centralPart.reason

  const regional = hops.find((h) => h.key === 'regional')
  const local = hops.find((h) => h.key === 'local')
  const senders: PictureNode[] = []
  const inbound: PictureEdge[] = []
  const centralIn = partLink(centralHealth, fresh)
  if (regional && regional.total > 0) {
    const n = senderNode('regional', 'Regional operators', { total: regional.total, state: regional.state, worst: regional.worst, lastData: regional.lastData }, 'operator')
    // Sending in from another cluster needs the central operator to be reachable from there.
    const reach: LinkState = exposed === false ? 'stalled' : 'flowing'
    if (exposed === false) n.more = 'They cannot reach the central operator from other clusters yet'
    senders.push(n)
    inbound.push({ from: 'regional', to: 'central', link: worse(worse(senderLink(regional.state, regional.total, fresh), centralIn), reach) })
  }
  if (local?.direct && local.direct.total > 0) {
    senders.push({ ...senderNode('local', 'Local operators', local.direct, 'operator'), note: `${plural(local.direct.total, 'operator')} ${local.direct.total === 1 ? 'sends' : 'send'} straight here` })
    inbound.push({ from: 'local', to: 'central', link: worse(senderLink(local.direct.state, local.direct.total, fresh), centralIn) })
  }
  if (senders.length === 0) {
    senders.push({ id: 'none', title: 'Nothing sends to it yet', health: 'unknown', note: 'Point an operator at FUSION on the Pipeline page', to: '/pipeline' })
    inbound.push({ from: 'none', to: 'central', link: 'unknown' })
  }

  const stores: PictureNode[] = []
  const edges: PictureEdge[] = [...inbound]
  for (const name of ['metrics', 'logs', 'traces'] as const) {
    const c = byName(name)
    if (!c) continue
    const h = health(c)
    const st = retention?.available ? retention.stores.find((s) => s.component === name) : undefined
    stores.push({ id: name, title: c.label, health: h, note: { metrics: 'metrics', logs: 'logs', traces: 'traces' }[name], more: c.reason && c.ready < c.desired ? c.reason : undefined, store: st })
    const own = partLink(h, fresh)
    edges.push({ from: 'central', to: name, link: worse(centralIn, own) })
  }

  const g = byName('grafana')
  let grafana: PictureNode | undefined
  if (g) {
    const h = health(g)
    grafana = { id: 'grafana', title: g.label, health: h, note: 'dashboards', more: g.reason && g.ready < g.desired ? g.reason : undefined }
    const reading = partLink(h, true)
    for (const s of stores) edges.push({ from: s.id, to: 'grafana', link: worse(reading, partLink(s.health, true)) })
  }
  return { senders, central, stores, grafana, edges }
}
