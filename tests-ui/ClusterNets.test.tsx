import { describe, expect, test } from 'vitest'
import { clusterNets } from '@/features/topology/lib/clusterNets'
import type { ClusterLink } from '@/lib/types'

const link = (over: Partial<ClusterLink>): ClusterLink => ({ fromCluster: 'a', fromName: 'alpha', toCluster: 'b', toName: 'bravo', kind: 'subnet', via: '10.30.0.0/16', redundancy: 1, ...over })

describe('cluster networks', () => {
  const nets = clusterNets([
    link({}),
    link({ toCluster: 'c', toName: 'charlie' }),
    link({ kind: 'overlay', via: 'wg0 (wireguard)', toCluster: 'd', toName: 'delta' }),
  ])

  test('a network counts the OTHER clusters on it, a tunnel its own peers; subnets come first', () => {
    expect(nets.get('a')).toEqual([
      { kind: 'subnet', via: '10.30.0.0/16', peers: [{ id: 'b', name: 'bravo' }, { id: 'c', name: 'charlie' }] },
      { kind: 'overlay', via: 'wg0 (wireguard)', peers: [{ id: 'd', name: 'delta' }] },
    ])
    expect(nets.get('b')?.[0].peers).toEqual([{ id: 'a', name: 'alpha' }])
    expect(nets.get('e')).toBeUndefined()
  })

  test('a tunnel name reused by unrelated pairs is not merged into one set of peers', () => {
    const reused = clusterNets([link({ kind: 'overlay', via: 'wg0 (wireguard)' }), link({ kind: 'overlay', via: 'wg0 (wireguard)', fromCluster: 'x', fromName: 'xray', toCluster: 'y', toName: 'yankee' })])
    expect(reused.get('a')?.[0].peers.map((p) => p.id)).toEqual(['b'])
    expect(reused.get('x')?.[0].peers.map((p) => p.id)).toEqual(['y'])
  })
})
