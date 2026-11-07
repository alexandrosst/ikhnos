/**
 * Which cluster stands where inside its tier, and when a tier breaks into more than one line.
 *
 * A tier is a row of clusters. Left to the alphabet, two clusters that talk to each other can sit at opposite ends of
 * the row, and their line has to cross everything between them. Each cluster is instead placed near the average
 * position of the clusters it is joined to in the other rows (the barycenter rule layered diagrams use), sweeping down
 * the tiers and back up a couple of times. A cluster that is joined to nothing, or whose neighbours all sit at the same
 * place, keeps its alphabetical place, so the result is the same every time for the same data.
 *
 * Pure functions of keys and weights, so they are testable without rendering anything.
 */

/** A link between two groups (by key); `weight` is how many lines join them (default 1). Direction does not matter. */
export type GroupLink = readonly [a: string, b: string, weight?: number]

/** How many down-and-up sweeps to make: the second one only settles what the first moved. */
const SWEEPS = 2

/**
 * `rows` are group keys in their starting (alphabetical) order, top tier first. Returns the same keys, reordered within
 * each row, never moved between rows.
 */
export function orderRows(rows: readonly (readonly string[])[], links: readonly GroupLink[]): string[][] {
  const out = rows.map((r) => [...r])
  if (out.length < 2 || links.length === 0) return out

  const rowOf = new Map<string, number>()
  out.forEach((r, i) => r.forEach((k) => rowOf.set(k, i)))
  const adj = new Map<string, Map<string, number>>()
  const add = (a: string, b: string, w: number) => {
    if (!adj.has(a)) adj.set(a, new Map())
    adj.get(a)!.set(b, (adj.get(a)!.get(b) ?? 0) + w)
  }
  for (const [a, b, w = 1] of links) {
    // Two groups in the same row do not pull each other, and an unknown key is ignored.
    if (a === b || rowOf.get(a) === undefined || rowOf.get(b) === undefined || rowOf.get(a) === rowOf.get(b)) continue
    add(a, b, w)
    add(b, a, w)
  }
  if (adj.size === 0) return out

  // Where a group stands along its row, as a share of the row (0..1), so rows of different lengths compare.
  const place = () => {
    const pos = new Map<string, number>()
    out.forEach((r) => r.forEach((k, i) => pos.set(k, (i + 0.5) / r.length)))
    return pos
  }

  const settle = (i: number, from: (j: number) => boolean) => {
    const pos = place()
    const row = out[i]
    const keyed = row.map((k, idx) => {
      let sum = 0
      let weight = 0
      for (const [n, w] of adj.get(k) ?? []) {
        if (!from(rowOf.get(n)!)) continue
        sum += (pos.get(n) ?? 0) * w
        weight += w
      }
      // Nothing to be near on this side: stay where it is.
      return { k, idx, at: weight > 0 ? sum / weight : pos.get(k)! }
    })
    keyed.sort((a, b) => a.at - b.at || a.idx - b.idx)
    out[i] = keyed.map((x) => x.k)
  }

  for (let s = 0; s < SWEEPS; s++) {
    for (let i = 1; i < out.length; i++) settle(i, (j) => j < i)
    for (let i = out.length - 2; i >= 0; i--) settle(i, (j) => j > i)
  }
  return out
}

/**
 * Splits one row of boxes into lines no wider than `max` (px), filling each line before starting the next, so a tier with
 * a great many clusters becomes a few lines instead of one strip that "fit to view" has to shrink to a smudge. A single
 * box wider than `max` still gets a line of its own. The order is kept.
 */
export function wrapRow<T extends { w: number }>(row: readonly T[], gap: number, max: number): T[][] {
  const fill = (limit: number) => {
    const lines: T[][] = []
    let cur: T[] = []
    let width = 0
    for (const b of row) {
      const next = cur.length === 0 ? b.w : width + gap + b.w
      if (cur.length > 0 && next > limit) {
        lines.push(cur)
        cur = [b]
        width = b.w
      } else {
        cur.push(b)
        width = next
      }
    }
    if (cur.length > 0) lines.push(cur)
    return lines
  }
  const first = fill(max)
  if (first.length < 2) return first
  // The same number of lines, but evened out: each box goes to the line its middle falls in when the whole tier is cut into
  // equal lengths. Kept only if no line ends up past the limit.
  const n = first.length
  const total = row.reduce((s, b) => s + b.w, 0) + gap * (row.length - 1)
  const even: T[][] = Array.from({ length: n }, () => [])
  let at = 0
  for (const b of row) {
    even[Math.min(n - 1, Math.floor((at + b.w / 2) / (total / n)))].push(b)
    at += b.w + gap
  }
  const widthOf = (l: readonly T[]) => l.reduce((s, b) => s + b.w, 0) + gap * (l.length - 1)
  const ok = even.every((l) => l.length > 0 && (widthOf(l) <= max || l.length === 1))
  return ok ? even : first
}

/** Where something a card is joined to stands relative to the card's own cluster: `rows` lines down (negative: up), `dx` px to the right (negative: left). */
export type Pull = { rows: number; dx: number }

/**
 * Orders a cluster's cards so that those joined to something above come first (top row), those joined to something below
 * last, and within a band those joined to things on the left come before those joined to the right. A card is laid out row by
 * row, so this puts each card on the side of its cluster that faces what it talks to, and its lines leave without crossing
 * its neighbours'. A card with nothing outside its cluster keeps its place in the middle; ties keep the incoming order.
 */
export function orderCards<T>(items: readonly T[], keyOf: (item: T) => string, pulls: ReadonlyMap<string, readonly Pull[]>): T[] {
  const sign = (n: number) => (n > 1e-6 ? 1 : n < -1e-6 ? -1 : 0)
  const scored = items.map((item, idx) => {
    const ps = pulls.get(keyOf(item)) ?? []
    if (ps.length === 0) return { item, idx, band: 0, h: 0 }
    const v = ps.reduce((s, p) => s + sign(p.rows), 0) / ps.length
    const h = ps.reduce((s, p) => s + sign(p.dx), 0) / ps.length
    return { item, idx, band: v < -0.25 ? -1 : v > 0.25 ? 1 : 0, h }
  })
  scored.sort((a, b) => a.band - b.band || a.h - b.h || a.idx - b.idx)
  return scored.map((s) => s.item)
}
