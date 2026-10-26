import { render, screen, waitFor } from '@testing-library/react'
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
vi.mock('@/features/agents/components/AgentInsight', async (importOriginal) => ({ ...(await importOriginal<typeof import('@/features/agents/components/AgentInsight')>()), ConsentPanel: () => null }))
vi.mock('@/features/pipeline/components/telemetry/TelemetryPanel', () => ({
  TelemetryPanel: ({ agentId }: { agentId?: string }) => <div data-testid="telemetry-stub" data-agent={agentId} />,
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
})
