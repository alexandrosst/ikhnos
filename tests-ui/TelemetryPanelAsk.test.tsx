import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import { TelemetryPanel } from '@/features/pipeline/components/telemetry/TelemetryPanel'
import type { AgentDiagnostics } from '@/lib/consent'
import type { TelemetryRequestDoc } from '@/lib/generated/wire'
import { DEFAULT_SETTINGS } from '@/lib/history'
import { forgetUpgrades } from './upgradeServer'

// Asking the cluster: the third way to make a change, next to helm and the patch of the resource. What is held here is when it is offered
// (a controller keeps the agent, and the agent can pass a request on), what is sent (the signals and the share of traces, nothing else),
// that whatever else the form changes is said to be left out, who may send it, and what the line under the form says as the answer moves.

let role = 'admin'
let agent: Record<string, unknown> = {}
vi.mock('@/store/settings', () => ({
  useSettings: () => ({ settings: DEFAULT_SETTINGS, loaded: true, error: undefined, save: vi.fn(async () => true) }),
}))
const CONN = { url: 'https://example.test', org: 'org-1' }
const connFn = () => CONN
const reloadInfo = vi.fn(async () => undefined)
const store = () => ({ role, conn: connFn, reloadInfo, state: { agents: [{ id: 'a1', ...agent }] } })
vi.mock('@/store/server', () => ({
  useServer: (selector?: (s: ReturnType<typeof store>) => unknown) => (selector ? selector(store()) : store()),
  useConn: () => CONN,
}))
const setAgentTelemetry = vi.fn()
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      listOperatorDestinations: async () => [],
      listTelemetryIntents: async () => [],
      agentUpgradeCommand: async (...a: Parameters<typeof import('./upgradeServer').answer>) => (await import('./upgradeServer')).answer(...a),
      setAgentTelemetry: (...a: unknown[]) => setAgentTelemetry(...a),
    },
  }
})

const diag = (over: Partial<AgentDiagnostics> = {}): AgentDiagnostics =>
  ({ reportedAt: new Date().toISOString(), agentVersion: 'x', uptimeSeconds: 1, installedTier: 1, approvedTier: 1, effectiveTier: 1, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], ...over }) as AgentDiagnostics

// Resource usage and events are collected now; traces are not. The destination is the owner's.
const INSTALLED = diag({
  installedTelemetry: ['resourceUsage', 'kubernetesEvents'],
  installedTelemetryConfig: { exportEndpoint: 'gw.example.com:4317', redactionEnabled: true, resourceDetectionEnabled: false },
})

const doc = (over: Partial<TelemetryRequestDoc> = {}): TelemetryRequestDoc => ({ revision: 4, requested: true, state: 'waiting', ...over })
const ALL = ['resourceUsage', 'energy', 'kubernetesState', 'nodeRuntime', 'networkLatency', 'clusterLinks', 'applicationMetrics', 'systemLogs', 'kubernetesEvents', 'applicationLogs', 'traces', 'accelerators']
/** A request states every signal, as the server keeps it. */
const sigs = (on: string[]) => Object.fromEntries(ALL.map((k) => [k, on.includes(k)])) as unknown as TelemetryRequestDoc['signals']
const KEPT = { keptByController: true, capabilities: ['telemetry-relay'] }

const tree = () => (
  <MemoryRouter>
    <TelemetryPanel standalone testIdPrefix="tp" agentId="a1" clusterId="cl-1" diagnostics={INSTALLED} />
  </MemoryRouter>
)

async function toRun(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByTestId('tp-guided-continue'))
  await user.click(screen.getByTestId('tp-guided-continue'))
  await user.click(screen.getByTestId('tp-guided-create-command'))
}

beforeEach(() => {
  forgetUpgrades()
  role = 'admin'
  agent = { ...KEPT }
  setAgentTelemetry.mockReset()
  setAgentTelemetry.mockImplementation(async (_c: unknown, _id: string, body: { signals?: Record<string, boolean>; tracesSamplingPercent?: number }) => doc({ revision: 5, requested: !!body.signals, signals: body.signals as TelemetryRequestDoc['signals'], tracesSamplingPercent: body.tracesSamplingPercent }))
})

