import clsx from 'clsx'

/**
 * The one on/off switch. A real checkbox underneath (so it is focusable, announced as a switch and has a test id), drawn as a track and a knob.
 * `size` "sm" is for a switch inside a menu row, `tone` "ok" for one that means something is running rather than merely chosen.
 * Wrap it in a <label> with the words to make the words a click target too.
 */
export function Switch({ testId, checked, disabled, onChange, label, describedBy, size = 'md', tone = 'accent' }: {
  testId?: string
  checked: boolean
  disabled?: boolean
  onChange: (v: boolean) => void
  label: string
  describedBy?: string
  size?: 'sm' | 'md'
  tone?: 'accent' | 'ok'
}) {
  const sm = size === 'sm'
  return (
    <span className="relative inline-flex shrink-0 items-center">
      <input
        type="checkbox"
        role="switch"
        className="peer absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        aria-label={label}
        aria-describedby={describedBy}
        data-testid={testId}
      />
      <span
        className={clsx(
          'rounded-full bg-nb-800 transition-colors peer-focus-visible:ring-2 peer-focus-visible:ring-accent/60 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-nb-920 peer-disabled:opacity-60',
          sm ? 'h-4 w-7' : 'h-5 w-9',
          tone === 'ok' ? 'peer-checked:bg-ok' : 'peer-checked:bg-accent',
        )}
        aria-hidden
      />
      <span
        className={clsx(
          'pointer-events-none absolute left-0.5 rounded-full bg-nb-300 shadow transition-transform peer-checked:bg-white',
          sm ? 'size-3 peer-checked:translate-x-3' : 'size-4 peer-checked:translate-x-4',
        )}
        aria-hidden
      />
    </span>
  )
}
