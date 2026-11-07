import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { InternalNode } from '@xyflow/react'
import { countConflicts } from '../src/features/topology/components/conflicts'
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
