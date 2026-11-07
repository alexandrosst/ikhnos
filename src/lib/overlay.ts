import type { Agent, Overlay } from '@/lib/types'

/** The product an interface looks like, as a person writes it. An unnamed WireGuard is just "WireGuard". */
const PRODUCT: Record<string, string> = { tailscale: 'Tailscale', netbird: 'NetBird', zerotier: 'ZeroTier', nebula: 'Nebula', wireguard: 'WireGuard' }

export const productName = (o: Pick<Overlay, 'product'>) => (o.product ? (PRODUCT[o.product] ?? o.product) : '')

/** True when the reading comes from an interface the nodes have, not only from addresses in a range that others use as well. */
export const fromInterface = (o: Overlay) => !!o.interface

/** "3 of 4 nodes", or "all 4 nodes" when every one is on it. */
export const nodesOn = (o: Overlay) => (o.nodes === o.ofNodes ? (o.ofNodes === 1 ? '1 node' : `all ${o.ofNodes} nodes`) : `${o.nodes} of ${o.ofNodes} nodes`)

export interface PairRow {
  name: string
  /** "Tailscale" or "Addresses seen"; with the range after it. */
  what: string
  range: string
  nodes: string
  /** Set when only addresses were seen. */
  note?: string
}

/** What the dialog for a pair of clusters says about how the two are connected, or null when there is nothing to say. */
export interface PairOverlay {
  rows: [PairRow, PairRow]
  sentence: string
  /** Whether the sentence goes with an offer to measure through mesh addresses. */
  offerMesh: boolean
  /** What the mesh option is called in the list: "Mesh addresses: Tailscale, both clusters (suggested)". */
  meshLabel?: string
}

const rowOf = (name: string, o: Overlay): PairRow =>
  fromInterface(o)
    ? { name, what: productName(o) || o.interface || 'Overlay', range: o.range, nodes: nodesOn(o) }
    : { name, what: 'Addresses in', range: o.range, nodes: nodesOn(o), note: 'no overlay interface seen' }

export function pairOverlay(agents: Agent[], x: string, y: string, name: (id: string) => string): PairOverlay | null {
  const ag = (id: string) => agents.find((a) => a.clusterId === id && a.status === 'approved')
  const ax = ag(x)
  const ay = ag(y)
  const ox = ax?.overlay
  const oy = ay?.overlay
  if (!ax || !ay || !ox || !oy) return null
  const rows: [PairRow, PairRow] = [rowOf(name(x), ox), rowOf(name(y), oy)]
  const peer = (ax.overlayPeers ?? []).find((p) => p.clusterId === y)
  if (peer && !peer.maybe) {
    const product = productName(ox)
    return {
      rows,
      offerMesh: true,
      meshLabel: `Mesh addresses${product ? `: ${product}, both clusters` : ''} (suggested)`,
      sentence: `Likely the same ${product || 'overlay'} network. Measure through mesh addresses to confirm.`,
    }
  }
  if (peer?.maybe) {
    const bare = fromInterface(ox) ? name(y) : name(x)
    return {
      rows,
      offerMesh: true,
      sentence: `${bare} may be on the same network, or on a carrier’s shared address space: that range is used for both. Turn on its node probe to tell, or measure through mesh addresses and see.`,
    }
  }
  if (fromInterface(ox) && fromInterface(oy) && ox.product !== oy.product) {
    return { rows, offerMesh: false, sentence: 'They are not on the same overlay network.' }
  }
  return null
}
