import { useStore, useViewport, type InternalNode } from '@xyflow/react'
import { useMemo, type ReactNode } from 'react'
import { HEADER, NS_HEADER, type TopoEdge } from '@/features/topology/lib/graph'
import { runsOf, type VRun } from './conflicts'
import { hoppedPath } from './hops'
import { polylineMidpoint } from './detour'
import { cachedOrtho, layoutEdge, orthoCacheCapFor, setOrthoCacheCap, useNodeGeometryKey, SQUARE_CORNER_RADIUS, SquareRoutesContext, type PathObstacle, type RoutedPath } from './OffsetEdge'
import { nudgeRoutes, type Pt, type Rect } from './nudge'
import { orthoRoute, simpleOrthoRoute } from './ortho'
import { assignPorts, type PortEnd } from './ports'
import { relieve } from './relieve'
import { straightenJog } from './straighten'

/** What a line was routed from, kept so it can be routed again when it turns out to be in the others' way. */
type Input = { p1: Pt; n1: Pt; p2: Pt; n2: Pt; hard: PathObstacle[]; soft: PathObstacle[] }
export type Laid = { id: string; pts: Pt[]; n1: Pt; n2: Pt; hasMarker: boolean; input: Input }

/** The lines' routes, all together. `dragging`: a card is being dragged, so only what is cheap is done. The options exist so a test can
 *  switch a step off and measure what it is worth; the page uses the defaults. */
export type RouteOptions = { rule?: boolean; slots?: boolean; relieve?: boolean }
export function routeLines(nodeLookup: Map<string, InternalNode>, edges: TopoEdge[], dragging: boolean, opts: RouteOptions = {}): Laid[] {
  const { rule = true, slots: useSlots = true, relieve: useRelieve = true } = opts
  setOrthoCacheCap(orthoCacheCapFor(edges.length))

  // 1. Sides by rule, then a slot for every end.
  const placed = edges.flatMap((e) => {
    const l = layoutEdge(nodeLookup, e.source, e.target, useSlots ? 0 : (e.data?.sourceOffset ?? 0), useSlots ? 0 : (e.data?.targetOffset ?? 0), true, rule)
    return l ? [{ e, l }] : []
  })
  const axisOf = (p: Pt, n: Pt) => (n.y !== 0 ? p.x : p.y)
  const centreOf = (b: Rect, n: Pt) => (n.y !== 0 ? b.x + b.w / 2 : b.y + b.h / 2)
  const ends: PortEnd[] = placed.flatMap(({ e, l }) => [
    { key: `${e.id}|s`, nodeId: e.source, box: l.sourceBox, normal: l.n1, natural: axisOf(l.p1, l.n1), heading: centreOf(l.targetBox, l.n1), tie: e.id },
    { key: `${e.id}|t`, nodeId: e.target, box: l.targetBox, normal: l.n2, natural: axisOf(l.p2, l.n2), heading: centreOf(l.sourceBox, l.n2), tie: e.id },
  ])
  const slots = useSlots ? assignPorts(ends) : new Map<string, number>()
  const sharing = new Map<string, number>()
  for (const en of ends) sharing.set(`${en.nodeId}|${en.normal.x},${en.normal.y}`, (sharing.get(`${en.nodeId}|${en.normal.x},${en.normal.y}`) ?? 0) + 1)
  const slotted = (p: Pt, n: Pt, v: number | undefined): Pt => (v === undefined ? p : n.y !== 0 ? { x: v, y: p.y } : { x: p.x, y: v })

  // 2. One search per line.
  const routes: Laid[] = []
  for (const { e, l } of placed) {
    const p1 = slotted(l.p1, l.n1, slots.get(`${e.id}|s`))
    const p2 = slotted(l.p2, l.n2, slots.get(`${e.id}|t`))
    const hard = [...l.obstacles, l.sourceBox, l.targetBox]
    const route = cachedOrtho(p1, l.n1, p2, l.n2, [...hard, ...l.soft]) ?? simpleOrthoRoute(p1, l.n1, p2, l.n2)
    const pts = route.map((p) => ({ ...p }))
    // A route that is only a few px of sideways jog becomes one straight run (see straighten.ts); an end in a slot among several stays put.
    const crowded = (id: string, n: Pt) => useSlots && (sharing.get(`${id}|${n.x},${n.y}`) ?? 0) > 1
    if (straightenJog(pts, { source: l.sourceBox, target: l.targetBox, sourceOffset: useSlots ? 0 : (e.data?.sourceOffset ?? 0), targetOffset: useSlots ? 0 : (e.data?.targetOffset ?? 0), sourceLocked: crowded(e.source, l.n1), targetLocked: crowded(e.target, l.n2) })) pts.splice(1, 2)
    routes.push({ id: e.id, pts, n1: l.n1, n2: l.n2, hasMarker: !!e.markerEnd, input: { p1, n1: l.n1, p2, n2: l.n2, hard, soft: l.soft } })
  }

  // While a card is being dragged every line is routed again on each frame, so what only tidies waits until it is dropped.
  if (dragging) return routes

  // 3. The crowded ones, again, knowing where the others are.
  if (useRelieve) {
    relieve(routes, (i, occupied) => {
      const { input } = routes[i]
      const again = orthoRoute(input.p1, input.n1, input.p2, input.n2, input.hard, input.soft, occupied)
      return again ? again.map((p) => ({ ...p })) : null
    })
  }

  // What a lane must not enter: every card, and the header strip of every cluster and namespace.
  const boxes: Rect[] = []
  for (const n of nodeLookup.values()) {
    const w = n.measured.width
    const h = n.measured.height
    if (!w || !h) continue
    const x = n.internals.positionAbsolute.x
    const y = n.internals.positionAbsolute.y
    if (n.type === 'card') boxes.push({ x, y, w, h })
    else if (n.type === 'boundary') boxes.push({ x, y, w, h: Math.min(h, HEADER) })
    else if (n.type === 'namespace') boxes.push({ x, y, w, h: Math.min(h, NS_HEADER) })
  }
  // 4. Whatever still runs together is fanned into lanes.
  nudgeRoutes(routes, boxes)
  return routes
}

