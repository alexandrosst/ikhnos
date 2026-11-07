/**
 * Crossings and overlaps between squared lines.
 *
 * Lines are routed one by one, each unaware of the others. This measures how much one route runs over or across the
 * rest (so the worst can be routed again, this time paying for it), and is what the routing test counts.
 *
 * A *crossing* is a horizontal run and a vertical run of two different lines meeting in the inside of both; an *overlap* is
 * the length (px) two parallel runs of different lines share while lying on top of each other. Pure.
 */

export type Pt = { x: number; y: number }

/** Runs closer together than this are one on top of the other. */
const SAME_LANE = 2.5
/** A run must be this much longer than a point to count. */
const MIN_LEN = 1

export type HRun = { y: number; lo: number; hi: number }
export type VRun = { x: number; lo: number; hi: number }
export type Runs = { h: HRun[]; v: VRun[] }

/** The horizontal and vertical runs of a route. */
export function runsOf(pts: readonly Pt[]): Runs {
  const h: HRun[] = []
  const v: VRun[] = []
  for (let i = 0; i + 1 < pts.length; i++) {
    const a = pts[i]
    const b = pts[i + 1]
    if (Math.abs(a.y - b.y) < 1e-6 && Math.abs(a.x - b.x) > MIN_LEN) h.push({ y: a.y, lo: Math.min(a.x, b.x), hi: Math.max(a.x, b.x) })
    else if (Math.abs(a.x - b.x) < 1e-6 && Math.abs(a.y - b.y) > MIN_LEN) v.push({ x: a.x, lo: Math.min(a.y, b.y), hi: Math.max(a.y, b.y) })
  }
  return { h, v }
}

/** Merged runs of several routes. */
export function mergeRuns(all: readonly Runs[]): Runs {
  return { h: all.flatMap((r) => r.h), v: all.flatMap((r) => r.v) }
}

/** What a step along `y` from `x0` to `x1` (horizontal) or along `x` from `y0` to `y1` (vertical) costs in company: how many
 *  of `others`' runs it crosses and how many px it lies on top of. */
export function stepConflicts(others: Runs, horizontal: boolean, at: number, from: number, to: number): { crossings: number; overlap: number } {
  const lo = Math.min(from, to)
  const hi = Math.max(from, to)
  let crossings = 0
  let overlap = 0
  if (horizontal) {
    for (const r of others.v) if (r.x > lo + 1e-6 && r.x < hi - 1e-6 && at > r.lo + 1e-6 && at < r.hi - 1e-6) crossings++
    for (const r of others.h) if (Math.abs(r.y - at) < SAME_LANE) overlap += Math.max(0, Math.min(hi, r.hi) - Math.max(lo, r.lo))
  } else {
    for (const r of others.h) if (r.y > lo + 1e-6 && r.y < hi - 1e-6 && at > r.lo + 1e-6 && at < r.hi - 1e-6) crossings++
    for (const r of others.v) if (Math.abs(r.x - at) < SAME_LANE) overlap += Math.max(0, Math.min(hi, r.hi) - Math.max(lo, r.lo))
  }
  return { crossings, overlap }
}

/** How much `pts` crosses and overlaps `others`. */
export function conflictsWith(pts: readonly Pt[], others: Runs): { crossings: number; overlap: number } {
  const mine = runsOf(pts)
  let crossings = 0
  let overlap = 0
  for (const r of mine.h) {
    const c = stepConflicts(others, true, r.y, r.lo, r.hi)
    crossings += c.crossings
    overlap += c.overlap
  }
  for (const r of mine.v) {
    const c = stepConflicts(others, false, r.x, r.lo, r.hi)
    crossings += c.crossings
    overlap += c.overlap
  }
  return { crossings, overlap }
}

/** One number for how bad a route's company is: a crossing counts for a good deal more than a stretch of shared lane. */
export const conflictScore = (c: { crossings: number; overlap: number }) => c.crossings * 3 + c.overlap / 12

/** Total crossings and overlap (px) among all `routes`, each pair counted once. For tests and for judging a layout. */
export function countConflicts(routes: readonly (readonly Pt[])[]): { crossings: number; overlap: number } {
  const runs = routes.map(runsOf)
  let crossings = 0
  let overlap = 0
  for (let i = 0; i < runs.length; i++) {
    for (let j = i + 1; j < runs.length; j++) {
      for (const r of runs[i].h) {
        crossings += stepConflicts({ h: [], v: runs[j].v }, true, r.y, r.lo, r.hi).crossings
        overlap += stepConflicts({ h: runs[j].h, v: [] }, true, r.y, r.lo, r.hi).overlap
      }
      for (const r of runs[i].v) {
        crossings += stepConflicts({ h: runs[j].h, v: [] }, false, r.x, r.lo, r.hi).crossings
        overlap += stepConflicts({ h: [], v: runs[j].v }, false, r.x, r.lo, r.hi).overlap
      }
    }
  }
  return { crossings, overlap }
}
