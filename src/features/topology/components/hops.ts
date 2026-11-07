/**
 * A squared line that hops over the lines it crosses.
 *
 * Where two lines cross, the horizontal one steps over the vertical one with a small arc, so a crossing reads as "passes
 * over" and not as a junction where the two might be joined. The corners are rounded the same way as everywhere else.
 */
import type { Pt, VRun } from './conflicts'

/** How far a hop reaches to each side of the line it crosses (px, flow space). */
export const HOP = 5

/** `vertical` are the vertical runs of every other line. Pure. */
export function hoppedPath(pts: readonly Pt[], radius: number, vertical: readonly VRun[], hop = HOP): string {
  const n = pts.length
  if (n < 2) return ''
  const dist = (a: Pt, b: Pt) => Math.hypot(b.x - a.x, b.y - a.y)
  // Each corner is rounded by at most half of either run that meets there.
  const rad = pts.map((p, i) => (i === 0 || i === n - 1 ? 0 : Math.min(radius, dist(pts[i - 1], p) / 2, dist(p, pts[i + 1]) / 2)))
  let d = `M${pts[0].x},${pts[0].y}`
  for (let i = 0; i < n - 1; i++) {
    const a = pts[i]
    const b = pts[i + 1]
    const len = dist(a, b)
    if (len < 1e-6) continue
    const ux = (b.x - a.x) / len
    const uy = (b.y - a.y) / len
    const s = { x: a.x + ux * rad[i], y: a.y + uy * rad[i] }
    const z = { x: b.x - ux * rad[i + 1], y: b.y - uy * rad[i + 1] }
    if (i > 0) d += ` Q${a.x},${a.y} ${s.x},${s.y}`
    if (Math.abs(uy) < 1e-6) {
      const lo = Math.min(s.x, z.x) + hop + 1
      const hi = Math.max(s.x, z.x) - hop - 1
      const xs = vertical
        .filter((v) => v.x > lo && v.x < hi && a.y > v.lo + 1e-6 && a.y < v.hi - 1e-6)
        .map((v) => v.x)
        .sort((p, q) => (ux > 0 ? p - q : q - p))
      let last = -Infinity
      for (const x of xs) {
        // Two crossings closer than a hop apart would overlap their arcs: take the first.
        if (Math.abs(x - last) < 2 * hop + 2) continue
        d += ` L${x - ux * hop},${a.y} A${hop},${hop} 0 0 ${ux > 0 ? 1 : 0} ${x + ux * hop},${a.y}`
        last = x
      }
    }
    d += ` L${z.x},${z.y}`
  }
  return d
}
