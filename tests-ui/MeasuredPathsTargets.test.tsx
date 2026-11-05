import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import MeasuredPaths from '@/features/topology/components/MeasuredPaths'
import type { ProbeTarget } from '@/lib/history'

// The form and the list of the addresses a person asked to be measured, for an administrator of a connected server.
let settings: { probeTargets: ProbeTarget[]; measureObserved: boolean }
const save = vi.fn(async (_c: unknown, _s: unknown) => true)

vi.mock('@/store/topology', () => ({
  usePaths: () => [],
  useTopology: () => ({ agents: [{ clusterId: 'c1', measuring: 1 }], clusters: [{ id: 'c1', name: 'cloud-eu' }] }),
}))
vi.mock('@/store/server', () => ({
  useServer: (selector: (s: { status: string; role: string }) => unknown) => selector({ status: 'connected', role: 'admin' }),
  useConn: () => ({ url: '', org: 'o' }),
}))
vi.mock('@/store/settings', () => ({
  useSettings: () => ({ settings, save, error: undefined }),
}))

const target = (i: number): ProbeTarget => ({ id: `t${i}`, clusterId: 'c1', host: `203.0.113.${i}`, port: 443, label: '' })

describe('the addresses a person asked to be measured', () => {
  beforeEach(() => {
    save.mockClear()
    settings = { probeTargets: [], measureObserved: false }
  })

  test('the addresses the traffic shows are not measured unless that is asked for, and it is asked for here', () => {
    render(<MeasuredPaths />)
    const box = screen.getByTestId('measure-observed')
    expect(box).not.toBeChecked()
    fireEvent.click(box)
    expect(save).toHaveBeenCalledTimes(1)
    expect(save.mock.calls[0][1]).toMatchObject({ measureObserved: true, probeTargets: [] })
  })

  test('turning it off keeps the addresses that were asked for', () => {
    settings = { probeTargets: [target(1)], measureObserved: true }
    render(<MeasuredPaths />)
    expect(screen.getByTestId('measure-observed')).toBeChecked()
    fireEvent.click(screen.getByTestId('measure-observed'))
    expect(save.mock.calls[0][1]).toMatchObject({ measureObserved: false, probeTargets: [target(1)] })
  })

  test('shows the first ten addresses and the rest when asked for', () => {
    settings = { probeTargets: Array.from({ length: 14 }, (_, i) => target(i + 1)), measureObserved: false }
    render(<MeasuredPaths />)
    const list = () => screen.getByTestId('probe-targets').querySelectorAll('li')
    expect(list()).toHaveLength(10)
    expect(screen.getByTestId('targets-more')).toHaveTextContent('Showing 10 of 14')
    fireEvent.click(screen.getByTestId('targets-toggle'))
    expect(list()).toHaveLength(14)
  })
})
