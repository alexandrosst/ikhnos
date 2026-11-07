import { ChevronRight } from 'lucide-react'
import { useId, useState } from 'react'
import { Section } from '@/features/topology/components/InspectorParts'
import { nodesOn, productName } from '@/lib/overlay'
import type { Overlay } from '@/lib/types'

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 py-1.5 text-sm">
      <span className="text-nb-500">{label}</span>
      <span className="text-right text-nb-300">{children}</span>
    </div>
  )
}

/**
 * The overlay network (a VPN or mesh) a cluster's nodes say they are on: what it looks like, its range, how many nodes, and which other
 * clusters are on it. What the reading rests on is one click away. Said only when an overlay interface was seen: addresses alone are for the
 * pair dialog, where they can be weighed against the other cluster.
 */
export function OverlaySection({ overlay, sharedWith }: { overlay: Overlay; sharedWith: string[] }) {
  const [open, setOpen] = useState(false)
  const id = useId()
  return (
    <Section title="Overlay network" hint="A VPN or mesh that gives the nodes addresses of their own (Tailscale, NetBird, WireGuard…). Not a service mesh such as Istio.">
      <div className="mb-1 text-sm font-medium text-nb-200" data-testid="overlay-name">{productName(overlay) || overlay.interface}</div>
      <Row label="Range"><span className="font-mono text-xs">{overlay.range}</span></Row>
      <Row label="On it">{nodesOn(overlay)}</Row>
      {sharedWith.length > 0 && <Row label="Also in">{sharedWith.join(', ')}</Row>}
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} aria-controls={id} className="mt-2 flex items-center gap-1 text-sm text-nb-400 hover:text-nb-300" data-testid="overlay-why">
        <ChevronRight size={14} className={open ? 'rotate-90' : ''} aria-hidden />Why we think so
      </button>
      {open && (
        <div id={id} className="mt-2 space-y-1.5 pl-5 text-sm text-nb-400" data-testid="overlay-evidence">
          {overlay.why.map((w) => <p key={w}>{w}</p>)}
          <p className="text-xs text-nb-500">It says what the nodes have, not that another cluster can reach them: the link measurement shows that.</p>
        </div>
      )}
    </Section>
  )
}
