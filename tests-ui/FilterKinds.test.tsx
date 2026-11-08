import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import FilterMenu from '@/features/topology/components/FilterMenu'
import { DEFAULT_KINDS, type Filter } from '@/features/topology/lib/filter'

const draw = (filter: Filter, onChange = vi.fn()) => {
  render(<FilterMenu open onOpenChange={() => {}} filter={filter} clusters={[]} applications={[]} onChange={onChange} />)
  return onChange
}
const box = (id: string) => screen.getByTestId(`filter-kinds-${id}`) as HTMLInputElement
const none: Filter = { clusters: [], apps: [], kinds: [] }

describe('the workload kinds in the filter', () => {
  test('start on the services: DaemonSets and Jobs are off, and ticking one adds it', () => {
    const onChange = draw({ ...none, kinds: DEFAULT_KINDS })
    expect([box('services').checked, box('DaemonSet').checked, box('Job').checked]).toEqual([true, false, false])
    fireEvent.click(box('Job'))
    expect(onChange).toHaveBeenCalledWith({ ...none, kinds: ['Deployment', 'StatefulSet', 'Job'] })
  })

  test('with no kinds chosen everything is shown; ticking all four again goes back to none chosen', () => {
    const onChange = draw(none)
    expect([box('services').checked, box('DaemonSet').checked, box('Job').checked]).toEqual([true, true, true])
    fireEvent.click(box('Job'))
    expect(onChange).toHaveBeenCalledWith({ ...none, kinds: ['Deployment', 'StatefulSet', 'DaemonSet'] })
  })

  test('the last thing shown cannot be switched off', () => {
    const onChange = draw({ ...none, kinds: ['Job'] })
    fireEvent.click(box('Job'))
    expect(onChange).not.toHaveBeenCalled()
  })

  test('adding the last missing kind clears the choice instead of listing all four', () => {
    const onChange = draw({ ...none, kinds: ['Deployment', 'StatefulSet', 'DaemonSet'] })
    fireEvent.click(box('Job'))
    expect(onChange).toHaveBeenCalledWith({ ...none, kinds: [] })
  })
})
