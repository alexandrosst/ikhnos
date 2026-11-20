import { act, renderHook } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { useDialog } from '@/lib/useDialog'

type D = { kind: 'a' } | { kind: 'b'; id: string }

describe('useDialog', () => {
  test('none to begin with; opening one replaces whichever was open; close leaves none', () => {
    const { result } = renderHook(() => useDialog<D>())
    expect(result.current.dialog).toBeNull()
    act(() => result.current.open({ kind: 'a' }))
    expect(result.current.dialog).toEqual({ kind: 'a' })
    act(() => result.current.open({ kind: 'b', id: '1' }))
    expect(result.current.dialog).toEqual({ kind: 'b', id: '1' })
    act(() => result.current.close())
    expect(result.current.dialog).toBeNull()
  })
  test('open and close keep their identity, so they are safe in effect dependencies', () => {
    const { result, rerender } = renderHook(() => useDialog<D>())
    const { open, close } = result.current
    act(() => result.current.open({ kind: 'a' }))
    rerender()
    expect(result.current.open).toBe(open)
    expect(result.current.close).toBe(close)
  })
})