describe('TelemetryPanel: asking the cluster', () => {
  test('is the first way offered when a controller keeps the agent and the agent can pass a request on, and sends the signals and the share', async () => {
    const user = userEvent.setup()
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await toRun(user)
    expect(screen.getByTestId('tp-via-ask')).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByTestId('tp-credential-hint')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('tp-ask-send'))
    await waitFor(() => expect(setAgentTelemetry).toHaveBeenCalledTimes(1))
    const [, id, body] = setAgentTelemetry.mock.calls[0] as [unknown, string, { signals: Record<string, boolean>; tracesSamplingPercent?: number }]
    expect(id).toBe('a1')
    // Every signal is stated, the three that are on are on, and nothing about where the data goes is in it.
    expect(Object.keys(body.signals)).toHaveLength(12)
    expect(Object.entries(body.signals).filter(([, on]) => on).map(([k]) => k).sort()).toEqual(['kubernetesEvents', 'resourceUsage', 'traces'])
    expect(body.tracesSamplingPercent).toBe(100)
    expect(Object.keys(body).sort()).toEqual(['signals', 'tracesSamplingPercent'])
    // The answer moves the line under the form at once, without waiting for the next poll.
    expect(await screen.findByTestId('tp-request')).toHaveAttribute('data-state', 'waiting')
    expect(screen.getByTestId('tp-request-title')).toHaveTextContent('Sent to the agent')
    expect(screen.getByTestId('tp-request-meta')).toHaveTextContent('Revision 5')
  })

  test('taking the request back sends nothing to collect, and is offered only while a request is in force', async () => {
    const user = userEvent.setup()
    agent = { ...KEPT, telemetry: doc({ state: 'applied', signals: sigs(['resourceUsage', 'kubernetesEvents']) }) }
    render(tree())
    await toRun(user)
    await user.click(screen.getByTestId('tp-ask-take-back'))
    await waitFor(() => expect(setAgentTelemetry).toHaveBeenCalledWith(expect.anything(), 'a1', {}))
    // The server answers a new revision that asks for nothing; the cluster has not said anything about it yet.
    expect(await screen.findByTestId('tp-request-title')).toHaveTextContent('Taking it back')
    expect(screen.queryByTestId('tp-ask-take-back')).not.toBeInTheDocument()
  })

  test('is not offered when nothing keeps the agent installed, and the commands stay', async () => {
    const user = userEvent.setup()
    agent = {}
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await toRun(user)
    expect(screen.queryByTestId('tp-via-ask')).not.toBeInTheDocument()
    expect(screen.getByTestId('tp-via-helm')).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByTestId('helm-command')).toBeInTheDocument()
    expect(screen.getByTestId('tp-ask-none')).toHaveTextContent('when a controller keeps this agent installed')
  })

  test('says an agent from before the relay is too old, and keeps the commands', async () => {
    const user = userEvent.setup()
    agent = { keptByController: true, capabilities: [] }
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await toRun(user)
    // Offered, but it explains instead of sending: the controller is there, the agent cannot pass a request to it.
    await user.click(screen.getByTestId('tp-via-ask'))
    expect(screen.getByTestId('tp-ask-old')).toHaveTextContent('too old')
    expect(screen.queryByTestId('tp-ask-send')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('tp-via-helm'))
    expect(await screen.findByTestId('helm-command')).toBeInTheDocument()
  })

  test('lists what the form changes that a request does not carry, so it is not taken for sent', async () => {
    const user = userEvent.setup()
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await user.click(screen.getByTestId('tp-guided-continue'))
    // Where to send: a different destination from the owner's.
    await user.click(screen.getByTestId('tp-guided-destination-external-preset-honeycomb'))
    await user.click(screen.getByTestId('tp-guided-continue'))
    await user.click(screen.getByTestId('tp-guided-create-command'))
    const left = await screen.findByTestId('tp-ask-left-out')
    expect(left).toHaveTextContent('Not part of a request')
    expect(left).toHaveTextContent('Sends to')
    // The signal change itself is not in that list, and sending is still possible.
    expect(left).not.toHaveTextContent('Turns on')
    expect(screen.getByTestId('tp-ask-send')).toBeEnabled()
  })

  test('an editor can ask for less but not for more', async () => {
    const user = userEvent.setup()
    role = 'editor'
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await toRun(user)
    expect(screen.getByTestId('tp-ask-blocked')).toHaveTextContent('takes an administrator')
    expect(screen.getByTestId('tp-ask-send')).toBeDisabled()
  })

  test('is disabled for what the cluster was already asked, and when there is nothing to change', async () => {
    const user = userEvent.setup()
    agent = {
      ...KEPT,
      telemetry: doc({ state: 'refused', reason: 'no', signals: sigs(['resourceUsage', 'kubernetesEvents', 'traces']), tracesSamplingPercent: 100 }),
    }
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await toRun(user)
    expect(screen.getByTestId('tp-ask-blocked')).toHaveTextContent('already asked')
    expect(screen.getByTestId('tp-ask-send')).toBeDisabled()
  })

  test('says what the server said when it refuses', async () => {
    const user = userEvent.setup()
    setAgentTelemetry.mockRejectedValueOnce(new Error('asking a cluster to collect more takes an administrator'))
    render(tree())
    await user.click(screen.getByTestId('tp-traces'))
    await toRun(user)
    await user.click(screen.getByTestId('tp-ask-send'))
    expect(await screen.findByTestId('tp-ask-error')).toHaveTextContent('takes an administrator')
  })
})

describe('TelemetryPanel: the line under the form', () => {
  test.each([
    ['written', true, 'The controller is applying it'],
    ['applied', true, 'In force'],
    ['applied', false, 'Taken back'],
    ['waiting', true, 'Sent to the agent'],
  ])('%s (requested %s) says %s', (state, requested, title) => {
    agent = { ...KEPT, telemetry: doc({ state, requested }) }
    render(tree())
    expect(screen.getByTestId('tp-request-title')).toHaveTextContent(title)
  })

  test('a refusal and a put-back carry the cluster’s own reason', () => {
    agent = { ...KEPT, telemetry: doc({ state: 'refused', reason: 'The cluster’s controller was not given the permissions telemetry needs (allowTelemetry in its chart).' }) }
    const { unmount } = render(tree())
    expect(screen.getByTestId('tp-request-title')).toHaveTextContent('did not take it')
    expect(screen.getByTestId('tp-request-body')).toHaveTextContent('allowTelemetry in its chart')
    unmount()
    agent = { ...KEPT, telemetry: doc({ state: 'reverted', reason: 'ikhnos-telemetry-host: 2 of 3 pods updated and ready' }) }
    render(tree())
    expect(screen.getByTestId('tp-request-title')).toHaveTextContent('Put back')
    expect(screen.getByTestId('tp-request-body')).toHaveTextContent('2 of 3 pods')
    expect(screen.getByTestId('tp-request-meta')).toHaveTextContent('change the request to try again')
  })

  test('a cluster that was never asked has no line', () => {
    render(tree())
    expect(screen.queryByTestId('tp-request')).not.toBeInTheDocument()
  })
})
