import type { ClusterLink } from '../../../lib/types'

/** One network a cluster sits on (a shared subnet), or one tunnel it is joined through, with the other clusters on it. */
export interface ClusterNet {
  kind: ClusterLink['kind']
  /** The shared subnet prefix ("10.30.0.0/16"), or the tunnel's name and kind ("wg0 (wireguard)"). */
  via: string
  peers: { id: string; name: string }[]
}

/**
 * What each cluster shares with the others, from the confirmed cluster links. Said in the Inspector instead of drawn as a line: that two clusters share a subnet or a tunnel is a fact,
 * not a status. A link is undirected, so both ends get the other as a peer. A tunnel is only ever paired with
 * the peers its own links name (a name like "wg0" is commonly reused across unrelated tunnels). Subnets come first.
 */
export function clusterNets(links: ClusterLink[]): Map<string, ClusterNet[]> {
  const nets = new Map<string, ClusterNet[]>()
  const add = (id: string, kind: ClusterNet['kind'], via: string, peer: { id: string; name: string }) => {
    const mine = nets.get(id) ?? []
    let net = mine.find((n) => n.kind === kind && n.via === via)
    if (!net) mine.push((net = { kind, via, peers: [] }))
    if (!net.peers.some((p) => p.id === peer.id)) net.peers.push(peer)
    nets.set(id, mine)
  }
  for (const l of links) {
    add(l.fromCluster, l.kind, l.via, { id: l.toCluster, name: l.toName })
    add(l.toCluster, l.kind, l.via, { id: l.fromCluster, name: l.fromName })
  }
  for (const mine of nets.values()) mine.sort((a, b) => Number(a.kind === 'overlay') - Number(b.kind === 'overlay'))
  return nets
}

const names = (n: ClusterNet) => n.peers.map((p) => p.name).join(', ')

/** The sentence behind an Inspector line: the network or tunnel by name, and who is on it. */
export const netSentence = (n: ClusterNet) => `${n.kind === 'subnet' ? 'Network' : 'Tunnel'} ${n.via} — ${n.kind === 'subnet' ? 'shared with' : 'to'} ${names(n)}`
