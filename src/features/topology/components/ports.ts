/**
 * Where each line meets its box.
 *
 * A line's side of a box is settled first (see `sidesByRule`). Everything that meets one side of one box is then given its
 * own evenly spaced slot along it, in the order of where the other end of its line is, so two lines leaving the same side
 * never cross each other at the box, and the ends sit in a tidy row instead of wherever each line happened to come from.
 * A side with a single line keeps it level with the other end (so a straight run stays straight).
 *
 * Pure functions of the geometry, so they are testable without rendering anything.
 */

export type Pt = { x: number; y: number }
export type Rect = { x: number; y: number; w: number; h: number }
export type Side = 'top' | 'bottom' | 'left' | 'right'

/** Distance between two neighbouring ends on one side (px, flow space). */
export const PORT_PITCH = 14
/** How far in from a corner an end may sit (px). */
export const PORT_INSET = 14

const NORMAL: Record<Side, Pt> = { top: { x: 0, y: -1 }, bottom: { x: 0, y: 1 }, left: { x: -1, y: 0 }, right: { x: 1, y: 0 } }
export const normalOf = (side: Side): Pt => NORMAL[side]

/** The gap between two boxes along one axis: how far apart they are there, or -1 when they overlap along it. */
function gap(aLo: number, aHi: number, bLo: number, bHi: number): number {
  if (bLo >= aHi) return bLo - aHi
  if (aLo >= bHi) return aLo - bHi
  return -1
}

/**
 * Which side of each box a line between them uses, from where the boxes stand: when one is below (or above) the other the
 * line leaves and arrives through the bottom and top, as the tiers are rows and a call between tiers runs up or down; when
 * they only stand side by side it uses the facing sides. Two boxes that overlap on both axes have no rule (null).
 */
export function sidesByRule(a: Rect, b: Rect): [Side, Side] | null {
  const v = gap(a.y, a.y + a.h, b.y, b.y + b.h)
  const h = gap(a.x, a.x + a.w, b.x, b.x + b.w)
  const vertical = v >= 0 && (h < 0 || v >= h * 0.25)
  if (vertical) return b.y >= a.y + a.h ? ['bottom', 'top'] : ['top', 'bottom']
  if (h >= 0) return b.x >= a.x + a.w ? ['right', 'left'] : ['left', 'right']
  return null
}

/** One end of one line, on one side of one box. */
export type PortEnd = {
  /** Unique per end (the line's id and which end). */
  key: string
  nodeId: string
  box: Rect
  normal: Pt
  /** Where along the side (x for a top or bottom side, y for a left or right one) the line would meet it, level with the other end. */
  natural: number
  /** Where the other end's box is centred along that same axis: what orders the ends. */
  heading: number
  /** Breaks a tie between two ends heading the same way. */
  tie: string
}

const axisX = (normal: Pt) => normal.y !== 0

/** The position along each end's side (a coordinate on that side's axis), by `PortEnd.key`. */
export function assignPorts(ends: readonly PortEnd[]): Map<string, number> {
  const groups = new Map<string, PortEnd[]>()
  for (const e of ends) {
    const k = `${e.nodeId}|${e.normal.x},${e.normal.y}`
    if (!groups.has(k)) groups.set(k, [])
    groups.get(k)!.push(e)
  }
  const out = new Map<string, number>()
  for (const group of groups.values()) {
    const { box, normal } = group[0]
    const x = axisX(normal)
    const lo = (x ? box.x : box.y) + PORT_INSET
    const hi = (x ? box.x + box.w : box.y + box.h) - PORT_INSET
    const clamp = (v: number) => (hi < lo ? (lo + hi) / 2 : Math.min(hi, Math.max(lo, v)))
    if (group.length === 1) {
      out.set(group[0].key, clamp(group[0].natural))
      continue
    }
    const sorted = [...group].sort((a, b) => a.heading - b.heading || a.natural - b.natural || a.tie.localeCompare(b.tie))
    const k = sorted.length
    const room = Math.max(0, hi - lo)
    const pitch = k > 1 ? Math.min(PORT_PITCH, room / (k - 1)) : 0
    const span = pitch * (k - 1)
    // Centred where the lines wanted to be, and kept on the side.
    const want = sorted.reduce((s, e) => s + e.natural, 0) / k
    const start = Math.min(Math.max(want - span / 2, lo), Math.max(lo, hi - span))
    sorted.forEach((e, i) => out.set(e.key, hi < lo ? (lo + hi) / 2 : start + i * pitch))
  }
  return out
}
