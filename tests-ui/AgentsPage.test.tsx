import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, test, vi } from 'vitest'
import AgentsPage from '@/features/agents/AgentsPage'
import type { Agent, Cluster } from '@/lib/types'

// A page test in the same style as NamespacesPage.test.tsx: everything AgentsPage reads from the shared
// stores is faked here, and ConnectFlow is mocked directly so the connect wizard's own dependencies (a real
// server connection, ServerConnect) never have to be stood up just to check the "Set up telemetry"
// button's own visibility/enabled state next to it.
const cl = (over: Partial<Cluster> = {}) => ({ id: 'c1', name: 'edge-1', tier: 'edge', source: 'discovered', state: 'live', orgId: 'o', ...over }) as Cluster
const ag = (over: Partial<Agent> = {}) => ({ id: 'a1', clusterId: 'c1', status: 'approved', accessTier: 2, name: 'edge-1', version: '1.0.0', modules: [], fingerprint: 'x', ...over }) as Agent

let topologyState: { agents: Agent[]; clusters: Cluster[]; sites: never[]; suggestions: never[]; nodes?: never[] }
let serverState: { status: string; canEdit: () => boolean; state?: { agents: Agent[] } }

vi.mock('@/store/topology', () => ({
  useTopology: () => topologyState,
}))
vi.mock('@/store/server', () => ({
  useServer: (selector?: (s: typeof serverState) => unknown) => (selector ? selector(serverState) : serverState),
  useConn: () => ({ org: 'o' }),
}))
// The consent form and the telemetry panel have their own tests and stores; this page test is about where the page puts the Telemetry section.
vi.mock('@/features/agents/components/AgentInsight', async (importOriginal) => ({ ...(await importOriginal<typeof import('@/features/agents/components/AgentInsight')>()), AccessPanel: () => <div data-testid="access-stub" /> }))
vi.mock('@/features/pipeline/components/telemetry/TelemetryPanel', () => ({
  TelemetryPanel: ({ agentId, changeElsewhere }: { agentId?: string; changeElsewhere?: import('react').ReactNode }) => (
    <div data-testid="telemetry-stub" data-agent={agentId} data-form={changeElsewhere ? 'elsewhere' : 'form'}>{changeElsewhere}</div>
  ),
}))
const startTelemetry = vi.fn()
vi.mock('@/features/pipeline/components/telemetry/TelemetryFlow', () => ({
  useTelemetryFlow: () => ({ start: startTelemetry, dialogs: null, canStart: true }),
}))
vi.mock('@/features/discovery/components/ConnectFlow', () => ({
  useConnectFlow: () => ({ start: vi.fn(), dialogs: null, canStart: false, wizardOpen: false }),
}))

function Where() {
  const l = useLocation()
  return <output data-testid="where">{l.pathname + l.search}</output>
}

function renderPage(entry = '/agents') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <AgentsPage />
      <Where />
    </MemoryRouter>,
  )
}

