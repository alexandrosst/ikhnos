import { Button } from '@/components/ui/primitives'
import { pairOverlay } from '@/lib/overlay'
import type { Agent } from '@/lib/types'
import type { Via } from '@/lib/clusterLinks'

/**
 * How two clusters are connected, as far as their nodes say: each one's overlay network (a VPN or mesh) and one sentence on whether they look
 * to share it. Nothing is chosen for the person: when mesh addresses are worth measuring through, there is a button that picks them, and the
 * measurement is what then shows whether the two reach each other. Shows nothing when there is nothing to say.
 */
export function PairOverlayBox({ agents, x, y, name, via, onUseMesh, reached }: { agents: Agent[]; x: string; y: string; name: (id: string) => string; via: Via | ''; onUseMesh: () => void; reached?: readonly string[] }) {
  const p = pairOverlay(agents, x, y, name, reached)
  if (!p) return null
  return (
    <div data-testid="pair-overlay">
      <div className="mb-2 text-sm font-medium text-nb-300">How these two are connected</div>
      <div className="rounded-lg border border-nb-850 bg-nb-900 px-4 py-3">
        <div className="grid grid-cols-[auto_1fr_auto] items-baseline gap-x-4 gap-y-2 text-sm">
          {p.rows.map((r) => (
            <div key={r.name} className="contents" data-testid="pair-overlay-row">
              <span className="text-nb-300">{r.name}</span>
              <span className="text-nb-400">
                <span className={r.note ? '' : 'text-nb-200'}>{r.what}</span> <span className="font-mono text-xs text-nb-300">{r.range}</span>
                {r.note && <span>, {r.note}</span>}
              </span>
              <span className="text-xs text-nb-500">{r.nodes}</span>
            </div>
          ))}
        </div>
        <p className="mt-3 border-t border-nb-850 pt-3 text-sm text-nb-300">{p.sentence}</p>
        {p.offerMesh && via !== 'mesh' && (
          <div className="mt-2">
            <Button variant="secondary" onClick={onUseMesh} data-testid="pair-overlay-use">Measure through mesh addresses</Button>
          </div>
        )}
      </div>
    </div>
  )
}
