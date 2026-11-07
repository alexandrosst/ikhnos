/**
 * Giving the crowded lines a second go.
 *
 * Every line is first routed on its own, so two of them can cross, or run along one another, for no better reason than that
 * neither knew about the other. This takes the lines with the worst company and routes each again, this time told where
 * the others are (and charged for crossing or sharing them), keeping the new route only if it really is less crowded.
 * The ends do not move, only what is between them.
 */
import { conflictScore, conflictsWith, mergeRuns, runsOf, type Pt, type Runs } from './conflicts'

/** Past this many lines the pairwise bookkeeping is not worth it. */
export const RELIEVE_MAX_LINES = 160
/** At most this many lines are routed again in one pass. */
const PER_PASS = 40
const PASSES = 2

/**
 * `reroute(i, occupied)` gives line `i`'s route when `occupied` (the other lines' runs) must be paid for, or null when
 * there is none. Replaces `routes[i].pts` where that is better; returns how many it replaced.
 */
export function relieve(routes: { pts: Pt[] }[], reroute: (i: number, occupied: Runs) => Pt[] | null): number {
  if (routes.length < 2 || routes.length > RELIEVE_MAX_LINES) return 0
  let moved = 0
  const runs = routes.map((r) => runsOf(r.pts))
  const others = (i: number): Runs => mergeRuns(runs.filter((_, j) => j !== i))
  const scoreOf = (i: number) => conflictScore(conflictsWith(routes[i].pts, others(i)))
  for (let pass = 0; pass < PASSES; pass++) {
    const worst = routes
      .map((_, i) => ({ i, s: scoreOf(i) }))
      .filter((x) => x.s > 0)
      .sort((a, b) => b.s - a.s || a.i - b.i)
      .slice(0, PER_PASS)
    if (worst.length === 0) break
    let changed = false
    for (const { i } of worst) {
      const rest = others(i)
      const now = conflictScore(conflictsWith(routes[i].pts, rest))
      if (now === 0) continue
      const cand = reroute(i, rest)
      if (!cand) continue
      if (conflictScore(conflictsWith(cand, rest)) < now) {
        routes[i].pts = cand
        runs[i] = runsOf(cand)
        changed = true
        moved++
      }
    }
    if (!changed) break
  }
  return moved
}
