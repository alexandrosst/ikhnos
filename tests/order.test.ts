import assert from 'node:assert/strict'
import { test } from 'node:test'
import { orderRows, wrapRow } from '../src/features/topology/lib/order'

test('the alphabet stays when nothing is joined', () => {
  assert.deepEqual(orderRows([['a'], ['x', 'y', 'z']], []), [['a'], ['x', 'y', 'z']])
})

test('a cluster stands under the one it talks to, not where the alphabet puts it', () => {
  // Below, alphabetically: a, b, c; but "a" talks to the right-hand one above and "c" to the left-hand one.
  const rows = [['left', 'right'], ['a', 'b', 'c']]
  const out = orderRows(rows, [['right', 'a'], ['left', 'c']])
  assert.ok(out[1].indexOf('c') < out[1].indexOf('a'))
  // Nothing moves between rows.
  assert.deepEqual([...out[0]].sort(), ['left', 'right'])
  assert.deepEqual([...out[1]].sort(), ['a', 'b', 'c'])
})

test('a cluster joined to nothing keeps a place in its row', () => {
  const out = orderRows([['p', 'q'], ['a', 'b', 'c']], [['q', 'c']])
  assert.equal(out[1].length, 3)
  assert.ok(out[1].includes('b'))
})

test('the same data gives the same order, whatever order the links come in', () => {
  const rows = [['t1', 't2', 't3'], ['m', 'n', 'o'], ['d1', 'd2']]
  const links = [['t1', 'o'], ['t3', 'm'], ['m', 'd2'], ['o', 'd1'], ['t2', 'n']] as const
  assert.deepEqual(orderRows(rows, [...links].reverse()), orderRows(rows, links))
})

test('two clusters in one row, an unknown key and a cluster joined to itself pull nothing', () => {
  assert.deepEqual(orderRows([['a', 'b'], ['c']], [['a', 'b'], ['a', 'a'], ['a', 'ghost']]), [['a', 'b'], ['c']])
})

test('two chains across three tiers come out uncrossed', () => {
  const out = orderRows([['x', 'y'], ['a', 'b'], ['m', 'n']], [['y', 'b'], ['b', 'm'], ['x', 'a'], ['a', 'n']])
  const crosses = (u: string[], l: string[], p: [string, string], q: [string, string]) => (u.indexOf(p[0]) - u.indexOf(q[0])) * (l.indexOf(p[1]) - l.indexOf(q[1])) < 0
  assert.equal(crosses(out[0], out[1], ['y', 'b'], ['x', 'a']), false)
  assert.equal(crosses(out[1], out[2], ['b', 'm'], ['a', 'n']), false)
})

test('a tier that fits stays one line', () => {
  assert.equal(wrapRow([{ w: 400 }, { w: 400 }], 64, 2400).length, 1)
})

test('a wide tier breaks into lines no wider than the limit, in order, evened out', () => {
  const row = Array.from({ length: 7 }, (_, i) => ({ w: 460, id: i }))
  const lines = wrapRow(row, 64, 2000)
  assert.deepEqual(lines.flat().map((b) => b.id), [0, 1, 2, 3, 4, 5, 6])
  for (const l of lines) assert.ok(l.reduce((s, b) => s + b.w, 0) + 64 * (l.length - 1) <= 2000)
  assert.ok(Math.min(...lines.map((l) => l.length)) >= 2)
})

test('a box wider than the limit gets a line of its own', () => {
  assert.deepEqual(wrapRow([{ w: 100 }, { w: 5000 }, { w: 100 }], 10, 2000).map((l) => l.length), [1, 1, 1])
})
