import { describe, expect, test } from 'vitest'
import { buildGraph } from '@/features/topology/lib/graph'
import type { Dependency, Topology } from '@/lib/types'

// What makes a drawn line move: traffic seen, not gone quiet (the long, settable window) and not idle (the short one). The server says which; the
// canvas only follows, for a single link and for the bundle that stands for several between two clusters.
const cl = (id: string) => ({ id, name: id, source: 'discovered', tier: 'core' }) as never
const sv = (id: string, clusterId: string) => ({ id, clusterId, name: id, namespace: 'ns', kind: 'Deployment', status: 'running', replicas: 1, readyReplicas: 1, nodeIds: [], labels: {} }) as never
const dep = (id: string, over: Partial<Dependency>) =>
  ({ id, from: 'a', to: 'b', fromKind: 'service', toKind: 'service', protocol: 'tcp', port: 80, sources: ['observed'], confidence: 'high', crossCluster: false, ...over }) as Dependency

const topo = (dependencies: Dependency[], second = 'c1'): Topology => ({
  clusters: second === 'c1' ? [cl('c1')] : [cl('c1'), cl(second)],
  nodes: [], namespaces: [], services: [sv('a', 'c1'), sv('b', second)], devices: [], dependencies, applications: [], sites: [], siteLinks: [], externalEndpoints: [],
})
const opts = { view: 'application', groupBy: 'cluster', servicesOnNodes: false, links: false } as const
const classes = (t: Topology) => buildGraph(t, opts).edges.map((e) => e.className)

describe('which lines move', () => {
  test('a link seen in traffic moves; an idle one, and a quiet one, do not', () => {
    expect(classes(topo([dep('d1', {})]))).toEqual(['edge-animated'])
    expect(classes(topo([dep('d1', { idle: true })]))).toEqual([undefined])
    expect(classes(topo([dep('d1', { stale: true })]))).toEqual([undefined])
  })

  test('the bundle between two clusters moves while one of its links does, and stops when all are idle', () => {
    const two = (a: Partial<Dependency>, b: Partial<Dependency>) => topo([dep('d1', { ...a, port: 80 }), dep('d2', { ...b, port: 81 })], 'c2')
    const bundle = (t: Topology) => buildGraph(t, { ...opts, view: 'infrastructure', links: true }).edges.find((e) => e.data?.aggregated)
    expect(bundle(two({ idle: true }, {}))?.className).toBe('edge-animated') // found, or this reads undefined for the wrong reason
    const allIdle = bundle(two({ idle: true }, { idle: true }))
    expect(allIdle).toBeDefined()
    expect(allIdle?.className).toBeUndefined()
    expect(allIdle?.data?.activeCount).toBe(2) // still seen lately: the count is not the animation
  })
})
