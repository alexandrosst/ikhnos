/** A small on/off switch. A real checkbox underneath (so it is focusable, announced as a switch and has a test id), drawn as a track and a knob. */
export function Switch({ testId, checked, disabled, onChange, label }: { testId: string; checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; label: string }) {
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
        data-testid={testId}
      />
      <span className="h-5 w-9 rounded-full bg-nb-800 transition-colors peer-checked:bg-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent/60 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-nb-920" aria-hidden />
      <span className="pointer-events-none absolute left-0.5 h-4 w-4 rounded-full bg-nb-300 shadow transition-transform peer-checked:translate-x-4 peer-checked:bg-white" aria-hidden />
    </span>
  )
}
