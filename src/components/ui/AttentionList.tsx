import clsx from 'clsx'
import type { ReactNode } from 'react'
import { Card } from './primitives'

/** One thing that needs a person: how bad it is (the chip), what it is about, what is wrong, and the one thing to do about it. */
export interface AttentionItem {
  id: string
  /** The status, as the glyph and word of whichever chip the page already uses. */
  chip: ReactNode
  /** What it is about: a component's name, a problem's headline. */
  title: ReactNode
  /** What is wrong, in a sentence. */
  detail: ReactNode
  /** The one control for it (a button or a link). */
  action?: ReactNode
  /** In `cards`, the colour of the card's edge. */
  tint?: 'bad' | 'warn'
  testId?: string
  attrs?: Record<`data-${string}`, string>
}

/**
 * "What needs attention": the same list wherever a page has one, so a problem always reads chip, subject, sentence, action.
 * `rows` is a compact single list (a long one is cut at `max`, saying how many more there are); `cards` gives each problem its own
 * card, tinted by how bad it is, for a short list where every item deserves room.
 */
export function AttentionList({ items, variant = 'rows', max, more }: { items: AttentionItem[]; variant?: 'rows' | 'cards'; max?: number; /** What to say about the items cut at `max`. */ more?: (hidden: number) => ReactNode }) {
  const shown = max === undefined ? items : items.slice(0, max)
  const hidden = items.length - shown.length
  const list =
    variant === 'cards' ? (
      <ul className="space-y-3">
        {shown.map((i) => (
          <li key={i.id} className={clsx('rounded-xl border px-5 py-4', i.tint === 'bad' ? 'border-bad/30 bg-bad/5' : 'border-warn/30 bg-warn/5')} data-testid={i.testId} {...i.attrs}>
            <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2 text-sm font-medium text-nb-300">
                  {i.chip} {i.title}
                </div>
                <p className="mt-1.5 max-w-2xl text-sm text-nb-500">{i.detail}</p>
              </div>
              {i.action}
            </div>
          </li>
        ))}
      </ul>
    ) : (
      <Card as="ul" className="divide-y divide-nb-850">
        {shown.map((i) => (
          <li key={i.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3" data-testid={i.testId} {...i.attrs}>
            {i.chip}
            <p className="min-w-0 flex-1 basis-60 text-sm text-nb-400"><span className="font-medium text-nb-300">{i.title}.</span> {i.detail}</p>
            {i.action}
          </li>
        ))}
      </Card>
    )
  return (
    <>
      {list}
      {hidden > 0 && more && <p className="mt-3 text-xs text-nb-500">{more(hidden)}</p>}
    </>
  )
}
