import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, test, vi } from 'vitest'
import NodesPage from '@/features/topology/NodesPage'
import type { Cluster, MachineNode } from '@/lib/types'

const cl = { id: 'c1', name: 'edge-1', source: 'discovered', state: 'live', orgId: 'o' } as Cluster
const node = (id: string, kind: MachineNode['kind']) =>
  ({ id, name: id, clusterId: 'c1', kind, role: 'worker', arch: 'amd64', source: 'discovered', state: 'live', status: 'online', orgId: 'o', ip: '10.0.0.1' }) as unknown as MachineNode

let nodes: MachineNode[] = []

vi.mock('@/store/topology', () => ({
  useTopology: () => ({ clusters: [cl], nodes, services: [], deleteNode: vi.fn() }),
}))
vi.mock('@/store/server', () => ({
  useServer: Object.assign((selector: (s: { status: string; conn: () => null }) => unknown) => selector({ status: 'connected', conn: () => null }), {
    getState: () => ({ conn: () => null }),
  }),
}))
vi.mock('@/features/discovery/components/ConnectFlow', () => ({
  useConnectFlow: () => ({ start: vi.fn(), dialogs: null, canStart: true, wizardOpen: false }),
}))

describe('NodesPage', () => {
  test('a node that is a virtual machine says so in full, not "VM"', () => {
    nodes = [node('n-vm', 'vm'), node('n-bm', 'bare-metal'), node('n-edge', 'edge-device')]
    render(
      <MemoryRouter>
        <NodesPage />
      </MemoryRouter>,
    )
    expect(screen.getByText('Virtual machine')).toBeInTheDocument()
    expect(screen.getByText('Bare metal')).toBeInTheDocument()
    expect(screen.getByText('Edge device')).toBeInTheDocument()
    expect(screen.queryByText('VM')).toBeNull()
  })
})
