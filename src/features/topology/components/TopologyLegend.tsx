import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import { ICON_SM, SectionLabel } from '@/components/ui/primitives'
import { TIER_COLOR, TIERS, type Tier } from '@/lib/types'
import { PodLegend } from '@/features/topology/components/Pods'
import { VERDICT_COLOR } from '@/features/topology/lib/mesh'

/** One line style, drawn the way the canvas draws it. */
function Line({ color, width = 1.8, dash }: { color: string; width?: number; dash?: string }) {
  return (
    <svg width="22" height="6" className="shrink-0" aria-hidden>
      <line x1="0" y1="3" x2="22" y2="3" stroke={color} strokeWidth={width} strokeDasharray={dash} />
    </svg>
  )
}

const Row = ({ swatch, label, title, quiet }: { swatch: ReactNode; label: string; title?: string; quiet?: boolean }) => (
  <span className={quiet ? 'flex items-center gap-2 whitespace-nowrap text-nb-500' : 'flex items-center gap-2 whitespace-nowrap'} title={title}>
    {swatch}
    {label}
  </span>
)

/** A kind of mark and its entries, side by side. */
const Group = ({ title, children }: { title: string; children: ReactNode }) => (
  <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
    <SectionLabel>{title}</SectionLabel>
    {children}
  </div>
)

const MESH: [keyof typeof VERDICT_COLOR, string, string][] = [
  ['encrypted', 'mTLS', 'Both ends are in the mesh and its policy is strict (or always on)'],
  ['permissive', 'Permissive or partial', 'Policy accepts plaintext, or only one end is in the mesh'],
  ['bypassed', 'Bypassed or off', 'A port skips the proxy, or the mesh has mutual TLS switched off'],
]

/**
 * What the lines and cells on the canvas mean. It lists only what is on the canvas at the moment (a mesh, measured cluster links, pods), grouped
 * by kind, so a short one stays short and a full one reads as a few columns instead of one long strip.
 */
export default function TopologyLegend({
  tiers,
  groupBy,
  mesh,
  traffic,
  connectionsOnly,
  clusterLinks,
  healthLens,
  pods,
  onClose,
}: {
  /** The tiers the clusters on the canvas belong to. */
  tiers: Tier[]
  groupBy: string
  mesh: boolean
  traffic: boolean
  connectionsOnly: boolean
  clusterLinks: boolean
  healthLens: boolean
  pods: boolean
  onClose: () => void
}) {
  return (
    <div className="topo-glass flex max-w-[min(92vw,860px)] items-start gap-4 rounded-xl border border-nb-850 py-2 pl-4 pr-2 text-xs text-nb-400" data-testid="legend">
      <div className="flex flex-1 flex-wrap items-center gap-x-5 gap-y-2">
        {tiers.length > 0 && (
          <Group title="Clusters">
            {TIERS.filter((t) => tiers.includes(t.value)).map((t) => (
              <Row key={t.value} swatch={<span className="size-2.5 shrink-0 rounded-sm" style={{ background: TIER_COLOR[t.value] }} aria-hidden />} label={t.label} />
            ))}
          </Group>
        )}
        <Group title="Links">
          {traffic && <Row swatch={<Line color="#8a96a0" width={2.4} />} label="Seen in traffic" title="Traffic was seen on this link; the thicker, the busier" />}
          {traffic && <Row swatch={<Line color="#8a96a0" width={1.2} dash="2 5" />} label="Not seen" title="Declared or entered by a person, but no traffic seen" />}
          {connectionsOnly && (
            <Row swatch={<Line color="#8a96a0" width={1.2} dash="8 4" />} label="Connections only" title="Seen by conntrack only - no eBPF collector on that node, so there is no byte count or retransmit data behind it, connections only" />
          )}
          <Row
            swatch={<Line color="#98a4ae" width={1.5} />}
            label={`Across ${groupBy}s`}
            title="A lighter line marks a dependency that crosses a cluster boundary. It is a separate signal from the solid and dotted traffic styles."
          />
        </Group>
        {mesh && (
          <Group title="Service mesh">
            {MESH.map(([k, label, hint]) => (
              <Row key={k} swatch={<Line color={VERDICT_COLOR[k]} width={2.4} />} label={label} title={`${hint}. Inferred from configuration, not measured.`} />
            ))}
          </Group>
        )}
        {clusterLinks && (
          <Group title="Cluster links">
            {healthLens ? (
              <>
                <Row swatch={<Line color="#34d399" />} label="Healthy" title="Average measured loss across the flows matched onto this link is under 1%" />
                <Row swatch={<Line color="#fbbf24" />} label="Degraded" title="Average measured loss is 1-5%" />
                <Row swatch={<Line color="#f87171" />} label="Unhealthy" title="Average measured loss is 5% or higher" />
                <Row swatch={<Line color="#98a4ae" />} label="No loss data" quiet title="A round trip is measured but no loss figure yet, so the line keeps the plain colour" />
              </>
            ) : (
              <Row swatch={<Line color="#98a4ae" />} label="Measured link" title="Round trip and loss measured across the flows between two clusters - hover the line for the numbers" />
            )}
          </Group>
        )}
        {pods && (
          <Group title="Pods">
            <PodLegend />
          </Group>
        )}
      </div>
      <button type="button" onClick={onClose} className="shrink-0 rounded p-1 text-nb-500 hover:bg-nb-900 hover:text-nb-200" aria-label="Hide legend" title="Hide legend">
        <X size={ICON_SM} />
      </button>
    </div>
  )
}
