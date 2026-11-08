import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import { ApplicationForm, DeviceForm, ServiceForm, SiteForm } from '@/components/forms'
import ApplicationsPage from '@/features/topology/ApplicationsPage'
import DevicesPage from '@/features/topology/DevicesPage'
import ServicesPage from '@/features/topology/ServicesPage'
import SitesPage from '@/features/topology/SitesPage'
import { useRawTopology } from '@/store/topology'
import type { Application, Cluster, Device, MachineNode, Service, Site } from '@/lib/types'

vi.mock('@/features/discovery/components/ConnectFlow', () => ({
  useConnectFlow: () => ({ start: vi.fn(), dialogs: null, canStart: true, wizardOpen: false }),
}))

// The Services, Devices, Applications and Sites panels: read-only first with Edit and Delete, a click on the row to open, the pencil to edit.

const cluster = { id: 'c1', orgId: 'o', name: 'edge-patras', tier: 'edge', source: 'discovered', status: 'healthy', labels: {}, lastSeen: new Date().toISOString() } as unknown as Cluster
const node = { id: 'n1', orgId: 'o', name: 'worker-1', clusterId: 'c1', role: 'worker', kind: 'vm', ip: '', os: '', cpu: 1, memoryGb: 1, status: 'healthy', labels: {}, source: 'discovered' } as MachineNode
const svc = (over: Partial<Service> = {}) =>
  ({ id: 's1', orgId: 'o', name: 'cart', namespace: 'shop', clusterId: 'c1', kind: 'Deployment', image: 'ghcr.io/x/cart:1', replicas: 2, nodeIds: ['n1'], status: 'healthy', labels: {}, source: 'discovered', lastSeen: new Date().toISOString(), ...over }) as Service
const dev = (over: Partial<Device> = {}) => ({ id: 'd1', orgId: 'o', name: 'Temperature sensors', kind: 'sensor', count: 12, protocol: 'MQTT', connectivity: 'lora', status: 'unknown', labels: {}, source: 'manual', ...over }) as Device
const app = (over: Partial<Application> = {}) => ({ id: 'a1', orgId: 'o', name: 'Shop', description: 'The storefront', origin: 'explicit', confidence: 'high', source: 'manual', ...over }) as Application
const site = (over: Partial<Site> = {}) => ({ id: 'si1', orgId: 'o', name: 'Patras', kind: 'data-center', lat: 38.25, lng: 21.74, country: 'GR', city: 'Patras', ...over }) as Site

const seed = () =>
  useRawTopology.setState({ clusters: [cluster], nodes: [node], services: [svc()], devices: [dev()], applications: [app()], sites: [site()], dependencies: [] })
const page = (ui: React.ReactElement) => render(<MemoryRouter>{ui}</MemoryRouter>)
const panel = () => within(screen.getByRole('dialog'))

beforeEach(() => {
  localStorage.clear()
  seed()
})

describe('ServiceForm panel', () => {
  test('opens read-only; the cluster shows by name and the image has a lock', () => {
    render(<ServiceForm initial={useRawTopology.getState().services[0]} onClose={() => {}} />)
    expect(screen.getByRole('heading', { name: 'cart' })).toBeInTheDocument()
    const cl = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('Cluster'))!
    expect(within(cl).getByText('edge-patras')).toBeInTheDocument()
    const image = screen.getAllByTestId('panel-row').find((r) => within(r).queryByText('Image'))!
    expect(within(image).getByLabelText('Detected')).toBeInTheDocument()
  })

  test('the form locks the reported image; sensitivity and labels are always open; the save keeps only the override', async () => {
    const user = userEvent.setup()
    render(<ServiceForm initial={useRawTopology.getState().services[0]} onClose={() => {}} edit />)
    expect(screen.queryByRole('textbox', { name: /image/i })).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: /data sensitivity/i })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Override Image' }))
    const input = screen.getByRole('textbox', { name: /image/i })
    await user.clear(input)
    await user.type(input, 'ghcr.io/x/cart:2')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(useRawTopology.getState().services[0].overrides).toEqual({ image: 'ghcr.io/x/cart:2' })
  })

  test('the node picker is a group of buttons: pressing one toggles it', async () => {
    const user = userEvent.setup()
    useRawTopology.setState({ services: [svc({ source: 'manual', nodeIds: [] })] })
    render(<ServiceForm initial={useRawTopology.getState().services[0]} onClose={() => {}} edit />)
    const w1 = screen.getByRole('button', { name: 'worker-1' })
    expect(w1).toHaveAttribute('aria-pressed', 'false')
    await user.click(w1)
    expect(w1).toHaveAttribute('aria-pressed', 'true')
  })

  test('the page: the name cell says where it comes from, a click opens, the pencil edits', async () => {
    const user = userEvent.setup()
    page(<ServicesPage />)
    expect(screen.getByText(/^Detected/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'cart' }))
    expect(panel().queryByRole('textbox')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Close' }))
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByRole('button', { name: 'Override Image' })).toBeInTheDocument()
  })
})

