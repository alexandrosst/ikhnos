import type { ReactNode } from 'react'
import type { ReviewNotes } from '@/features/pipeline/lib/telemetrySetup'
import Disclosure from './Disclosure'

/** One block of the review: a bold lead and short sentences. */
function Block({ title, items, testId }: { title: string; items: string[]; testId: string }) {
  return (
    <section data-testid={testId}>
      <h4 className="text-xs font-medium uppercase tracking-wide text-nb-500">{title}</h4>
      <ul className="mt-1 space-y-1 text-sm text-nb-300">
        {items.map((t) => (
          <li key={t} className="flex gap-2"><span className="mt-2 size-1 shrink-0 rounded-full bg-nb-600" aria-hidden />{t}</li>
        ))}
      </ul>
    </section>
  )
}

/**
 * What the command does, in plain words, one click below the summary: what changes in the cluster, what does not, and how to stop. It opens by
 * itself when an install already has telemetry, since then it says what is being added and what removed. `children` are what stops the
 * command from being made (a destination that cannot receive), each with its way forward.
 */
export default function ReviewStep({ notes, cluster, open, children, testIdPrefix }: { notes: ReviewNotes; cluster: string; open: boolean; children?: ReactNode; testIdPrefix: string }) {
  return (
    <div className="space-y-3" data-testid={`${testIdPrefix}-guided-step-review`}>
      <Disclosure title="What this changes" defaultOpen={open} testId={`${testIdPrefix}-review`}>
        <div className="space-y-3">
          <Block title={`Changes in ${cluster}`} items={notes.changes} testId={`${testIdPrefix}-review-changes`} />
          <Block title="Does not change" items={notes.unchanged} testId={`${testIdPrefix}-review-unchanged`} />
          <Block title="If you stop" items={notes.stop} testId={`${testIdPrefix}-review-stop`} />
        </div>
      </Disclosure>
      {children}
    </div>
  )
}
