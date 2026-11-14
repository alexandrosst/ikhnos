import { act, render, renderHook, screen } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { Modal, StatusDot } from '@/components/ui/primitives'
import { EXIT_MS, leaveAsGhost, motionWanted, usePresence } from '@/lib/motion'

/** A browser that does want motion: jsdom has no matchMedia at all, which `motionWanted` reads as "no". */
function wantMotion(reduced = false) {
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: reduced && q.includes('reduce'), media: q, addEventListener() {}, removeEventListener() {} }))
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('motionWanted', () => {
  test('is off where nothing can say it is wanted, and when the person asked for less', () => {
    expect(motionWanted()).toBe(false)
    wantMotion(true)
    expect(motionWanted()).toBe(false)
    wantMotion(false)
    expect(motionWanted()).toBe(true)
  })
})

describe('usePresence', () => {
  test('without motion it is mounted exactly while open', () => {
    const { result, rerender } = renderHook(({ open }) => usePresence(open), { initialProps: { open: true } })
    expect(result.current).toEqual({ mounted: true, closing: false })
    rerender({ open: false })
    expect(result.current).toEqual({ mounted: false, closing: false })
  })

  test('with motion it stays mounted, closing, for EXIT_MS after it is closed, then goes', () => {
    wantMotion()
    const { result, rerender } = renderHook(({ open }) => usePresence(open), { initialProps: { open: true } })
    rerender({ open: false })
    expect(result.current).toEqual({ mounted: true, closing: true })
    act(() => void vi.advanceTimersByTime(EXIT_MS + 5))
    expect(result.current).toEqual({ mounted: false, closing: false })
  })

  test('opening again while it is leaving cancels the exit', () => {
    wantMotion()
    const { result, rerender } = renderHook(({ open }) => usePresence(open), { initialProps: { open: true } })
    rerender({ open: false })
    rerender({ open: true })
    act(() => void vi.advanceTimersByTime(EXIT_MS * 3))
    expect(result.current).toEqual({ mounted: true, closing: false })
  })
})

describe('a closed dialog leaves as a copy', () => {
  test('with motion, a fading, inert copy stays for a moment; the dialog itself is gone at once', () => {
    wantMotion()
    const { rerender } = render(<Modal open onClose={() => {}} title="Hello">body</Modal>)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    rerender(<Modal open={false} onClose={() => {}} title="Hello">body</Modal>)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    const ghost = document.body.querySelector('.fade-out')
    expect(ghost).not.toBeNull()
    expect(ghost).toHaveAttribute('aria-hidden', 'true')
    expect(ghost).toHaveAttribute('inert')
    expect(ghost?.textContent).toContain('body')
    expect(ghost?.querySelector('.modal-out')).not.toBeNull()
    act(() => void vi.advanceTimersByTime(EXIT_MS + 60))
    expect(document.body.querySelector('.fade-out')).toBeNull()
  })

  test('without motion nothing is left behind', () => {
    const { rerender } = render(<Modal open onClose={() => {}} title="Hello">body</Modal>)
    rerender(<Modal open={false} onClose={() => {}} title="Hello">body</Modal>)
    expect(document.body.querySelector('.fade-out')).toBeNull()
  })

  test('a copy carries no ids or test ids, so a query never finds the leaving one', () => {
    wantMotion()
    const el = document.createElement('div')
    el.className = 'fade-in'
    el.innerHTML = '<div role="dialog" class="modal-pop"><span id="x" data-testid="t">hi</span></div>'
    document.body.appendChild(el)
    leaveAsGhost(el)
    const ghost = document.body.querySelector('.fade-out') as HTMLElement
    expect(ghost.querySelector('[id], [data-testid], [role="dialog"]')).toBeNull()
  })
})

describe('status dots', () => {
  test('a down dot breathes and a healthy one stays still', () => {
    const { container, rerender } = render(<StatusDot status="offline" />)
    expect(container.querySelector('.pip-down')).not.toBeNull()
    rerender(<StatusDot status="healthy" />)
    expect(container.querySelector('.pip-down')).toBeNull()
    expect(container.querySelector('.pip')).not.toBeNull()
  })
})

describe('the shared movements run on the shared timings', () => {
  const css = readFileSync('src/index.css', 'utf8')
  const rule = (sel: string) => css.split('\n').find((l) => l.startsWith(`${sel} {`)) ?? ''
  test.each(['.menu-pop', '.modal-pop', '.fade-in', '.wizard-step-in', '.wizard-step-in-back', '.menu-out', '.modal-out', '.drawer-out', '.fade-out', '.row-exit', '.meter-bar', '.pip', '.drawer-in', '.detail-in', '.tab-panel'])(
    '%s takes its duration from a --dur- token, not a number',
    (sel) => {
      const line = rule(sel)
      expect(line, `${sel} is not defined`).not.toBe('')
      expect(line).toContain('var(--dur-')
      expect(line).not.toMatch(/\b\d+(\.\d+)?m?s\b/)
    },
  )
})