describe('AgentsPage', () => {
  test('"Set up telemetry" is hidden entirely for a viewer who cannot edit', () => {
    topologyState = { agents: [ag()], clusters: [cl()], sites: [], suggestions: [] }
    serverState = { status: 'connected', canEdit: () => false }
    renderPage()
    expect(screen.queryByRole('button', { name: /Set up telemetry/ })).not.toBeInTheDocument()
  })

  test('with no approved cluster yet, the button is disabled with an explanatory title - discovery is a prerequisite', () => {
    topologyState = { agents: [], clusters: [], sites: [], suggestions: [] }
    serverState = { status: 'connected', canEdit: () => true }
    renderPage()
    const btn = screen.getByRole('button', { name: /Set up telemetry/ })
    expect(btn).toBeDisabled()
    expect(btn).toHaveAttribute('title', 'Connect a cluster first')
  })

  test('once a cluster is approved, an editor can open the telemetry flow', () => {
    topologyState = { agents: [ag()], clusters: [cl()], sites: [], suggestions: [] }
    serverState = { status: 'connected', canEdit: () => true }
    renderPage()
    const btn = screen.getByRole('button', { name: /Set up telemetry/ })
    expect(btn).not.toBeDisabled()
    expect(btn).not.toHaveAttribute('title')
  })

  test('a link from the Pipeline page (?open=<agent>) opens that agent\'s row, and is spent at once', async () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('telemetry-stub')).toHaveAttribute('data-agent', 'a1')
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/agents$/)
  })

  test('without the link, no agent is open', () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents')
    expect(screen.queryByTestId('telemetry-stub')).toBeNull()
  })

  test('an agent\'s detail settles in with a short fade and its chevron turns, instead of being swapped', () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    const row = screen.getByTestId('agent-row')
    expect(row).toHaveAttribute('aria-expanded', 'true')
    expect(row.querySelector('.chevron-turn')).toHaveAttribute('data-open', 'true')
    expect(screen.getByTestId('telemetry-stub').closest('.detail-in')).not.toBeNull()
    fireEvent.click(row)
    expect(screen.getByTestId('agent-row').querySelector('.chevron-turn')).toHaveAttribute('data-open', 'false')
    expect(screen.queryByTestId('telemetry-stub')).toBeNull()
  })

  test('the connection says how long the certificate has left, and turns amber only when it is about to run out while connected', () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1.2.3', arch: 'arm64', os: 'linux', uptimeSeconds: 7200, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [{ name: 'pods', synced: true }, { name: 'nodes', synced: false, lastError: 'forbidden' }], problems: [], installedTelemetry: [] }
    const open = (certExpiresAt: string, connected = true) => {
      topologyState = { agents: [ag({ connected, certExpiresAt })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
      serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected, certExpiresAt }), diagnostics } as unknown as Agent] } }
      return renderPage('/agents?open=a1')
    }
    const hours = (h: number) => new Date(Date.now() + h * 3_600_000).toISOString()
    const first = open(hours(30))
    const conn = screen.getByTestId('agent-connection')
    expect(conn).toHaveTextContent('v1.2.3 · linux/arm64 · up 2 h')
    expect(conn).toHaveTextContent('1 of 2 read')
    expect(conn).toHaveTextContent('nodes: forbidden')
    expect(within(conn).getByText(/left$/).className).not.toMatch(/text-warn|text-bad/)
    first.unmount()
    const soon = open(hours(1))
    expect(within(screen.getByTestId('agent-connection')).getByText(/left$/)).toHaveClass('text-warn')
    soon.unmount()
    open(hours(-1))
    expect(within(screen.getByTestId('agent-connection')).getByText(/expired/)).toHaveClass('text-bad')
  })

  test('an open agent shows what it can see and its connection; the identity, counters and modules are in a closed fold', () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('access-stub')).toBeInTheDocument()
    expect(screen.getByTestId('agent-connection')).toBeInTheDocument()
    expect((screen.getByTestId('agent-technical') as HTMLDetailsElement).open).toBe(false)
    expect((screen.getByTestId('agent-history') as HTMLDetailsElement).open).toBe(false)
    // The three tiers are not three rows any more, and the long paragraphs are tooltips.
    expect(screen.queryByText('Enrollment ceiling')).toBeNull()
    expect(screen.queryByText(/Sizes are of the messages as encoded/)).toBeNull()
    expect(screen.queryByText('Access and health')).toBeNull()
    // Inside the fold: a few lines, and only what is not already said above.
    const body = screen.getByTestId('agent-technical-body')
    expect(body).toHaveTextContent('Install')
    expect(body).not.toHaveTextContent('Heartbeats')
    expect(body).not.toHaveTextContent('Last heartbeat')
  })

  test('nothing is changed in an agent\'s row: a running one is changed from its collector, one with none is set up in the guided dialog', () => {
    const base = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics: { ...base, installedTelemetry: ['resourceUsage'] } } as unknown as Agent] } }
    const first = renderPage('/agents?open=a1')
    expect(screen.getByTestId('telemetry-stub')).toHaveAttribute('data-form', 'elsewhere')
    const link = screen.getByTestId('change-in-pipeline-a1')
    expect(link).toHaveAttribute('href', '/pipeline?open=a1')
    expect(link).toHaveTextContent(/Pipeline → .* collector/)
    expect(screen.queryByTestId('setup-telemetry-a1')).toBeNull()
    first.unmount()

    startTelemetry.mockClear()
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics: { ...base, installedTelemetry: [] } } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('telemetry-stub')).toHaveAttribute('data-form', 'elsewhere')
    expect(screen.queryByTestId('change-in-pipeline-a1')).toBeNull()
    fireEvent.click(screen.getByTestId('setup-telemetry-a1'))
    expect(startTelemetry).toHaveBeenCalledWith('a1')
  })

  test('an agent that has not reported at all is set up the same way', () => {
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }) } as unknown as Agent] } }
    startTelemetry.mockClear()
    renderPage('/agents?open=a1')
    fireEvent.click(screen.getByTestId('setup-telemetry-a1'))
    expect(startTelemetry).toHaveBeenCalledWith('a1')
  })
})
