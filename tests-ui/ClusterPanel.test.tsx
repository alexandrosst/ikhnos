import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import { ClusterForm } from '@/components/forms'
import { useRawTopology } from '@/store/topology'
import type { Cluster } from '@/lib/types'

// The cluster panel: read-only first, every fact marked with where it comes from; Edit turns it into a form in which a value an agent
// reports stays locked until you choose Override.

const found = (over: Partial<Cluster> = {}) =>
  ({
    id: 'c1',
    orgId: 'o',
    name: 'edge-patras',
    tier: 'edge',
    distribution: 'k3s',
    version: 'v1.30.2',
    provider: 'On-prem',
    region: 'patras',
    status: 'healthy',
    labels: { env: 'prod' },
    source: 'discovered',
    lastSeen: new Date().toISOString(),
    ...over,
  }) as Cluster

const seed = (c: Cluster) => useRawTopology.setState({ clusters: [c], nodes: [], services: [], dependencies: [], sites: [] })
const stored = () => useRawTopology.getState().clusters[0]

beforeEach(() => seed(found()))

describe('ClusterForm panel', () => {
  test('opens read-only: facts with a lock where an agent reports them, and an Edit button', () => {
    render(<ClusterForm initial={stored()} onClose={() => {}} />)
    expect(screen.getByRole('heading', { name: 'edge-patras' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    const version = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('Version'))!
    expect(within(version).getByText('v1.30.2')).toBeInTheDocument()
    expect(within(version).getByLabelText('Detected')).toBeInTheDocument()
  })

  test('a value you set shows a pencil, and a field nobody filled is a dash', () => {
    seed(found({ overrides: { region: 'greece' }, overrideMeta: { region: { by: 'alex', at: '2026-01-01T00:00:00Z' } } }))
    render(<ClusterForm initial={stored()} onClose={() => {}} />)
    const region = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('Region label'))!
    expect(within(region).getByText('greece')).toBeInTheDocument()
    expect(within(region).getByLabelText('Set by you')).toBeInTheDocument()
    const ingress = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('Ingress'))!
    expect(within(ingress).getByText('—')).toBeInTheDocument()
  })

  test('says what is not known yet about a cluster that was discovered', () => {
    render(<ClusterForm initial={stored()} onClose={() => {}} />)
    expect(screen.getByText(/Not known yet:/)).toBeInTheDocument()
  })

  test('Edit opens the form with every reported field locked; Delete is offered only when the caller can delete', async () => {
    const user = userEvent.setup()
    const onDelete = vi.fn()
    render(<ClusterForm initial={stored()} onClose={() => {}} onDelete={onDelete} />)
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onDelete).toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /edit/i }))
    expect(screen.getByRole('button', { name: 'Override Version' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: /version/i })).not.toBeInTheDocument()
    // Fields that are yours by nature are open from the start.
    expect(screen.getByRole('textbox', { name: /data residency/i })).toBeInTheDocument()
  })

  test('Override opens a locked field; saving stores only that field as an override', async () => {
    const user = userEvent.setup()
    render(<ClusterForm initial={stored()} onClose={() => {}} edit />)
    await user.click(screen.getByRole('button', { name: 'Override Version' }))
    const input = screen.getByRole('textbox', { name: /version/i })
    await user.clear(input)
    await user.type(input, 'v1.31.0')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(stored().version).toBe('v1.30.2')
    expect(stored().overrides).toEqual({ version: 'v1.31.0' })
    // Back in the read-only view, showing the new value.
    expect(screen.getByText('v1.31.0')).toBeInTheDocument()
  })

  test('Reset puts the detected value back and locks the field again', async () => {
    const user = userEvent.setup()
    render(<ClusterForm initial={stored()} onClose={() => {}} edit />)
    await user.click(screen.getByRole('button', { name: 'Override Version' }))
    const input = screen.getByRole('textbox', { name: /version/i })
    await user.clear(input)
    await user.type(input, 'v9')
    await user.click(screen.getByRole('button', { name: 'Reset Version' }))
    expect(screen.getByRole('button', { name: 'Override Version' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(stored().overrides).toBeUndefined()
  })

  test('a field you already overrode starts open, and Reset to detected values clears them all', async () => {
    const user = userEvent.setup()
    seed(found({ overrides: { region: 'greece' } }))
    render(<ClusterForm initial={stored()} onClose={() => {}} edit />)
    expect(screen.getByRole('textbox', { name: /region label/i })).toHaveValue('greece')
    await user.click(screen.getByRole('button', { name: 'Reset to detected values' }))
    expect(stored().overrides).toBeUndefined()
  })

  test('a new cluster is a plain form with nothing locked', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<ClusterForm initial={null} onClose={onClose} />)
    expect(screen.queryByRole('button', { name: /^Override/ })).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: /^name/i }), 'lab-1')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    expect(useRawTopology.getState().clusters.some((c) => c.name === 'lab-1')).toBe(true)
    expect(onClose).toHaveBeenCalled()
  })
})
