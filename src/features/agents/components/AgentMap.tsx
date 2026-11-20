import clsx from 'clsx'
import { Server } from 'lucide-react'
import { Card, ICON_SM, LiveDot } from '@/components/ui/primitives'
import type { AgentHealth } from '@/features/agents/lib/agentHealth'
import { HEALTH, shortAge, tierOf } from '@/features/agents/lib/agentView'
import { bytesTotal } from '@/lib/observed'
import type { Agent, Cluster, Tier } from '@/lib/types'

/* ---------- the map: this server in the middle of the picture, agents by tier ---------- */

const ROWS: { tier: Tier; label: string }[] = [
  { tier: 'cloud', label: 'Cloud' },
  { tier: 'edge', label: 'Edge' },
  { tier: 'far-edge', label: 'Far edge' },
]
const CARD_W = 236
const CARD_H = 62
const GAP = 22

export function AgentMap({ rows, clusterOf, selected, onSelect }: { rows: { a: Agent; h: AgentHealth }[]; clusterOf: (id?: string) => Cluster | undefined; selected: string | null; onSelect: (id: string | null) => void }) {
  const W = 1000
  const perLine = 4
  // Group by the tier of the agent's cluster; an agent with no cluster yet goes in a row of its own.
  const groups = [...ROWS.map((r) => ({ ...r, items: rows.filter((x) => clusterOf(x.a.clusterId)?.tier === r.tier) })), { tier: 'none' as const, label: 'Not in a cluster yet', items: rows.filter((x) => !clusterOf(x.a.clusterId)) }].filter((g) => g.items.length > 0)
  let y = 96
  const placed: { x: number; y: number; a: Agent; h: AgentHealth }[] = []
  const labels: { y: number; text: string }[] = []
  for (const g of groups) {
    labels.push({ y, text: g.label })
    y += 18
    for (let i = 0; i < g.items.length; i += perLine) {
      const line = g.items.slice(i, i + perLine)
      const total = line.length * CARD_W + (line.length - 1) * GAP
      line.forEach((it, k) => placed.push({ x: (W - total) / 2 + k * (CARD_W + GAP), y, ...it }))
      y += CARD_H + GAP
    }
    y += 14
  }
  const H = Math.max(y, 180)
  const hub = { x: W / 2, y: 34 }
  return (
    <Card className="overflow-x-auto p-2" data-testid="agents-map">
      <svg viewBox={`0 0 ${W} ${H}`} className="mx-auto w-full min-w-[640px] max-w-[1000px]" role="img" aria-label="Agents connected to this server, grouped by the tier of their cluster">
        {placed.map((p) => {
          const st = HEALTH[p.h]
          const live = p.h === 'ok' || p.h === 'late'
          const d = `M${hub.x},${hub.y + 20} C${hub.x},${(hub.y + p.y) / 2 + 20} ${p.x + CARD_W / 2},${(hub.y + p.y) / 2} ${p.x + CARD_W / 2},${p.y}`
          return (
            <path
              key={p.a.id}
              d={d}
              fill="none"
              strokeWidth={live ? 1.6 : 1.2}
              strokeLinecap="round"
              className={live ? 'map-link' : undefined}
              data-reverse="1"
              style={{ stroke: p.h === 'ok' ? '#5fd3a0' : p.h === 'late' ? '#fbbf24' : '#6f7b85', ['--flow' as string]: '2.6s', strokeDasharray: live ? undefined : '2 5', opacity: live ? 0.85 : 0.5 }}
              data-status={st.label}
            />
          )
        })}
        <g transform={`translate(${hub.x - 70} ${hub.y - 20})`}>
          <rect width={140} height={40} rx={10} style={{ fill: 'var(--color-nb-920)', stroke: 'var(--color-accent)' }} strokeWidth={1.4} />
          <foreignObject x={0} y={0} width={140} height={40}>
            <div className="flex h-full items-center justify-center gap-2 text-sm font-medium text-nb-300"><Server size={ICON_SM} className="text-accent" aria-hidden /> This server</div>
          </foreignObject>
        </g>
        {labels.map((l) => (
          <text key={l.text} x={22} y={l.y + 4} fontSize={11} style={{ fill: 'var(--color-nb-500)' }} className="uppercase tracking-wide">{l.text}</text>
        ))}
        {placed.map((p) => {
          const c = clusterOf(p.a.clusterId)
          const st = HEALTH[p.h]
          const sel = selected === p.a.id
          return (
            <foreignObject key={p.a.id} x={p.x} y={p.y} width={CARD_W} height={CARD_H}>
              <button
                onClick={() => onSelect(sel ? null : p.a.id)}
                aria-pressed={sel}
                className={clsx('flex h-full w-full flex-col justify-center rounded-lg border bg-nb-920 px-3 text-left transition-colors hover:border-nb-700', sel ? 'border-accent' : 'border-nb-850')}
                data-testid="agent-node"
              >
                <span className="flex items-center gap-2 text-sm font-medium text-nb-300">
                  <LiveDot kind={st.dot} />
                  <span className="truncate">{p.a.name}</span>
                </span>
                <span className="mt-0.5 truncate text-xs text-nb-500">{c ? c.name : 'no cluster yet'} · {p.a.lastHeartbeat ? shortAge(p.a.lastHeartbeat) : st.label.toLowerCase()}</span>
                <span className="truncate text-[11px] text-nb-600">{p.a.link ? `${bytesTotal(p.a.link.bytes)} received` : tierOf(p.a)}</span>
              </button>
            </foreignObject>
          )
        })}
      </svg>
    </Card>
  )
}
