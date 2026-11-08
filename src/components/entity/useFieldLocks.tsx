import { useState, type ReactNode } from 'react'
import { GuessDot } from '@/components/ui/primitives'
import { initiallyOpen, isDetected, isLocked } from '@/lib/fieldOrigin'
import { useWeakValue } from '@/store/rowEvidence'
import { useTopology } from '@/store/topology'

type Kind = 'cluster' | 'node' | 'service'
type Raw = Parameters<typeof initiallyOpen>[0]

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

/**
 * What an edit form needs to lock a record's reported fields. `lock(field, text)` gives a FormRow its props: locked with an Override button
 * while an agent reports the value, open once you choose to override it, with Reset to put the detected value back. `yours` are the fields
 * an agent never reports, so they are always open. A guessed value is open too, with a one-click confirm beside it.
 */
export function useFieldLocks<T extends object>({
  kind,
  id,
  raw,
  form,
  setForm,
  yours,
  by,
}: {
  kind: Kind
  id?: string
  raw: Raw
  form: T
  setForm: (update: (p: T) => T) => void
  yours: readonly string[]
  by?: string
}) {
  const weak = useWeakValue(kind)
  const confirmField = useTopology((s) => s.confirmField)
  const [opened, setOpened] = useState<Set<string>>(() => initiallyOpen(raw))
  const value = (o: unknown, field: string) => (o as Record<string, unknown> | undefined)?.[field]

  const locked = (field: string) => isLocked(raw, field, { opened, guess: weak(form as never, field)?.level === 'guess', open: yours })
  const changed = (field: string) => isDetected(raw, field) && !yours.includes(field) && !locked(field) && !same(value(form, field), value(raw, field))
  const reset = (field: string) => {
    setForm((p) => ({ ...p, [field]: value(raw, field) }))
    setOpened((p) => {
      const n = new Set(p)
      n.delete(field)
      return n
    })
  }
  const chip = (field: string): ReactNode => {
    const w = weak(form as never, field)
    if (!w) return undefined
    return (
      <span className="inline-flex items-center gap-1.5">
        <GuessDot level={w.level} why={w.why} />
        {w.level === 'guess' && id && by && (
          <button type="button" onClick={() => confirmField(kind, id, field, by)} className="text-[11px] text-accent hover:underline" title="Keep this value as it is; it will not be overwritten by rediscovery.">
            confirm
          </button>
        )}
      </span>
    )
  }
  return {
    weak,
    /** Start again from the record as stored: the fields you already overrode open, the rest locked. */
    reopen: (r: Raw) => setOpened(initiallyOpen(r)),
    /** The props for a FormRow: `text` is what the locked field shows. */
    lock: (field: string, text: ReactNode) => ({
      locked: locked(field),
      lockedValue: text,
      onOverride: () => setOpened((p) => new Set(p).add(field)),
      changed: changed(field),
      onReset: changed(field) ? () => reset(field) : undefined,
      adornment: chip(field),
    }),
    isLocked: locked,
  }
}
