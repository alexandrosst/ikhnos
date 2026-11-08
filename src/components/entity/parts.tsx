import clsx from 'clsx'
import { Lock, Pencil, Radar, UserRound } from 'lucide-react'
import type { ReactNode } from 'react'
import { ICON_SM } from '@/components/ui/primitives'
import type { Origin } from '@/lib/fieldOrigin'
import { ageLabel, observation } from '@/lib/provenance'
import type { Provenance } from '@/lib/types'

/** The small mark at the end of a row: a lock where an agent reports the value, a pencil where you set it. Nothing for a record you declared. */
export function OriginMark({ origin, by }: { origin: Origin; by?: string }) {
  if (!origin) return null
  return origin === 'detected' ? (
    <span title="Reported by an agent. Edit to override it." className="text-nb-500"><Lock size={ICON_SM} aria-label="Detected" /></span>
  ) : (
    <span title={by ? `Set by ${by}. Kept when the record is rediscovered.` : 'Set by you. Kept when the record is rediscovered.'} className="text-accent"><Pencil size={ICON_SM} aria-label="Set by you" /></span>
  )
}

/** A group of related rows with its own small heading. */
export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mt-5 first:mt-0">
      <h3 className="mb-1 text-[11px] font-medium uppercase tracking-wider text-nb-500">{title}</h3>
      <div>{children}</div>
    </section>
  )
}

/** One fact in a read-only panel: the label, the value, and where it comes from. An empty value reads as a quiet dash. */
export function Row({ label, children, origin, by, mono }: { label: string; children?: ReactNode; origin?: Origin; by?: string; mono?: boolean }) {
  const empty = children === undefined || children === null || children === '' || children === false
  return (
    <div className="grid grid-cols-[8.25rem_1fr_1.25rem] items-center gap-3 border-b border-nb-850 py-2 text-sm last:border-b-0" data-testid="panel-row">
      <dt className="text-nb-500">{label}</dt>
      <dd className={clsx('min-w-0 break-words', empty ? 'text-nb-600' : 'text-nb-300', mono && 'font-mono text-xs')}>{empty ? '—' : children}</dd>
      <span className="grid place-items-center">{!empty && <OriginMark origin={origin} by={by} />}</span>
    </div>
  )
}

/** The second line of a list's name cell: where the record comes from and how fresh it is. One wording for every list. */
export function OriginLine({ rec, createdAt }: { rec: Pick<Provenance, 'source' | 'state' | 'stateReason' | 'stale' | 'deletedAt' | 'lastSeen'>; createdAt?: string }) {
  const obs = observation(rec)
  if (rec.source === 'discovered') {
    const seen = ageLabel(rec.lastSeen)
    const live = !obs || obs.kind === 'live'
    return (
      <div className={clsx('mt-0.5 flex items-center gap-1.5 text-xs', live ? 'text-nb-500' : obs.tone === 'bad' ? 'text-bad' : obs.tone === 'warn' ? 'text-warn' : 'text-nb-500')} title={obs?.reason}>
        <Radar size={12} aria-hidden className="shrink-0" />
        {live ? `Detected${seen ? ` · ${seen} ago` : ''}` : `Detected · ${obs.label}`}
      </div>
    )
  }
  const age = ageLabel(createdAt)
  return (
    <div className="mt-0.5 flex items-center gap-1.5 text-xs text-nb-500">
      <UserRound size={12} aria-hidden className="shrink-0" />
      Declared by you{age ? ` · ${age} old` : ''}
    </div>
  )
}

/**
 * One field of an edit form, label on the left. A value an agent reports is locked: it shows as it is, with an Override button, and
 * only opens when you choose to. Once opened it reads as yours (an accent edge) and Reset puts the detected value back and locks it.
 */
export function FormRow({
  label,
  hint,
  adornment,
  children,
  locked,
  lockedValue,
  onOverride,
  changed,
  onReset,
}: {
  label: string
  hint?: string
  adornment?: ReactNode
  children: ReactNode
  locked?: boolean
  lockedValue?: ReactNode
  onOverride?: () => void
  /** The field is open and differs from the detected value. */
  changed?: boolean
  onReset?: () => void
}) {
  const head = (
    <span className="flex flex-wrap items-center gap-1.5 pt-2 text-sm text-nb-400">
      {label}
      {adornment}
    </span>
  )
  if (locked) {
    return (
      <div className="grid grid-cols-[8.25rem_1fr] gap-3 py-1.5" data-testid="form-row" data-state="locked">
        {head}
        <div className="flex items-center gap-2">
          <div className="flex min-w-0 flex-1 items-center gap-2 rounded-lg border border-dashed border-nb-700 px-3 py-2 text-sm text-nb-400" title="Reported by an agent">
            <Lock size={ICON_SM} aria-hidden className="shrink-0 text-nb-500" />
            <span className="min-w-0 break-words">{lockedValue === undefined || lockedValue === '' ? '—' : lockedValue}</span>
            <span className="ml-auto shrink-0 text-xs text-nb-500">Detected</span>
          </div>
          <button type="button" onClick={onOverride} aria-label={`Override ${label}`} className="shrink-0 text-xs text-accent hover:underline">
            Override
          </button>
        </div>
      </div>
    )
  }
  return (
    <div className="relative" data-testid="form-row" data-state={changed ? 'overridden' : 'open'}>
      <label className={clsx('grid grid-cols-[8.25rem_1fr] gap-3 py-1.5', onReset && 'pr-12')}>
        {head}
        <span className="block min-w-0">
          <span className={clsx('block rounded-lg', changed && 'ring-1 ring-accent/70')}>{children}</span>
          {hint && <span className="mt-1 block text-xs text-nb-500">{hint}</span>}
        </span>
      </label>
      {onReset && (
        <button type="button" onClick={onReset} aria-label={`Reset ${label}`} className="absolute right-0 top-3.5 text-xs text-accent hover:underline">
          Reset
        </button>
      )}
    </div>
  )
}
