import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import { AccessPanel } from '@/features/agents/components/AgentInsight'
import type { AgentDiagnostics } from '@/lib/consent'
import type { Agent } from '@/lib/types'

// One panel for what an agent may read: the level, the collectors with their switches, the namespaces left out. An editor changes it; anyone else only reads it.

const setAgentTier = vi.fn(async () => undefined)
const setAgentConsent = vi.fn(async () => undefined)
const serverState = {
  info: { implementedTier: 2, install: { chartRef: 'oci://registry.example.com/team/ikhnos-agent', chartVersion: '0.9.0' } },
  conn: () => ({ url: '', org: 'o' }),
  refresh: async () => undefined,
  boostPolling: async () => undefined,
  reloadInfo: async () => undefined,
}
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, setAgentTier: (...a: unknown[]) => (setAgentTier as (...x: unknown[]) => Promise<undefined>)(...a), setAgentConsent: (...a: unknown[]) => (setAgentConsent as (...x: unknown[]) => Promise<undefined>)(...a), agentUpgradeCommand: async (...a: Parameters<typeof import('./upgradeServer').answer>) => (await import('./upgradeServer')).answer(...a) } }
})
vi.mock('@/store/server', () => ({
  useServer: (selector?: (s: typeof serverState) => unknown) => (selector ? selector(serverState) : serverState),
  useConn: () => ({ url: '', org: 'o' }),
}))

type AgentCollector = AgentDiagnostics['collectors'][number]
const agent = (over: Partial<Agent> = {}) => ({ id: 'a1', clusterId: 'c1', status: 'approved', accessTier: 2, installedTier: 2, name: 'edge-1', ...over }) as Agent
const collector = (name: string, over: Partial<AgentCollector> = {}) => ({ name, configured: true, enabled: true, producing: true, expected: 0, reporting: 0, pausedByServer: false, ...over }) as AgentCollector
const diag = (collectors: AgentCollector[], over: Partial<AgentDiagnostics> = {}) =>
  ({ reportedAt: new Date().toISOString(), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors, informers: [], problems: [], scope: { description: '', namespaces: 5, inScope: 5 }, ...over }) as AgentDiagnostics

beforeEach(() => {
  setAgentTier.mockClear()
  setAgentConsent.mockClear()
})

describe('AccessPanel', () => {
  test('a collector in the cluster has a row and a switch; the ones that are not installed are one quiet line with none', () => {
    render(<AccessPanel editable agent={agent()} diagnostics={diag([collector('probes')])} />)
    expect(within(screen.getByTestId('collector-probes')).getByRole('switch', { name: 'Node probe on' })).toBeChecked()
    expect(screen.queryByTestId('collector-flow')).toBeNull()
    expect(screen.getByTestId('collectors-missing')).toHaveTextContent('Not installed: traffic observer, path measurements')
    expect(screen.getAllByRole('switch')).toHaveLength(1)
  })

  test('switching a collector off and saving pauses just that one', async () => {
    render(<AccessPanel editable agent={agent()} diagnostics={diag([collector('probes'), collector('flow')])} />)
    expect(screen.getByTestId('consent-save')).toBeDisabled()
    fireEvent.click(screen.getByTestId('collector-switch-flow'))
    expect(screen.getByTestId('collector-switch-flow')).not.toBeChecked()
    fireEvent.click(screen.getByTestId('consent-save'))
    await waitFor(() => expect(setAgentConsent).toHaveBeenCalledWith(expect.anything(), 'a1', { pausedCollectors: ['flow'], excludedNamespaces: [] }))
    expect(setAgentTier).not.toHaveBeenCalled()
  })

  test('choosing a lower level and saving changes only the level', async () => {
    render(<AccessPanel editable agent={agent()} diagnostics={diag([])} />)
    fireEvent.click(screen.getByTestId('tier-option-1'))
    fireEvent.click(screen.getByTestId('consent-save'))
    await waitFor(() => expect(setAgentTier).toHaveBeenCalledWith(expect.anything(), 'a1', 1))
    expect(setAgentConsent).not.toHaveBeenCalled()
  })

  test('a level above what the install allows is asked about, not selected: the owner\'s command is shown', async () => {
    render(<AccessPanel editable agent={agent({ accessTier: 1, installedTier: 1 })} diagnostics={diag([], { installedTier: 1, effectiveTier: 1, approvedTier: 1 })} />)
    expect(screen.getByTestId('widen-hint')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('tier-option-2'))
    expect(screen.getByTestId('consent-save')).toBeDisabled()
    expect(await screen.findByTestId('helm-command')).toBeInTheDocument()
  })

  test('without the right to change it, the same panel only says what is so', () => {
    render(<AccessPanel editable={false} agent={agent()} diagnostics={diag([collector('probes')])} />)
    expect(screen.getByTestId('scope-words')).toHaveTextContent('5 of 5 namespaces in view')
    expect(screen.getByTestId('collector-probes')).toBeInTheDocument()
    expect(screen.queryByRole('switch')).toBeNull()
    expect(screen.queryByTestId('consent-save')).toBeNull()
    expect(screen.queryByTestId('exclude-input')).toBeNull()
    expect(screen.getByTestId('ceiling-line')).toHaveTextContent('The install allows up to services')
  })

  test('the level the agent really collects is drawn, and a lower one than approved is said so', () => {
    render(<AccessPanel editable={false} agent={agent()} diagnostics={diag([], { effectiveTier: 1 })} />)
    expect(screen.getByTestId('tier-option-2')).toHaveTextContent('Approved up to services')
    expect(screen.getByTestId('tier-note')).toBeInTheDocument()
  })
})
