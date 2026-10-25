import type { LinkState } from '@/features/pipeline/lib/pipelineFlow'

/**
 * The line from one node to the next, arrowhead included, as one SVG: shaft and head share a stroke (same colour, same 2px weight), so they
 * join without a seam and stay sharp at any zoom. The drawing is `LEN` long, far more than any gap between nodes, and pinned to the end of its
 * box ("max" + slice), the part that does not fit cropped away at the node it starts from, so on every hop the tip stops `GAP` short of the next node, and the dashes of a moving or
 * unknown line are laid out backwards from the tip and end in one fixed solid dash, so they run into the head at every moment of the animation. `v` is the narrow-screen,
 * downward one: the same drawing with its two axes swapped.
 */
const LEN = 3000, GAP = 4, ARM = 4, MID = 6, DASH = 5, PERIOD = 9
const TIP = LEN - GAP - 1 // the corner of the head; its round join reaches one more
export function Arrow({ link, v, className }: { link: LinkState; v: boolean; className?: string }) {
  const pt = (along: number, across: number) => (v ? `${across} ${along}` : `${along} ${across}`)
  const dashed = link === 'flowing' || link === 'unknown'
  return (
    <svg
      viewBox={v ? `0 0 ${2 * MID} ${LEN}` : `0 0 ${LEN} ${2 * MID}`}
      preserveAspectRatio={v ? 'xMidYMax slice' : 'xMaxYMid slice'}
      className={className ?? (v ? 'absolute inset-y-0 left-[15px] h-full w-3 xl:hidden' : 'absolute left-0 top-[19px] hidden h-3 w-full xl:block')}
      aria-hidden
      focusable="false"
    >
      <g fill="none" stroke="currentColor" strokeWidth={2} strokeLinejoin="round">
        <path d={`M${pt(dashed ? (TIP - DASH) % PERIOD : 0, MID)}L${pt(TIP, MID)}`} className={link === 'flowing' ? 'flow-dash' : undefined} strokeDasharray={link === 'unknown' ? `${DASH} ${PERIOD - DASH}` : undefined} />
        {/* A dashed line always ends in one solid dash fixed to the head, so the moving dashes run into it instead of stopping short of the tip. */}
        {dashed && <path d={`M${pt(TIP - DASH, MID)}L${pt(TIP, MID)}`} />}
        <path d={`M${pt(TIP - ARM, MID - ARM)}L${pt(TIP, MID)}L${pt(TIP - ARM, MID + ARM)}`} />
      </g>
    </svg>
  )
}
