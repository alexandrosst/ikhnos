import { useCallback, useState } from 'react'

/**
 * Which dialog a page has open: none, or one, with what it is about. A page that kept one `useState` per dialog could have two open at
 * once without meaning to, and every new dialog meant another state, another setter to thread through and another branch to render. One
 * value says it cannot happen: opening a dialog replaces whichever was open, and `close` leaves none.
 *
 * `D` is a union of the page's dialogs, each tagged with a `kind` and carrying what it needs (`{ kind: 'health'; operator }`).
 */
export function useDialog<D extends { kind: string }>(): { dialog: D | null; open: (d: D) => void; close: () => void } {
  const [dialog, setDialog] = useState<D | null>(null)
  const open = useCallback((d: D) => setDialog(d), [])
  const close = useCallback(() => setDialog(null), [])
  return { dialog, open, close }
}
