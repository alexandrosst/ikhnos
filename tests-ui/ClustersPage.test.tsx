import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import ClustersPage from '@/features/topology/ClustersPage'
import { useRawTopology } from '@/store/topology'
import type { Cluster } from '@/lib/types'

vi.mock('@/features/discovery/components/ConnectFlow', () => ({
  useConnectFlow: () => ({ start: vi.fn(), dialogs: null, canStart: true, wizardOpen: false }),
}))

const cl = (over: Partial<Cluster> = {}) =>
  ({ id: 'c1', orgId: 'o', name: 'edge-patras', tier: 'edge', distribution: 'k3s', version: 'v1.30', provider: '', region: 'patras', status: 'healthy', labels: {}, source: 'discovered', lastSeen: new Date().toISOString(), ...over }) as Cluster

function renderPage() {
  return render(
    <MemoryRouter>
      <ClustersPage />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  localStorage.clear()
  useRawTopology.setState({ clusters: [cl(), cl({ id: 'c2', name: 'lab', source: 'manual', lastSeen: undefined })], nodes: [], services: [], dependencies: [], sites: [] })
})

describe('Clusters page', () => {
  test('the name cell says where each record comes from, one wording for every row', () => {
    renderPage()
    const rows = screen.getAllByRole('row').slice(1)
    expect(within(rows[0]).getByText(/^Detected/)).toBeInTheDocument()
    expect(within(rows[1]).getByText(/^Declared by you/)).toBeInTheDocument()
  })

  test('a click on the row or the name opens the record read-only; the pencil opens it straight in edit', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.click(screen.getByRole('button', { name: 'edge-patras' }))
    expect(screen.getByRole('heading', { name: 'edge-patras' })).toBeInTheDocument()
    expect(within(screen.getByRole('dialog')).queryByRole('textbox')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Close' }))

    await user.click(screen.getAllByRole('button', { name: 'Edit' })[0])
    expect(screen.getByRole('heading', { name: 'Edit edge-patras' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Override Version' })).toBeInTheDocument()
  })

  test('Delete in the panel hands over to the confirmation', async () => {
    const user = userEvent.setup()
    renderPage()
    await user.click(screen.getByRole('button', { name: 'lab' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(screen.getByText('Delete lab?')).toBeInTheDocument()
  })

  test('the Discovered column is off by default and can be turned on', async () => {
    const user = userEvent.setup()
    renderPage()
    expect(screen.queryByRole('columnheader', { name: 'Discovered' })).not.toBeInTheDocument()
    await user.click(screen.getByTestId('columns-button'))
    await user.click(screen.getByTestId('column-discovered'))
    expect(screen.getByRole('columnheader', { name: 'Discovered' })).toBeInTheDocument()
  })
})
