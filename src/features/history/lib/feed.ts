import { kindLabel, type ChangeEvent } from '@/lib/history'
import type { AuditEvent } from '@/lib/types'

/** What the history list shows: everything, only what Ikhnos noticed, or only what people did. */
export type FeedFilter = 'all' | 'changes' | 'people'

export type FeedItem =
  | { type: 'change'; id: string; at: string; name: string; what: string; where?: string; detail: string; cause?: string; severity: ChangeEvent['severity'] }
  | { type: 'action'; id: string; at: string; actor: string; what: string; detail: string }

// Only the action kinds the store writes (topology.ts's `audit`/`editAudit`); an unknown one still reads, under its own name.
const VERB: Record<string, string> = {
  edit: 'edited',
  delete: 'deleted',
  'accept-suggestion': 'accepted a suggestion',
  'dismiss-suggestion': 'dismissed a suggestion',
  'save-view': 'saved a view',
}

/** "edited cluster", "accepted a suggestion": the action in words, naming what it touched only when the verb alone would not. */
export function actionWhat(a: Pick<AuditEvent, 'action' | 'targetKind'>): string {
  const verb = VERB[a.action] ?? a.action.replace(/-/g, ' ')
  return a.action === 'edit' || a.action === 'delete' ? `${verb} ${a.targetKind}` : verb
}

/**
 * One list, newest first, of what Ikhnos noticed and what people did. The changes arrive already narrowed by the server (window and cluster);
 * the actions are kept locally, so the window and the cluster are applied here: an action belongs to a cluster when it was done to that cluster.
 */
export function buildFeed(events: ChangeEvent[], audit: AuditEvent[], o: { filter: FeedFilter; clusterId?: string; since?: number }): FeedItem[] {
  const items: FeedItem[] = []
  if (o.filter !== 'people') {
    for (const e of events) {
      items.push({
        type: 'change',
        id: `e:${e.id}`,
        at: e.at,
        name: e.name || e.clusterName || '—',
        what: kindLabel(e.kind),
        where: e.clusterName && e.clusterName !== e.name ? e.clusterName : undefined,
        detail: e.detail,
        cause: e.cause,
        severity: e.severity,
      })
    }
  }
  if (o.filter !== 'changes') {
    for (const a of audit) {
      if (o.since !== undefined && new Date(a.at).getTime() < o.since) continue
      if (o.clusterId && a.targetId !== o.clusterId) continue
      items.push({ type: 'action', id: `a:${a.id}`, at: a.at, actor: a.actor, what: actionWhat(a), detail: a.detail ?? '' })
    }
  }
  return items.sort((x, y) => y.at.localeCompare(x.at))
}

/** The items split by local calendar day, each day labelled Today, Yesterday or by date. Order kept. */
export function groupByDay<T extends { at: string }>(items: T[], now = Date.now()): { label: string; items: T[] }[] {
  const key = (t: number) => {
    const d = new Date(t)
    return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
  }
  const today = key(now)
  const yesterday = key(now - 86400_000)
  const out: { key: string; label: string; items: T[] }[] = []
  for (const it of items) {
    const t = new Date(it.at).getTime()
    const k = key(t)
    let g = out[out.length - 1]
    if (!g || g.key !== k) {
      const label = k === today ? 'Today' : k === yesterday ? 'Yesterday' : new Date(t).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' })
      g = { key: k, label, items: [] }
      out.push(g)
    }
    g.items.push(it)
  }
  return out.map(({ label, items }) => ({ label, items }))
}
