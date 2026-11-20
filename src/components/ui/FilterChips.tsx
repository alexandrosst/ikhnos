import clsx from 'clsx'
import type { ReactNode } from 'react'

export interface FilterChoice<K extends string> {
  key: K
  label: ReactNode
  /** How many things this choice would show, so a choice that shows nothing is not a mystery. */
  count?: number
  testId?: string
}

/**
 * A row of pills that narrows a list to one kind ("All 12 · Healthy 9 · Late 2 …"): exactly one is on, pressing another moves it. Anything
 * given as children (a chip that clears a second filter) sits at the end of the same row.
 */
export function FilterChips<K extends string>({ label, value, onChange, choices, children, className }: { /** Names the group for a screen reader, e.g. "Show agents". */ label: string; value: K; onChange: (key: K) => void; choices: FilterChoice<K>[]; children?: ReactNode; className?: string }) {
  return (
    <div className={clsx('flex flex-wrap items-center gap-1.5', className)} role="group" aria-label={label}>
      {choices.map((c) => (
        <button
          key={c.key}
          type="button"
          onClick={() => onChange(c.key)}
          aria-pressed={value === c.key}
          className={clsx('rounded-full border px-3 py-1 text-xs transition-colors', value === c.key ? 'border-accent/40 bg-accent-soft text-accent' : 'border-nb-850 text-nb-400 hover:border-nb-800 hover:text-nb-300')}
          data-testid={c.testId}
        >
          {c.label}
          {c.count !== undefined && <> <span className="text-nb-500">{c.count}</span></>}
        </button>
      ))}
      {children}
    </div>
  )
}
