import assert from 'node:assert/strict'
import { test } from 'node:test'
import { conflictScore, conflictsWith, countConflicts, runsOf } from '../src/features/topology/components/conflicts'
import { hoppedPath } from '../src/features/topology/components/hops'
import { orthoRoute } from '../src/features/topology/components/ortho'
import { assignPorts, PORT_INSET, PORT_PITCH, sidesByRule, type PortEnd } from '../src/features/topology/components/ports'
import { relieve } from '../src/features/topology/components/relieve'
import { straightenJog } from '../src/features/topology/components/straighten'

const box = (x: number, y: number, w = 200, h = 60) => ({ x, y, w, h })
const end = (key: string, heading: number, natural: number, over: Partial<PortEnd> = {}): PortEnd => ({ key, nodeId: 'n', box: box(0, 0), normal: { x: 0, y: 1 }, natural, heading, tie: key, ...over })

test('a call between tiers uses a bottom and a top; boxes side by side use the facing sides', () => {
  assert.deepEqual(sidesByRule(box(0, 0), box(0, 200)), ['bottom', 'top'])
  assert.deepEqual(sidesByRule(box(0, 200), box(500, 0)), ['top', 'bottom'])
  assert.deepEqual(sidesByRule(box(0, 0), box(300, 10)), ['right', 'left'])
  assert.deepEqual(sidesByRule(box(300, 0), box(0, 10)), ['left', 'right'])
  // Far to the side and well below: the tiers still rule. Far to the side and only a little below: they are neighbours.
  assert.deepEqual(sidesByRule(box(0, 0), box(900, 260)), ['bottom', 'top'])
  assert.deepEqual(sidesByRule(box(0, 0), box(900, 100)), ['right', 'left'])
  assert.equal(sidesByRule(box(0, 0), box(50, 20)), null)
})

test('ends on one side take slots in the order of where their lines head, evenly spaced and centred', () => {
  const out = assignPorts([end('c', 900, 100), end('a', 100, 100), end('b', 500, 100)])
  assert.ok(out.get('a')! < out.get('b')! && out.get('b')! < out.get('c')!)
  assert.equal(out.get('b')! - out.get('a')!, PORT_PITCH)
  assert.equal(out.get('c')! - out.get('b')!, PORT_PITCH)
  assert.equal(out.get('b'), 100)
})

test('a lone end stays level with the other end, but never closer to a corner than the inset', () => {
  assert.equal(assignPorts([end('a', 0, 120)]).get('a'), 120)
  assert.equal(assignPorts([end('a', 0, -50)]).get('a'), PORT_INSET)
  assert.equal(assignPorts([end('a', 0, 999)]).get('a'), 200 - PORT_INSET)
})

test('a crowded side squeezes the slots together instead of leaving the box', () => {
  const ends = Array.from({ length: 30 }, (_, i) => end(`e${i}`, i, 100))
  const out = [...assignPorts(ends).values()]
  assert.ok(Math.min(...out) >= PORT_INSET - 1e-9)
  assert.ok(Math.max(...out) <= 200 - PORT_INSET + 1e-9)
  assert.equal(new Set(out.map((v) => v.toFixed(3))).size, 30)
})

test('two lines heading the same way keep one order at both of their boxes', () => {
  const a = assignPorts([end('x|s', 400, 100, { nodeId: 'p' }), end('y|s', 400, 100, { nodeId: 'p' })])
  const b = assignPorts([end('x|t', 400, 100, { nodeId: 'q' }), end('y|t', 400, 100, { nodeId: 'q' })])
  assert.ok(a.get('x|s')! < a.get('y|s')!)
  assert.ok(b.get('x|t')! < b.get('y|t')!)
})

test('crossings and shared lane between squared routes are counted', () => {
  const horizontal = [{ x: 0, y: 50 }, { x: 100, y: 50 }]
  const vertical = [{ x: 50, y: 0 }, { x: 50, y: 100 }]
  assert.deepEqual(countConflicts([horizontal, vertical]), { crossings: 1, overlap: 0 })
  // Touching at an end is not crossing.
  assert.deepEqual(countConflicts([horizontal, [{ x: 100, y: 0 }, { x: 100, y: 50 }]]), { crossings: 0, overlap: 0 })
  assert.deepEqual(countConflicts([horizontal, [{ x: 40, y: 50 }, { x: 160, y: 50 }]]), { crossings: 0, overlap: 60 })
  assert.ok(conflictScore({ crossings: 1, overlap: 0 }) > conflictScore({ crossings: 0, overlap: 12 }))
})

