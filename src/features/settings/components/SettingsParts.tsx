import clsx from 'clsx'
import type { ReactNode } from 'react'
import { StatusPip, type StatusTone } from '@/components/ui/Status'

/**
 * The parts a settings page is made of, so every page says things the same way: a small heading, then one box of rows. A row is a name, its
 * value and, when there is something to do, one action; what a row needs to say beyond that opens beneath it (`RowMore`), instead of being
 * a paragraph above it.
 */
export function Group({ title, id, children, note }: { title: string; id?: string; children: ReactNode; note?: ReactNode }) {
  return (
    <section id={id} aria-label={title} className="scroll-mt-6" data-testid={id ? `group-${id}` : undefined}>
      <h2 className="mb-1.5 ml-0.5 text-[11px] font-medium uppercase tracking-wide text-nb-500">{title}</h2>
      <div className="divide-y divide-nb-850 overflow-hidden rounded-xl border border-nb-850 bg-nb-925">{children}</div>
      {note && <p className="mt-1.5 ml-0.5 text-xs text-nb-500">{note}</p>}
    </section>
  )
}

export function SettingRow({
  label,
  value,
  action,
  tone,
  mono,
  testId,
  valueTestId,
}: {
  label: string
  value?: ReactNode
  action?: ReactNode
  /** A dot before the value: how that setting stands. */
  tone?: StatusTone
  mono?: boolean
  testId?: string
  valueTestId?: string
}) {
  return (
    <div className="flex min-h-[3rem] flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5" data-testid={testId}>
      <span className="w-40 shrink-0 text-sm font-medium text-nb-300">{label}</span>
      <span className={clsx('flex min-w-0 flex-1 items-center gap-2 text-sm text-nb-400', mono && 'font-mono text-[13px] text-nb-300')} data-testid={valueTestId}>
        {tone && <StatusPip tone={tone} />}
        <span className="min-w-0 break-all">{value}</span>
      </span>
      {action && <span className="shrink-0">{action}</span>}
    </div>
  )
}

/** What opens beneath a row: a field, a command, a form. */
export function RowMore({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <div className="detail-in space-y-3 bg-nb-930 px-4 py-3" data-testid={testId}>
      {children}
    </div>
  )
}
