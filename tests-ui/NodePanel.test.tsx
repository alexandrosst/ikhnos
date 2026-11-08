import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import { NodeForm } from '@/components/forms'
import NodesPage from '@/features/topology/NodesPage'
import { useRawTopology } from '@/store/topology'
import type { Cluster, MachineNode } from '@/lib/types'

vi.mock('@/features/discovery/components/ConnectFlow', () => ({
  useConnectFlow: () => ({ start: vi.fn(), dialogs: null, canStart: true, wizardOpen: false }),
}))

const cluster = { id: 'c1', orgId: 'o', name: 'edge-patras', tier: 'edge', source: 'discovered', status: 'healthy', labels: {}, lastSeen: new Date().toISOString() } as unknown as Cluster
const found = (over: Partial<MachineNode> = {}) =>
  ({ id: 'n1', orgId: 'o', name: 'worker-1', clusterId: 'c1', role: 'worker', kind: 'vm', ip: '10.0.0.7', os: 'Ubuntu 22.04', cpu: 4, memoryGb: 16, status: 'healthy', labels: {}, arch: 'amd64', source: 'discovered', lastSeen: new Date().toISOString(), ...over }) as MachineNode

const seed = (n: MachineNode) => useRawTopology.setState({ clusters: [cluster], nodes: [n], services: [], dependencies: [], sites: [] })
const stored = () => useRawTopology.getState().nodes[0]

beforeEach(() => {
  localStorage.clear()
  seed(found())
})

describe('NodeForm panel', () => {
  test('opens read-only with a lock beside what an agent reports, and its cluster by name', () => {
    render(<NodeForm initial={stored()} onClose={() => {}} />)
    expect(screen.getByRole('heading', { name: 'worker-1' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    const ip = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('IP address'))!
    expect(within(ip).getByText('10.0.0.7')).toBeInTheDocument()
    expect(within(ip).getByLabelText('Detected')).toBeInTheDocument()
    const cl = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('Cluster'))!
    expect(within(cl).getByText('edge-patras')).toBeInTheDocument()
  })

  test('active conditions are a warning under the name', () => {
    seed(found({ conditions: ['MemoryPressure'] }))
    render(<NodeForm initial={stored()} onClose={() => {}} />)
    expect(screen.getByText('MemoryPressure')).toBeInTheDocument()
  })

  test('the form locks the reported IP; Override opens it and the save keeps only that override', async () => {
    const user = userEvent.setup()
    render(<NodeForm initial={stored()} onClose={() => {}} edit />)
    expect(screen.queryByRole('textbox', { name: /ip address/i })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Override IP address' }))
    const input = screen.getByRole('textbox', { name: /ip address/i })
    await user.clear(input)
    await user.type(input, '10.0.0.9')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(stored().ip).toBe('10.0.0.7')
    expect(stored().overrides).toEqual({ ip: '10.0.0.9' })
  })

  test('a new node is a plain form with nothing locked', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<NodeForm initial={null} onClose={onClose} />)
    expect(screen.queryByRole('button', { name: /^Override/ })).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: /^name/i }), 'lab-node')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    expect(useRawTopology.getState().nodes.some((n) => n.name === 'lab-node')).toBe(true)
    expect(onClose).toHaveBeenCalled()
  })
})

describe('Nodes page', () => {
  const page = () =>
    render(
      <MemoryRouter>
        <NodesPage />
      </MemoryRouter>,
    )

  test('the name cell says where the record comes from, and status has its own column', () => {
    page()
    expect(screen.getByText(/^Detected/)).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Status' })).toBeInTheDocument()
  })

  test('a click opens the node read-only; the pencil opens it in edit; Delete in the panel asks to confirm', async () => {
    const user = userEvent.setup()
    page()
    await user.click(screen.getByRole('button', { name: 'worker-1' }))
    expect(within(screen.getByRole('dialog')).queryByRole('textbox')).not.toBeInTheDocument()
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(screen.getByText('Delete worker-1?')).toBeInTheDocument()
  })

  test('the pencil in the row opens the form straight away', async () => {
    const user = userEvent.setup()
    page()
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByRole('button', { name: 'Override IP address' })).toBeInTheDocument()
  })
})