test('a line told where the others are goes round them when a way exists', () => {
  const wall = [{ x: 100, y: -200 }, { x: 100, y: 200 }]
  const p1 = { x: 0, y: 0 }
  const p2 = { x: 200, y: 0 }
  const right = { x: 1, y: 0 }
  const left = { x: -1, y: 0 }
  const alone = orthoRoute(p1, right, p2, left, [])!
  assert.equal(conflictsWith(alone, { h: [], v: runsOf(wall).v }).crossings, 1)
  const told = orthoRoute(p1, right, p2, left, [], [], { h: [], v: runsOf(wall).v })!
  // The wall is too long to go round inside this grid, so crossing is still the only way, but it is still one crossing and never more.
  assert.ok(conflictsWith(told, { h: [], v: runsOf(wall).v }).crossings <= 1)
  // A short wall can be gone round: the route then crosses nothing.
  const short = runsOf([{ x: 100, y: -5 }, { x: 100, y: 5 }])
  const around = orthoRoute(p1, right, p2, left, [{ x: 95, y: -5, w: 10, h: 10 }], [], { h: [], v: short.v })!
  assert.equal(conflictsWith(around, { h: [], v: short.v }).crossings, 0)
})

test('the second go keeps a new route only when it is less crowded', () => {
  // Two lines on the same y, one of which has a way round and one of which has none.
  const routes = [
    { pts: [{ x: 0, y: 0 }, { x: 100, y: 0 }] },
    { pts: [{ x: 50, y: -50 }, { x: 50, y: 50 }] },
  ]
  const before = routes.map((r) => r.pts)
  let calls = 0
  const moved = relieve(routes, (i) => {
    calls++
    return i === 1 ? [{ x: 150, y: -50 }, { x: 150, y: 50 }] : [{ x: 0, y: 0 }, { x: 100, y: 0 }]
  })
  assert.ok(calls > 0)
  assert.equal(moved, 1, 'only the route that really got better was replaced')
  assert.equal(countConflicts(routes.map((r) => r.pts)).crossings, 0)
  assert.deepEqual(routes[0].pts, before[0])
  // Offered something worse, it keeps what it has.
  const keep = [{ pts: [{ x: 0, y: 0 }, { x: 100, y: 0 }] }, { pts: [{ x: 150, y: -50 }, { x: 150, y: 50 }] }]
  assert.equal(relieve(keep, () => [{ x: 50, y: -50 }, { x: 50, y: 50 }]), 0)
})

test('a line hops over the vertical lines it crosses, and only those it really crosses', () => {
  const pts = [{ x: 0, y: 100 }, { x: 300, y: 100 }]
  const d = hoppedPath(pts, 12, [{ x: 100, lo: 0, hi: 200 }, { x: 200, lo: 150, hi: 250 }])
  assert.equal((d.match(/A/g) ?? []).length, 1)
  assert.match(d, /A5,5 0 0 1 105,100/)
  // Too near an end to hop, or two crossings closer than a hop: no arc / just the first.
  assert.equal((hoppedPath(pts, 12, [{ x: 4, lo: 0, hi: 200 }]).match(/A/g) ?? []).length, 0)
  assert.equal((hoppedPath(pts, 12, [{ x: 100, lo: 0, hi: 200 }, { x: 106, lo: 0, hi: 200 }]).match(/A/g) ?? []).length, 1)
  // Going the other way the hop curves the same way up.
  assert.match(hoppedPath([{ x: 300, y: 100 }, { x: 0, y: 100 }], 12, [{ x: 100, lo: 0, hi: 200 }]), /A5,5 0 0 0 95,100/)
})

test('a path with no crossings is the plain rounded polyline', () => {
  const d = hoppedPath([{ x: 0, y: 0 }, { x: 100, y: 0 }, { x: 100, y: 100 }], 12, [])
  assert.equal(d, 'M0,0 L88,0 Q100,0 100,12 L100,100')
})

test('an end in a slot among several is not moved to straighten a jog', () => {
  const jog = () => [{ x: 0, y: 0 }, { x: 0, y: 20 }, { x: 8, y: 20 }, { x: 8, y: 40 }]
  const ends = { source: box(-100, -60), target: box(-100, 40), sourceOffset: 0, targetOffset: 0 }
  const free = jog()
  assert.equal(straightenJog(free, ends), true)
  const locked = jog()
  assert.equal(straightenJog(locked, { ...ends, sourceLocked: true, targetLocked: true }), false)
  assert.deepEqual(locked, jog())
})