describe('DeviceForm panel', () => {
  test('a declared device has no marks and nothing locked', async () => {
    const user = userEvent.setup()
    render(<DeviceForm initial={useRawTopology.getState().devices[0]} onClose={() => {}} />)
    expect(screen.getByText(/^Declared by you/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Detected')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /edit/i }))
    expect(screen.queryByRole('button', { name: /^Override/ })).not.toBeInTheDocument()
    const units = screen.getByRole('spinbutton', { name: /units/i })
    await user.clear(units)
    await user.type(units, '20')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(useRawTopology.getState().devices[0].count).toBe(20)
    // Back in the read-only view.
    expect(screen.getByText('20')).toBeInTheDocument()
  })

  test('the page opens a device on a click, and Delete in the panel asks to confirm', async () => {
    const user = userEvent.setup()
    page(<DevicesPage />)
    await user.click(screen.getByRole('button', { name: 'Temperature sensors' }))
    await user.click(panel().getByRole('button', { name: 'Delete' }))
    expect(screen.getByText('Delete Temperature sensors?')).toBeInTheDocument()
  })
})

describe('ApplicationForm panel', () => {
  test('lists the services that belong to it; Edit changes the description', async () => {
    const user = userEvent.setup()
    useRawTopology.setState({ services: [svc({ applicationId: 'a1' })] })
    render(<ApplicationForm initial={useRawTopology.getState().applications[0]} onClose={() => {}} />)
    expect(screen.getByText('cart')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /edit/i }))
    const d = screen.getByRole('textbox', { name: /description/i })
    await user.clear(d)
    await user.type(d, 'Checkout and cart')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(useRawTopology.getState().applications[0].description).toBe('Checkout and cart')
  })

  test('the page shows the description in its own column and opens the panel on a click', async () => {
    const user = userEvent.setup()
    page(<ApplicationsPage />)
    expect(screen.getByText('The storefront')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Shop' }))
    expect(panel().getByRole('button', { name: /edit/i })).toBeInTheDocument()
  })
})

describe('SiteForm panel', () => {
  test('shows the place and coordinates, lists its clusters, and Edit opens the form', async () => {
    const user = userEvent.setup()
    useRawTopology.setState({ clusters: [{ ...cluster, siteId: 'si1' }] })
    render(<SiteForm initial={useRawTopology.getState().sites[0]} onClose={() => {}} />)
    expect(screen.getByText('38.2500, 21.7400')).toBeInTheDocument()
    expect(screen.getByText('edge-patras')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /edit/i }))
    expect(screen.getByRole('textbox', { name: /^name/i })).toHaveValue('Patras')
  })

  test('the page opens a site on a click', async () => {
    const user = userEvent.setup()
    page(<SitesPage />)
    await user.click(screen.getByRole('button', { name: 'Patras' }))
    expect(panel().getByRole('button', { name: /edit/i })).toBeInTheDocument()
  })
})
