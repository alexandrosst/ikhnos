import { fireEvent, render, renderHook, screen, act } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, test, vi } from 'vitest'
import { AttentionList, type AttentionItem } from '@/components/ui/AttentionList'
import { FilterChips } from '@/components/ui/FilterChips'
import { useQueryParam, useQueryParams, withParams } from '@/lib/useQueryParams'
import type { ReactNode } from 'react'

describe('withParams', () => {
  test('sets strings, removes null, undefined and false, leaves the rest, and does not touch the original', () => {
    const before = new URLSearchParams('a=1&b=2&c=3&d=4')
    const after = withParams(before, { a: 'x', b: null, c: undefined, d: false, e: 'new' })
    expect(after.toString()).toBe('a=x&e=new')
    expect(before.toString()).toBe('a=1&b=2&c=3&d=4')
    expect(withParams(new URLSearchParams('k=1&z=9'), { k: 'a b' }).toString()).toBe('k=a+b&z=9')
  })
})

function Where() {
  const l = useLocation()
  return <p data-testid="where">{l.pathname + l.search}</p>
}
const wrap = (url: string) => ({ children }: { children: ReactNode }) => (
  <MemoryRouter initialEntries={[url]}>{children}<Where /></MemoryRouter>
)

describe('useQueryParams', () => {
  test('changes several parameters in one navigation that replaces the history entry', () => {
    const { result } = renderHook(() => useQueryParams(), { wrapper: wrap('/p?tab=seen&view=map&keep=1') })
    act(() => result.current[1]({ tab: null, view: null, x: 'y' }))
    expect(result.current[0].toString()).toBe('keep=1&x=y')
  })
  test('takes a function of what is there now', () => {
    const { result } = renderHook(() => useQueryParams(), { wrapper: wrap('/p?kind=local&via=central') })
    act(() => result.current[1]((p) => (p.get('via') === 'central' ? { kind: null, via: null } : { kind: 'local', via: 'central' })))
    expect(result.current[0].toString()).toBe('')
    act(() => result.current[1]((p) => (p.get('via') === 'central' ? { kind: null, via: null } : { kind: 'local', via: 'central' })))
    expect(result.current[0].toString()).toBe('kind=local&via=central')
  })
  test('one parameter: its value, and an empty string removes it', () => {
    const { result } = renderHook(() => useQueryParam('namespace'), { wrapper: wrap('/p?namespace=kube-system&cluster=c1') })
    expect(result.current[0]).toBe('kube-system')
    act(() => result.current[1](''))
    expect(result.current[0]).toBeNull()
  })
})

describe('FilterChips', () => {
  test('marks the chosen one, shows counts, reports a press, and shows what follows the chips', () => {
    const onChange = vi.fn()
    render(
      <FilterChips label="Show agents" value="ok" onChange={onChange} choices={[{ key: 'all', label: 'All', count: 5, testId: 'f-all' }, { key: 'ok', label: 'Connected', count: 3 }]}>
        <span>extra</span>
      </FilterChips>,
    )
    expect(screen.getByRole('group', { name: 'Show agents' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Connected/ }).getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByTestId('f-all').getAttribute('aria-pressed')).toBe('false')
    expect(screen.getByTestId('f-all').textContent).toBe('All 5')
    fireEvent.click(screen.getByTestId('f-all'))
    expect(onChange).toHaveBeenCalledWith('all')
    expect(screen.getByText('extra')).toBeTruthy()
  })
})

describe('AttentionList', () => {
  const items: AttentionItem[] = ['a', 'b', 'c', 'd'].map((id) => ({ id, chip: <b>chip-{id}</b>, title: `Name ${id}`, detail: `Why ${id}`, action: <button>do {id}</button>, tint: id === 'a' ? 'bad' : 'warn', testId: `item-${id}`, attrs: { 'data-health': id } }))
  test('rows: chip, subject, sentence and action for each, cut at max with a note on the rest', () => {
    render(<AttentionList items={items} max={3} more={(n) => `and ${n} more`} />)
    expect(screen.getByTestId('item-a').textContent).toContain('chip-a')
    expect(screen.getByTestId('item-a').textContent).toContain('Name a. Why a')
    expect(screen.getByTestId('item-a').getAttribute('data-health')).toBe('a')
    expect(screen.getByRole('button', { name: 'do c' })).toBeTruthy()
    expect(screen.queryByTestId('item-d')).toBeNull()
    expect(screen.getByText('and 1 more')).toBeTruthy()
  })
  test('cards: tinted by how bad, and no note when nothing is cut', () => {
    render(<AttentionList items={items.slice(0, 2)} variant="cards" max={3} more={(n) => `and ${n} more`} />)
    expect(screen.getByTestId('item-a').className).toContain('border-bad/30')
    expect(screen.getByTestId('item-b').className).toContain('border-warn/30')
    expect(screen.queryByText(/more/)).toBeNull()
  })
})
