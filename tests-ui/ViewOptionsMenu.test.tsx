import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import HopsControl from '@/features/topology/components/HopsControl'
import ViewOptionsMenu from '@/features/topology/components/ViewOptionsMenu'
import { readViewOptions } from '@/features/topology/useTopologyOptions'

function draw(query = '', over: Partial<Parameters<typeof ViewOptionsMenu>[0]> = {}) {
  const calls = { setParam: vi.fn(), onDevicesHidden: vi.fn(), onGroupChanged: vi.fn(), onToggle: vi.fn(), onClose: vi.fn() }
  const hasMesh = over.hasMesh ?? false
  render(<ViewOptionsMenu options={readViewOptions(new URLSearchParams(query), hasMesh)} hasMesh={hasMesh} hasNoise={false} open disabled={false} {...calls} {...over} />)
  return calls
}
const sw = (name: string) => screen.getByRole('switch', { name }) as HTMLInputElement

describe('ViewOptionsMenu', () => {
  test('the application view offers its switches, each writing its own parameter (on is 1, off takes the default out)', () => {
    const c = draw()
    expect(sw('Edge labels').checked).toBe(false)
    fireEvent.click(sw('Edge labels'))
    expect(c.setParam).toHaveBeenLastCalledWith('labels', '1')
    fireEvent.click(sw('Network health'))
    expect(c.setParam).toHaveBeenLastCalledWith('health', '1')
    fireEvent.click(sw('Chain layout'))
    expect(c.setParam).toHaveBeenLastCalledWith('chain', '1')
    // Devices are on by default, so switching them off is the one that writes a value.
    expect(sw('Devices and external endpoints').checked).toBe(true)
    fireEvent.click(sw('Devices and external endpoints'))
    expect(c.setParam).toHaveBeenLastCalledWith('devices', '0')
    expect(c.onDevicesHidden).toHaveBeenCalledTimes(1)
  })
  test('switching devices back on lets nothing go', () => {
    const c = draw('devices=0')
    fireEvent.click(sw('Devices and external endpoints'))
    expect(c.setParam).toHaveBeenLastCalledWith('devices', null)
    expect(c.onDevicesHidden).not.toHaveBeenCalled()
  })
  test('the infrastructure view has its own switches and none of the application ones', () => {
    draw('view=infrastructure')
    expect(sw('Services on nodes')).toBeTruthy()
    expect(sw('Cross-cluster links').checked).toBe(true)
    expect(screen.queryByRole('switch', { name: 'Chain layout' })).toBeNull()
    expect(screen.queryByRole('switch', { name: 'Service mesh' })).toBeNull()
  })
  test('the noise switch is only offered when there is noise', () => {
    draw('', { hasNoise: false })
    expect(screen.queryByRole('switch', { name: 'DNS & system traffic' })).toBeNull()
  })
  test('with noise it is offered', () => {
    draw('', { hasNoise: true })
    expect(sw('DNS & system traffic')).toBeTruthy()
  })
  test('the service mesh lens is disabled, and says why, when no cluster runs one', () => {
    draw('', { hasMesh: false })
    expect(sw('Service mesh').disabled).toBe(true)
    expect(screen.getByText('No mesh found in your clusters')).toBeTruthy()
  })
  test('with a mesh the lens can be switched', () => {
    const c = draw('', { hasMesh: true })
    expect(sw('Service mesh').disabled).toBe(false)
    fireEvent.click(sw('Service mesh'))
    expect(c.setParam).toHaveBeenLastCalledWith('mesh', '1')
  })
  test('namespace boxes and the grouping say why they are unavailable in the chain layout, and chain turns them off', () => {
    draw('chain=1')
    expect(sw('Namespace sub-boxes').disabled).toBe(true)
    expect(screen.getByText(/aren.t available while this is on/)).toBeTruthy()
    expect((screen.getByRole('combobox') as HTMLSelectElement).disabled).toBe(true)
  })
  test('grouped by tier, namespaces are unavailable with the reason beside them', () => {
    draw('group=tier')
    expect(sw('Namespace sub-boxes').disabled).toBe(true)
    expect(screen.getByText('Only available grouped by cluster')).toBeTruthy()
  })
  test('going back to the cluster grouping takes the parameter out (it is the default)', () => {
    const c = draw('group=tier')
    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(screen.getByRole('option', { name: 'Cluster' }))
    expect(c.setParam).toHaveBeenLastCalledWith('group', null)
  })
  test('changing the grouping writes it and lets the selection go', () => {
    const c = draw()
    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(screen.getByRole('option', { name: 'Tier' }))
    expect(c.setParam).toHaveBeenLastCalledWith('group', 'tier')
    expect(c.onGroupChanged).toHaveBeenCalledTimes(1)
  })
  test('the button opens and closes the menu and is disabled for an empty topology', () => {
    const c = draw('', { open: false, disabled: true })
    const b = screen.getByTestId('view-options') as HTMLButtonElement
    expect(b.disabled).toBe(true)
    expect(c.onToggle).not.toHaveBeenCalled()
  })
})

describe('HopsControl', () => {
  test('pressing a number chooses it, pressing the chosen one goes back, and the X clears', () => {
    const onChange = vi.fn()
    const { rerender } = render(<HopsControl hops={undefined} onChange={onChange} />)
    expect(screen.queryByTestId('hops-clear')).toBeNull()
    fireEvent.click(screen.getByTestId('hops-2'))
    expect(onChange).toHaveBeenLastCalledWith(2)
    rerender(<HopsControl hops={2} onChange={onChange} />)
    expect(screen.getByTestId('hops-2').getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByTestId('hops-1').getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(screen.getByTestId('hops-2'))
    expect(onChange).toHaveBeenLastCalledWith(undefined)
    fireEvent.click(screen.getByTestId('hops-3'))
    expect(onChange).toHaveBeenLastCalledWith(3)
    fireEvent.click(screen.getByTestId('hops-clear'))
    expect(onChange).toHaveBeenLastCalledWith(undefined)
  })
})
