import clsx from 'clsx'
import { Target, X } from 'lucide-react'
import { ICON_MD, ICON_SM } from '@/components/ui/primitives'
import { PRESS_CLASS } from '@/components/ui/buttonClass'

/** Backstage-style "max depth" for a selected service: show only its neighbourhood, one, two or three steps out. Pressing the chosen number, or the X, goes back to everything. */
export default function HopsControl({ hops, onChange }: { hops: number | undefined; onChange: (hops: number | undefined) => void }) {
  return (
    <div
      className="flex items-center gap-1 rounded-md border border-nb-800 bg-nb-925 px-1.5 py-1"
      title="Show only this service's neighborhood: what it calls and what calls it, this many steps out"
      data-testid="hops-control"
    >
      <Target size={ICON_SM} className="ml-0.5 shrink-0 text-nb-500" aria-hidden />
      {[1, 2, 3].map((n) => (
        <button
          key={n}
          type="button"
          onClick={() => onChange(hops === n ? undefined : n)}
          className={clsx(
            'rounded px-2 py-0.5 text-xs',
            PRESS_CLASS,
            hops === n ? 'bg-accent-soft text-accent' : 'text-nb-400 hover:text-nb-300',
          )}
          aria-pressed={hops === n}
          data-testid={`hops-${n}`}
        >
          {n}
        </button>
      ))}
      {hops !== undefined && (
        <button
          type="button"
          onClick={() => onChange(undefined)}
          className="rounded px-1 py-0.5 text-nb-500 hover:bg-nb-850 hover:text-nb-300"
          aria-label="Clear neighborhood filter"
          data-testid="hops-clear"
        >
          <X size={ICON_MD} />
        </button>
      )}
    </div>
  )
}
