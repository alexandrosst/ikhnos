import clsx from 'clsx'
import { CheckCircle2, Clock, Loader2, Undo2, XCircle, type LucideIcon } from 'lucide-react'
import { ICON_SM } from '@/components/ui/primitives'
import type { TelemetryRequestDoc } from '@/lib/generated/wire'
import { requestLine, type LineTone } from '@/lib/telemetryRequest'

const LOOK: Record<LineTone, { box: string; text: string }> = {
  info: { box: 'border-info/30 bg-info/10', text: 'text-info' },
  ok: { box: 'border-ok/30 bg-ok/10', text: 'text-ok' },
  bad: { box: 'border-bad/30 bg-bad/10', text: 'text-bad' },
  warn: { box: 'border-warn/30 bg-warn/10', text: 'text-warn' },
}

/** One glyph per state, so the line never rests on colour alone. */
function glyph(doc: TelemetryRequestDoc): { Icon: LucideIcon; spin?: boolean } {
  switch (doc.state) {
    case 'written':
      return { Icon: Loader2, spin: true }
    case 'applied':
      return { Icon: CheckCircle2 }
    case 'refused':
      return { Icon: XCircle }
    case 'reverted':
      return { Icon: Undo2 }
    default:
      return { Icon: Clock }
  }
}

/**
 * What became of the latest request to collect more or less, in the cluster's own words: sent, being applied, in force, taken back,
 * refused, or put back because the collectors did not come up. Nothing is shown for a cluster that was never asked.
 */
export function TelemetryRequestLine({ request, testId = 'telemetry-request' }: { request: TelemetryRequestDoc | undefined; testId?: string }) {
  const line = requestLine(request)
  if (!request || !line) return null
  const look = LOOK[line.tone]
  const { Icon, spin } = glyph(request)
  return (
    <div role="status" className={clsx('flex items-start gap-3 rounded-xl border px-4 py-3', look.box)} data-testid={testId} data-state={request.state} data-requested={request.requested}>
      <Icon size={ICON_SM + 4} className={clsx('mt-0.5 shrink-0', look.text, spin && 'animate-spin motion-reduce:animate-none')} aria-hidden />
      <div className="min-w-0 space-y-0.5">
        <div className={clsx('text-sm font-medium', look.text)} data-testid={`${testId}-title`}>{line.title}</div>
        <p className="max-w-prose text-sm text-nb-300" data-testid={`${testId}-body`}>{line.body}</p>
        <p className="text-xs text-nb-500" data-testid={`${testId}-meta`}>{line.meta}</p>
      </div>
    </div>
  )
}