/**
 * Routes every line of the canvas together in the squared style and hands each its path through context.
 *
 * 1. Each line takes the side its boxes call for (a call between tiers leaves through a bottom and arrives through a top),
 *    and every end on one side of a box gets its own evenly spaced slot, in the order of where its line is heading (ports.ts).
 * 2. Each line is searched for on its own (cached, so only a line whose ends or neighbours moved is searched again).
 * 3. The lines that cross or run along others are routed again, told where the others are (relieve.ts).
 * 4. `nudgeRoutes` spaces apart the runs that would still overlap or crowd.
 *
 * The ends are pulled back off the boxes last, by a screen-sized gap, so zooming only redoes that cheap last step, not the search.
 */
export function SquareRoutes({ edges, children }: { edges: TopoEdge[]; children: ReactNode }) {
  const nodeLookup = useStore((s) => s.nodeLookup)
  const geometry = useNodeGeometryKey()
  const { zoom } = useViewport()
  // Selection and styling change the edge objects but not where a line goes.
  const key = edges.map((e) => `${e.id}|${e.source}|${e.target}|${e.markerEnd ? 1 : 0}`).join(';')

  const laid = useMemo<Laid[]>(() => routeLines(nodeLookup, edges, geometry.includes('d;')), // eslint-disable-next-line react-hooks/exhaustive-deps -- `key` stands for the parts of `edges` that matter
    [nodeLookup, geometry, key])

  const zoomStep = Math.max(0.1, Math.round(zoom * 25) / 25)
  const paths = useMemo<Map<string, RoutedPath>>(() => {
    const gap = 5 / zoomStep
    const out = new Map<string, RoutedPath>()
    // The ends step off their boxes first, so a line hops over exactly what is drawn, not what was routed.
    const final = new Map<string, Pt[]>()
    for (const r of laid) {
      const pts = r.pts.map((p) => ({ ...p }))
      pts[0] = { x: pts[0].x + r.n1.x * gap, y: pts[0].y + r.n1.y * gap }
      if (!r.hasMarker) {
        const last = pts.length - 1
        pts[last] = { x: pts[last].x + r.n2.x * gap, y: pts[last].y + r.n2.y * gap }
      }
      final.set(r.id, pts)
    }
    const verticals: (VRun & { id: string })[] = [...final].flatMap(([id, pts]) => runsOf(pts).v.map((v) => ({ ...v, id })))
    for (const r of laid) {
      const pts = final.get(r.id)!
      const mid = polylineMidpoint(pts)
      out.set(r.id, { path: hoppedPath(pts, SQUARE_CORNER_RADIUS, verticals.filter((v) => v.id !== r.id)), labelX: mid.x, labelY: mid.y, length: pts.slice(1).reduce((sum, q, i) => sum + Math.hypot(q.x - pts[i].x, q.y - pts[i].y), 0) })
    }
    return out
  }, [laid, zoomStep])

  return <SquareRoutesContext.Provider value={paths}>{children}</SquareRoutesContext.Provider>
}
