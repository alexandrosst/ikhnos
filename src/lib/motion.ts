import { useEffect, useState } from 'react'

/** How long a leaving popover, dialog or drawer plays its exit. The same 120 ms as `--dur-fast` in index.css, where the `*-out` classes use it. */
export const EXIT_MS = 120

/** Whether movement is wanted at all: not when the person asked for less of it, and not where there is no way to ask (a test, a server render). */
export function motionWanted(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return !window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * Keeps something mounted while its exit plays. `mounted` says whether to render it; `closing` says it is on its way out, so the caller puts
 * its `*-out` class on it. With no motion wanted it is mounted exactly while `open`.
 */
export function usePresence(open: boolean, ms = EXIT_MS): { mounted: boolean; closing: boolean } {
  const [mounted, setMounted] = useState(open)
  // Opening takes effect at once, in the same render (adjusting state while rendering rather than in an effect).
  if (open && !mounted) setMounted(true)
  useEffect(() => {
    if (open || !mounted) return
    const t = setTimeout(() => setMounted(false), ms)
    return () => clearTimeout(t)
  }, [open, mounted, ms])
  if (!motionWanted()) return { mounted: open, closing: false }
  return { mounted: open || mounted, closing: !open && mounted }
}

/**
 * Plays the exit of something that is already gone from React: a copy of its element, laid over the page where it was, fades out and is removed.
 * For a component whose callers close it by simply no longer rendering it (a dialog), where keeping it mounted would mean changing every caller.
 * The copy is inert and hidden from assistive technology, and never receives a click.
 */
export function leaveAsGhost(el: HTMLElement) {
  if (!motionWanted() || !document.body) return
  const ghost = el.cloneNode(true) as HTMLElement
  ghost.setAttribute('aria-hidden', 'true')
  ghost.setAttribute('inert', '')
  ghost.removeAttribute('data-testid')
  ghost.querySelectorAll('[id], [data-testid]').forEach((n) => {
    n.removeAttribute('id')
    n.removeAttribute('data-testid')
  })
  ghost.classList.remove('fade-in')
  ghost.classList.add('fade-out')
  const box = ghost.querySelector('[role="dialog"]')
  if (box) {
    box.removeAttribute('role')
    const drawer = box.classList.contains('drawer-in')
    box.classList.remove('drawer-in', 'modal-pop')
    box.classList.add(drawer ? 'drawer-out' : 'modal-out')
  }
  document.body.appendChild(ghost)
  setTimeout(() => ghost.remove(), EXIT_MS + 40)
}
