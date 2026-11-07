import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { InternalNode } from '@xyflow/react'
import { countConflicts } from '../src/features/topology/components/conflicts'
import { collectObstacles } from '../src/features/topology/components/OffsetEdge'
import { ORTHO_MAX_OBSTACLES } from '../src/features/topology/components/ortho'
import { routeLines, type Laid } from '../src/features/topology/components/SquareRoutes'
import { buildGraph, HEADER, type TopoNode } from '../src/features/topology/lib/graph'
import { seedTopology } from '../src/lib/seed'

/** React Flow's node lookup, as far as routing reads it: absolute boxes with their parents. */
function lookupOf(nodes: TopoNode[]): Map<string, InternalNode> {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const abs = (n: TopoNode): { x: number; y: number } => {
    const p = n.parentId ? byId.get(n.parentId) : undefined
    const o = p ? abs(p) : { x: 0, y: 0 }
    return { x: o.x + n.position.x, y: o.y + n.position.y }
  }
  const m = new Map<string, InternalNode>()
  for (const n of nodes) {
    const w = Number((n.style as { width?: number }).width)
    const h = Number((n.style as { height?: number }).height)
    m.set(n.id, { id: n.id, type: n.type, parentId: n.parentId, measured: { width: w, height: h }, internals: { positionAbsolute: abs(n) } } as unknown as InternalNode)
  }
  return m
}

const scene = () => {
  const g = buildGraph(seedTopology(), { view: 'application', groupBy: 'cluster', servicesOnNodes: false, links: true, devices: true })
  return { g, lookup: lookupOf(g.nodes) }
}
const measure = (laid: Laid[]) => countConflicts(laid.map((l) => l.pts))

test('the sample topology: slots, the side rule and the second go leave fewer crossings and less shared lane than before', () => {
  const { g, lookup } = scene()
  const before = measure(routeLines(lookup, g.edges, false, { rule: false, slots: false, relieve: false }))
  const after = measure(routeLines(lookup, g.edges, false))
  console.log(`# lines ${g.edges.length}: crossings ${before.crossings} -> ${after.crossings}, shared lane ${Math.round(before.overlap)}px -> ${Math.round(after.overlap)}px`)
  assert.ok(after.crossings <= before.crossings, `crossings ${before.crossings} -> ${after.crossings}`)
  assert.ok(after.overlap <= before.overlap, `overlap ${before.overlap} -> ${after.overlap}`)
})

test('the second go never makes things worse', () => {
  const { g, lookup } = scene()
  const without = measure(routeLines(lookup, g.edges, false, { relieve: false }))
  const withIt = measure(routeLines(lookup, g.edges, false))
  assert.ok(withIt.crossings <= without.crossings)
  assert.ok(withIt.overlap <= without.overlap + 1e-6)
})

test('no two ends share a spot on one side of a box, and the ends of one side keep their order', () => {
  const { g, lookup } = scene()
  const laid = routeLines(lookup, g.edges, false)
  const seen = new Map<string, number[]>()
  for (const [i, l] of laid.entries()) {
    const e = g.edges.find((x) => x.id === l.id)!
    for (const [id, p, n] of [[e.source, l.pts[0], l.n1], [e.target, l.pts[l.pts.length - 1], l.n2]] as const) {
      const k = `${id}|${n.x},${n.y}`
      seen.set(k, [...(seen.get(k) ?? []), n.y !== 0 ? p.x : p.y])
      assert.ok(i >= 0)
    }
  }
  for (const [k, v] of seen) {
    const sorted = [...v].sort((a, b) => a - b)
    for (let i = 1; i < sorted.length; i++) assert.ok(sorted[i] - sorted[i - 1] >= 5.9, `${k}: ends ${sorted[i - 1]} and ${sorted[i]} are closer than 6px`)
  }
})

test('a line going down a tier leaves through a bottom; going up, through a top unless that would run over the cluster title', () => {
  const { g, lookup } = scene()
  const laid = routeLines(lookup, g.edges, false)
  let down = 0
  let up = 0
  for (const l of laid) {
    const e = g.edges.find((x) => x.id === l.id)!
    const a = lookup.get(e.source)!
    const b = lookup.get(e.target)!
    const ay = a.internals.positionAbsolute.y
    const by = b.internals.positionAbsolute.y
    if (by >= ay + (a.measured.height ?? 0)) {
      down++
      assert.equal(l.n1.y, 1, `${l.id} goes down and should leave through the bottom`)
    } else if (ay >= by + (b.measured.height ?? 0)) {
      up++
      const parent = a.parentId ? lookup.get(a.parentId) : undefined
      const underTitle = !!parent && ay - parent.internals.positionAbsolute.y - HEADER <= 20
      assert.ok(l.n1.y === -1 || underTitle, `${l.id} goes up and should leave through the top`)
    }
  }
  assert.ok(down > 0 && up > 0, 'the scene has both')
})

test('the sample topology stays tidy: before this was reworked it had 21 crossings and 337px of shared lane', () => {
  const { g, lookup } = scene()
  const now = measure(routeLines(lookup, g.edges, false))
  assert.ok(now.crossings <= 14, `crossings: ${now.crossings}`)
  assert.ok(now.overlap <= 40, `shared lane: ${Math.round(now.overlap)}px`)
})

