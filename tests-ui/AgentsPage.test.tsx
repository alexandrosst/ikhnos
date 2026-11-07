import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, test, vi } from 'vitest'
import AgentsPage from '@/features/agents/AgentsPage'
import type { Agent, Cluster } from '@/lib/types'

// A page test in the same style as NamespacesPage.test.tsx: everything AgentsPage reads from the shared
// stores is faked here, and ConnectFlow is mocked directly so the connect wizard's own dependencies (a real
// server connection, ServerConnect) never have to be stood up just to check the page.
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
// The access panel has its own tests and store; this page test is about the page around it.
vi.mock('@/features/agents/components/AgentInsight', async (importOriginal) => ({ ...(await importOriginal<typeof import('@/features/agents/components/AgentInsight')>()), AccessPanel: () => <div data-testid="access-stub" /> }))
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
  test('the page\'s header has only "Connect a cluster": telemetry is set up on the Pipeline page', () => {
    topologyState = { agents: [ag()], clusters: [cl()], sites: [], suggestions: [] }
    serverState = { status: 'connected', canEdit: () => true }
    renderPage()
    expect(screen.queryByRole('button', { name: /Set up telemetry/ })).not.toBeInTheDocument()
  })

  test('a link from the Pipeline page (?open=<agent>) opens that agent\'s row, and is spent at once', async () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('open-in-pipeline-a1')).toBeInTheDocument()
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/agents$/)
  })

  test('without the link, no agent is open', () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents')
    expect(screen.queryByTestId('agent-telemetry')).toBeNull()
  })

  test('an agent\'s detail settles in with a short fade and its chevron turns, instead of being swapped', () => {
    const diagnostics = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: ['resourceUsage'] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    const row = screen.getByTestId('agent-row')
    expect(row).toHaveAttribute('aria-expanded', 'true')
    expect(row.querySelector('.chevron-turn')).toHaveAttribute('data-open', 'true')
    expect(screen.getByTestId('agent-telemetry').closest('.detail-in')).not.toBeNull()
    fireEvent.click(row)
    expect(screen.getByTestId('agent-row').querySelector('.chevron-turn')).toHaveAttribute('data-open', 'false')
    expect(screen.queryByTestId('agent-telemetry')).toBeNull()
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

  test('an agent\'s telemetry is a pointer to Pipeline: a running one says how many signals and opens its collector, one with none is set up there', () => {
    const base = { reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [] }
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics: { ...base, installedTelemetry: ['resourceUsage', 'kubernetesState'] } } as unknown as Agent] } }
    const first = renderPage('/agents?open=a1')
    expect(screen.getByTestId('agent-telemetry-count')).toHaveTextContent('2 signals')
    expect(screen.getByTestId('open-in-pipeline-a1')).toHaveAttribute('href', '/pipeline?open=a1')
    expect(screen.queryByTestId('setup-in-pipeline-a1')).toBeNull()
    first.unmount()

    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }), diagnostics: { ...base, installedTelemetry: [] } } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('agent-telemetry')).toHaveTextContent('Nothing is sent from this cluster yet.')
    expect(screen.queryByTestId('open-in-pipeline-a1')).toBeNull()
    expect(screen.getByTestId('setup-in-pipeline-a1')).toHaveAttribute('href', '/pipeline?setup=a1')
  })

  test('an agent that has not reported at all is not said to send nothing, and can still be set up', () => {
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => true, state: { agents: [{ ...ag({ connected: true }) } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('agent-telemetry')).toHaveTextContent('Not known until the agent has reported.')
    expect(screen.getByTestId('setup-in-pipeline-a1')).toHaveAttribute('href', '/pipeline?setup=a1')
  })

  test('someone who cannot change things is shown the state but not sent to set it up', () => {
    topologyState = { agents: [ag({ connected: true })], clusters: [cl()], sites: [], suggestions: [], nodes: [] }
    serverState = { status: 'connected', canEdit: () => false, state: { agents: [{ ...ag({ connected: true }) } as unknown as Agent] } }
    renderPage('/agents?open=a1')
    expect(screen.getByTestId('agent-telemetry')).toBeInTheDocument()
    expect(screen.queryByTestId('setup-in-pipeline-a1')).toBeNull()
  })
})