test('no line runs over the title of a cluster', () => {
  const { g, lookup } = scene()
  const wide = buildGraph(crowded(3) as never, { view: 'application', groupBy: 'cluster', servicesOnNodes: false, links: true, devices: false })
  const dense = lookupOf(wide.nodes)
  const lines = [...routeLines(lookup, g.edges, false).map((l) => ({ l, lookup, nodes: g.nodes })), ...routeLines(dense, wide.edges, false).map((l) => ({ l, lookup: dense, nodes: wide.nodes }))]
  for (const { l, lookup, nodes } of lines) {
    for (const n of nodes) {
      if (n.type !== 'boundary') continue
      const b = lookup.get(n.id)!
      const x0 = b.internals.positionAbsolute.x
      const y0 = b.internals.positionAbsolute.y
      // The title and its subtitle: the left part of the top of the box.
      for (let i = 0; i + 1 < l.pts.length; i++) {
        const a = l.pts[i]
        const c = l.pts[i + 1]
        const over = Math.max(a.x, c.x) > x0 && Math.min(a.x, c.x) < x0 + 260 && Math.max(a.y, c.y) > y0 && Math.min(a.y, c.y) < y0 + 60
        assert.ok(!over, `${l.id} runs over the title of ${n.id}`)
      }
    }
  }
})

test('a cluster neither end is in is one box to go round, with nothing of its inside to search past', () => {
  const { g, lookup } = scene()
  const e = g.edges.find((x) => x.id === 'd15')! // cloud -> a card of edge-patras, past athens-regional
  const all = collectObstacles(lookup, e.source, e.target, { minX: -1e5, maxX: 1e5, minY: -1e5, maxY: 1e5 }, true, true)
  const region = lookup.get('g:cl-region')!
  const whole = all.filter((o) => !o.soft && o.w > (region.measured.width ?? 0) && o.w < (region.measured.width ?? 0) + 60 && o.x < region.internals.positionAbsolute.x && o.y < region.internals.positionAbsolute.y)
  assert.equal(whole.length, 1, 'athens-regional is one whole box')
  const inside = (o: { x: number; y: number }) => o.x >= region.internals.positionAbsolute.x && o.x < region.internals.positionAbsolute.x + (region.measured.width ?? 0) && o.y >= region.internals.positionAbsolute.y && o.y < region.internals.positionAbsolute.y + (region.measured.height ?? 0)
  assert.equal(all.filter((o) => inside(o) && o !== whole[0]).length, 0, 'none of its cards or its header is listed besides')
  // The clusters the line is in stay open: their own cards are still in the way.
  const own = lookup.get('g:cl-edge-a')!
  assert.ok(all.some((o) => !o.soft && o.x > own.internals.positionAbsolute.x && o.w < (own.measured.width ?? 0)), 'edge-patras still lists its cards')
})

/** The sample, copied `k` times (as many clusters, a call from each service to one in a tier above): a graph with a lot of boxes in the way. */
function crowded(k: number) {
  const m = seedTopology() as unknown as Record<string, any[]>
  const rank: Record<string, number> = { cloud: 0, edge: 1, 'far-edge': 2 }
  const out: Record<string, any> = { ...m, clusters: [], services: [], namespaces: [], nodes: [], dependencies: [], devices: [] }
  const at = (id: string, i: number) => (i === 0 ? id : `${id}~${i}`)
  for (let i = 0; i < k; i++) {
    for (const c of m.clusters) out.clusters.push({ ...c, id: at(c.id, i), name: `${c.name}-${i}` })
    for (const s of m.services) out.services.push({ ...s, id: at(s.id, i), clusterId: at(s.clusterId, i), nodeIds: [] })
    for (const n of m.namespaces) out.namespaces.push({ ...n, id: `${n.id}~${i}`, clusterId: at(n.clusterId, i) })
    for (const d of m.dependencies) if (d.fromKind === 'service' && d.toKind === 'service') out.dependencies.push({ ...d, id: `${d.id}~${i}`, from: at(d.from, i), to: at(d.to, i) })
  }
  const tier = new Map<string, number>(out.clusters.map((c: any) => [c.id, rank[c.tier]]))
  const ups = (r: number) => out.services.filter((s: any) => (tier.get(s.clusterId) ?? 0) < r)
  let n = 0
  for (const [i, s] of out.services.entries()) {
    const r = tier.get(s.clusterId) ?? 0
    const above = ups(r)
    if (r === 0 || above.length === 0) continue
    out.dependencies.push({ ...m.dependencies[0], id: `x${n++}`, from: s.id, to: above[(i * 7) % above.length].id, sources: ['declared'], crossCluster: true })
  }
  return out
}

test('with many clusters every line is searched for and none runs through a card', () => {
  const g = buildGraph(crowded(6) as never, { view: 'application', groupBy: 'cluster', servicesOnNodes: false, links: true, devices: false })
  const lookup = lookupOf(g.nodes)
  const laid = routeLines(lookup, g.edges, false)
  const cards = g.nodes.filter((n) => n.type === 'card').map((n) => ({ id: n.id, ...lookup.get(n.id)!.internals.positionAbsolute, w: lookup.get(n.id)!.measured.width!, h: lookup.get(n.id)!.measured.height! }))
  let through = 0
  for (const l of laid) {
    assert.ok(l.input.hard.length + l.input.soft.length <= ORTHO_MAX_OBSTACLES, `${l.id} has ${l.input.hard.length + l.input.soft.length} boxes in the way, more than are searched past`)
    const e = g.edges.find((x) => x.id === l.id)!
    for (let i = 0; i + 1 < l.pts.length; i++) {
      const [a, b] = [l.pts[i], l.pts[i + 1]]
      if (cards.some((c) => c.id !== e.source && c.id !== e.target && Math.max(a.x, b.x) > c.x + 1 && Math.min(a.x, b.x) < c.x + c.w - 1 && Math.max(a.y, b.y) > c.y + 1 && Math.min(a.y, b.y) < c.y + c.h - 1)) {
        through++
        break
      }
    }
  }
  console.log(`# crowded: ${g.edges.length} lines, ${through} run through a card`)
  assert.ok(through <= 5, `${through} lines run through a card`)
})
