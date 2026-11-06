import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import ConnectClusterWizard from '@/features/discovery/components/ConnectClusterWizard'
import { ApiError } from '@/lib/api'

// The wizard that connects a cluster: what the person chooses (the access level, the per-node collectors, the namespaces the agent may
// report) is sent to the server with the request for the token, and the command it shows is the one the server wrote - the page appends
// nothing to it. What the server writes from those choices is pinned in backend/internal/server/admin_token_install_test.go.

const CONN = { url: '', org: 'org-1' }
const createToken = vi.fn()
const refresh = vi.fn(async () => undefined)
const server = { conn: () => CONN, url: '', refresh, state: undefined, info: { implementedTier: 2, install: { imagesConfigured: true } }, reloadInfo: async () => undefined }
vi.mock('@/store/server', () => ({
  useServer: (selector?: (s: typeof server) => unknown) => (selector ? selector(server) : server),
}))
const topology = { clusters: [], agents: [], nodes: [], services: [], sites: [], decideDerived: vi.fn() }
vi.mock('@/store/topology', () => ({ useRawTopology: (selector: (s: typeof topology) => unknown) => selector(topology) }))
vi.mock('@/lib/places-data', () => ({ usePlaceIndex: () => undefined }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, createToken: (...a: unknown[]) => createToken(...a) } }
})

const NONE = { collectors: { nodeProbe: false, flowObserver: false, measurements: false }, scope: { namespaces: [], exclude: [], selector: '' } }
const CONTROLLER = {
  available: true,
  reason: '',
  namespace: 'ikhnos-system',
  maxTier: 2,
  install: 'helm upgrade --install ikhnos-controller oci://r.example/ikhnos-controller \\\n  --namespace ikhnos-system --create-namespace \\\n  --set maxAccessTier=2 \\\n  --set-string bootstrap.token=ikh_x',
  raise: 'helm upgrade ikhnos-controller oci://r.example/ikhnos-controller --namespace ikhnos-system --reset-then-reuse-values --set maxAccessTier=2',
  allowTelemetry: true,
}
const SERVED = 'helm install ikhnos-agent oci://r.example/ikhnos-agent \\\n  --namespace ikhnos-system \\\n  --set enrollment.key=PIN.ikh_x'

beforeEach(() => {
  createToken.mockReset()
  createToken.mockResolvedValue({ token: 'ikh_x', install: SERVED, controller: CONTROLLER, meta: { id: 't1', name: 'cluster-1', tier: 2, createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString() } })
})

const wizard = (props: { nodeProbe?: boolean } = {}) =>
  render(
    <MemoryRouter>
      <ConnectClusterWizard open onClose={() => undefined} {...props} />
    </MemoryRouter>,
  )
const create = (user: ReturnType<typeof userEvent.setup>) => user.click(screen.getByRole('button', { name: 'Create install command' }))

describe('ConnectClusterWizard: what is sent for the command', () => {
  test('with nothing chosen: the recommended access level, no collectors, no scope', async () => {
    const user = userEvent.setup()
    wizard()
    await create(user)
    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1))
    expect(createToken).toHaveBeenCalledWith(CONN, 'cluster-1', 2, NONE)
  })

  test('shows the command the server wrote, as it wrote it', async () => {
    const user = userEvent.setup()
    wizard()
    await create(user)
    expect(await screen.findByTestId('install-command')).toHaveTextContent('--set enrollment.key=PIN.ikh_x')
    expect(screen.getByTestId('install-command').textContent).toBe(SERVED)
  })

  test('every collector and the scope that were chosen', async () => {
    const user = userEvent.setup()
    wizard()
    await user.click(screen.getByTestId('node-probe-toggle'))
    await user.click(screen.getByTestId('flow-observer-toggle'))
    await user.click(screen.getByTestId('measurements-toggle'))
    await user.click(screen.getByTestId('scope-toggle'))
    await user.type(screen.getByTestId('scope-include'), 'shop{enter}pay{enter}')
    await user.type(screen.getByTestId('scope-exclude'), 'pay-test{enter}')
    await user.type(screen.getByTestId('scope-label'), 'ikhnos.io/scope=yes')
    await create(user)
    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1))
    expect(createToken).toHaveBeenCalledWith(CONN, 'cluster-1', 2, {
      collectors: { nodeProbe: true, flowObserver: true, measurements: true },
      scope: { namespaces: ['shop', 'pay'], exclude: ['pay-test'], selector: 'ikhnos.io/scope=yes' },
    })
  })

  test('at the lower access level the collectors and scope that level cannot have are not asked for, whatever was ticked before', async () => {
    const user = userEvent.setup()
    wizard()
    await user.click(screen.getByTestId('node-probe-toggle'))
    await user.click(screen.getByTestId('flow-observer-toggle'))
    await user.click(screen.getByTestId('measurements-toggle'))
    await user.click(screen.getByTestId('scope-toggle'))
    await user.type(screen.getByTestId('scope-include'), 'shop{enter}')
    await user.click(screen.getByTestId('wizard-tier-1'))
    await create(user)
    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1))
    expect(createToken).toHaveBeenCalledWith(CONN, 'cluster-1', 1, { collectors: { nodeProbe: true, flowObserver: false, measurements: false }, scope: NONE.scope })
  })

  test('the controller permissions for telemetry and the identity at uninstall are not asked about: the server\'s defaults are what everyone gets', async () => {
    const user = userEvent.setup()
    wizard()
    expect(screen.queryByTestId('controller-telemetry-toggle')).toBeNull()
    expect(screen.queryByTestId('purge-identity-toggle')).toBeNull()
    await create(user)
    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1))
    expect(createToken.mock.calls[0][3]).not.toHaveProperty('purgeIdentity')
    expect(createToken.mock.calls[0][3]).not.toHaveProperty('controllerTelemetry')
  })

  test('a node probe asked for by the page that opened this is on from the start', async () => {
    const user = userEvent.setup()
    wizard({ nodeProbe: true })
    await create(user)
    await waitFor(() => expect(createToken).toHaveBeenCalledTimes(1))
    expect(createToken.mock.calls[0][3].collectors).toEqual({ nodeProbe: true, flowObserver: false, measurements: false })
  })

  test('a namespace that is not a name asks for nothing: Create stays off and says why', async () => {
    const user = userEvent.setup()
    wizard()
    await user.click(screen.getByTestId('scope-toggle'))
    await user.type(screen.getByTestId('scope-include'), 'Not A Name{enter}')
    expect(screen.getByRole('alert')).toHaveTextContent('is not a valid namespace name')
    expect(screen.getByRole('button', { name: 'Create install command' })).toBeDisabled()
    expect(createToken).not.toHaveBeenCalled()
  })

  test('everything that can be chosen is in view at once: no fold to open, no "extras"', () => {
    wizard()
    expect(screen.queryByText(/more options/i)).toBeNull()
    expect(screen.queryByText(/extras/i)).toBeNull()
    for (const id of ['wizard-tier', 'node-probe-toggle', 'flow-observer-toggle', 'measurements-toggle', 'scope-toggle']) expect(screen.getByTestId(id)).toBeVisible()
    for (const t of ['Access', 'Collect more', 'Limit what it reports']) expect(screen.getByText(t)).toBeInTheDocument()
    for (const t of ['Node hardware and network', 'Traffic between services', 'Round-trip times', 'Only some namespaces']) expect(screen.getByText(t)).toBeInTheDocument()
  })

  test('each row says in a tag what it puts in the cluster, and the one that asks for more privileges says so', () => {
    wizard()
    expect(within(screen.getByTestId('wizard-collect')).getAllByText('pod on each node')).toHaveLength(2)
    expect(screen.getByText('runs in the agent')).toBeInTheDocument()
    expect(screen.getByText('extra privileges')).toBeInTheDocument()
  })

  test('round-trip times without the traffic observer say which addresses they time; with it, the hint goes', async () => {
    const user = userEvent.setup()
    wizard()
    expect(screen.queryByTestId('measurements-hint')).toBeNull()
    await user.click(screen.getByTestId('measurements-toggle'))
    expect(screen.getByTestId('measurements-hint')).toHaveTextContent('Times the addresses you add on the Sites page.')
    expect(screen.getByTestId('measurements-hint')).toHaveTextContent('Turn on Traffic between services')
    await user.click(screen.getByTestId('flow-observer-toggle'))
    expect(screen.queryByTestId('measurements-hint')).toBeNull()
  })

  test('at the lower access level the rows that need more are still shown, off, and say what they need', async () => {
    const user = userEvent.setup()
    wizard()
    await user.click(screen.getByTestId('flow-observer-toggle'))
    await user.click(screen.getByTestId('measurements-toggle'))
    await user.click(screen.getByTestId('wizard-tier-1'))
    for (const id of ['flow-observer-toggle', 'measurements-toggle', 'scope-toggle']) {
      expect(screen.getByTestId(id)).toBeDisabled()
      expect(screen.getByTestId(id)).not.toBeChecked()
      expect(screen.getByTestId(`${id}-needs`)).toHaveTextContent('Needs Services access')
    }
    expect(screen.queryByTestId('measurements-hint')).toBeNull()
    expect(screen.getByTestId('node-probe-toggle')).toBeEnabled()
  })

  test('the buttons stay in view while the form is long: the dialog\'s footer is sticky', () => {
    wizard()
    expect(screen.getByRole('button', { name: 'Create install command' }).parentElement).toHaveClass('sticky', '-bottom-4')
  })

  test('a refusal from the server is shown and nothing is installed from it', async () => {
    createToken.mockRejectedValue(new ApiError(400, 'the traffic observer needs access tier 2 or higher'))
    const user = userEvent.setup()
    wizard()
    await create(user)
    expect(await screen.findByText('the traffic observer needs access tier 2 or higher')).toBeInTheDocument()
    expect(screen.queryByTestId('install-command')).not.toBeInTheDocument()
  })
})

const waitingFor = (status?: string) => {
  if (!status) return
  const a = { id: 'a1', name: 'cluster-1', status, requestedAt: new Date(Date.now() + 1000).toISOString(), fingerprint: 'c0ffee12-3456', version: '0.9.3', installedTier: 2, tierCap: 2 }
  ;(server as { state: unknown }).state = { agents: [a] }
  ;(topology as { agents: unknown[] }).agents = [a]
}
afterEach(() => {
  ;(server as { state: unknown }).state = undefined
  ;(topology as { agents: unknown[] }).agents = []
})

describe('ConnectClusterWizard: once the agent has connected', () => {
  test('the install command stays open while the agent has not connected', async () => {
    const user = userEvent.setup()
    wizard()
    await create(user)
    expect(await screen.findByTestId('install-command')).toBeVisible()
    expect(screen.queryByTestId('install-collapsed')).toBeNull()
  })

  test('at the approval it folds down to one line, still there to read again, with the code card below it and the same steps above', async () => {
    waitingFor('pending')
    const user = userEvent.setup()
    wizard()
    await create(user)
    const folded = await screen.findByTestId('install-collapsed')
    expect(folded).toHaveTextContent('Install command run.')
    expect(folded).not.toHaveAttribute('open')
    expect(within(folded).getByTestId('install-command')).toHaveTextContent('--set enrollment.key=PIN.ikh_x')
    expect(screen.getByTestId('approval-code')).toBeVisible()
    expect(screen.getByTestId('wizard-steps')).toHaveTextContent('You approve it')
    // The wizard has just asked for this access, so the card does not ask again.
    expect(screen.queryByTestId('approval-tier')).toBeNull()
  })
})

describe('ConnectClusterWizard: the same install as a resource for the controller', () => {
  const made = async () => {
    const user = userEvent.setup()
    wizard()
    await create(user)
    await screen.findByTestId('install-command')
    return user
  }

  test('opens on the helm command and offers the controller beside it', async () => {
    await made()
    expect(screen.getByTestId('install-tab-helm')).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByTestId('install-tab-controller')).toHaveAttribute('aria-selected', 'false')
    expect(screen.queryByTestId('controller-steps')).not.toBeInTheDocument()
  })

  test('shows one command, as the server wrote it, with nothing to run before it', async () => {
    const user = await made()
    await user.click(screen.getByTestId('install-tab-controller'))
    const steps = await screen.findByTestId('controller-steps')
    expect(screen.queryByTestId('install-command')).not.toBeInTheDocument()
    expect(screen.getByTestId('controller-install').textContent).toBe(CONTROLLER.install)
    expect(screen.queryByTestId('controller-secret')).not.toBeInTheDocument()
    expect(screen.queryByTestId('controller-manifest')).not.toBeInTheDocument()
    expect(steps).toHaveTextContent('maxAccessTier=2')
    expect(steps).toHaveTextContent('That is all there is to do')
    expect(steps).toHaveTextContent('also given the permissions telemetry needs')
    // Nothing about a ceiling that is too low is said up front: it is said after a wait, if the agent has not shown up (see below).
    expect(screen.queryByTestId('controller-raise')).toBeNull()
    expect(steps).not.toHaveTextContent('lower ceiling')
    expect(steps).not.toHaveTextContent('<chart>')
  })

  test('goes back to the helm command', async () => {
    const user = await made()
    await user.click(screen.getByTestId('install-tab-controller'))
    await user.click(screen.getByTestId('install-tab-helm'))
    expect(screen.getByTestId('install-command').textContent).toBe(SERVED)
    expect(screen.queryByTestId('controller-steps')).not.toBeInTheDocument()
  })

  test('says why there is nothing to run when the server cannot name the controller chart', async () => {
    createToken.mockResolvedValue({
      token: 'ikh_x', install: SERVED, meta: { id: 't1', name: 'cluster-1', tier: 2, createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString() },
      controller: { available: false, reason: 'This server has no image registry configured.', namespace: 'ikhnos-system', maxTier: 2, install: '', raise: '', allowTelemetry: false },
    })
    const user = await made()
    await user.click(screen.getByTestId('install-tab-controller'))
    expect(await screen.findByTestId('controller-unavailable')).toHaveTextContent('This server has no image registry configured.')
    expect(screen.queryByTestId('controller-steps')).not.toBeInTheDocument()
    // The way that works is still there.
    await user.click(screen.getByTestId('install-tab-helm'))
    expect(screen.getByTestId('install-command').textContent).toBe(SERVED)
  })

  test('names the ceiling the controller was given', async () => {
    createToken.mockResolvedValue({
      token: 'ikh_x', install: SERVED, meta: { id: 't1', name: 'cluster-1', tier: 1, createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString() },
      controller: { ...CONTROLLER, maxTier: 1, install: CONTROLLER.install.replace('=2', '=1'), raise: CONTROLLER.raise.replace('=2', '=1') },
    })
    const user = await made()
    await user.click(screen.getByTestId('install-tab-controller'))
    expect(await screen.findByTestId('controller-steps')).toHaveTextContent('access tier 1 and no more')
    expect(screen.getByTestId('controller-install')).toHaveTextContent('--set maxAccessTier=1')
  })
})

describe('ConnectClusterWizard: when the agent is slow to show up', () => {
  const TWO_MINUTES = 120_000
  beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }))
  afterEach(() => vi.useRealTimers())
  const waitAndSee = async (via?: 'controller') => {
    const user = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
    wizard()
    await create(user)
    await screen.findByTestId('install-command')
    if (via) await user.click(screen.getByTestId('install-tab-controller'))
    return user
  }

  test('says nothing about what to check at first', async () => {
    await waitAndSee()
    act(() => void vi.advanceTimersByTime(TWO_MINUTES - 5_000))
    expect(screen.queryByTestId('still-waiting')).toBeNull()
  })

  test('after two minutes, for a helm install, says how to see the pod and its log', async () => {
    await waitAndSee()
    act(() => void vi.advanceTimersByTime(TWO_MINUTES + 1_000))
    const hint = screen.getByTestId('still-waiting')
    expect(hint).toHaveTextContent('kubectl -n ikhnos-system get pods')
    expect(hint).toHaveTextContent('kubectl -n ikhnos-system logs deploy/ikhnos-agent')
    expect(screen.queryByTestId('controller-raise')).toBeNull()
  })

  test('after two minutes, for the controller, says how to see what it made, and the server\'s command to raise its ceiling', async () => {
    await waitAndSee('controller')
    act(() => void vi.advanceTimersByTime(TWO_MINUTES + 1_000))
    const hint = screen.getByTestId('still-waiting')
    expect(hint).toHaveTextContent('kubectl -n ikhnos-system get ikhnosagent')
    expect(screen.getByTestId('controller-raise').textContent).toBe(CONTROLLER.raise)
    expect(hint).toHaveTextContent('lower access level than tier 2')
  })

  test('names the ceiling the controller was given, in the command that raises it', async () => {
    createToken.mockResolvedValue({
      token: 'ikh_x', install: SERVED, meta: { id: 't1', name: 'cluster-1', tier: 1, createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString() },
      controller: { ...CONTROLLER, maxTier: 1, install: CONTROLLER.install.replace('=2', '=1'), raise: CONTROLLER.raise.replace('=2', '=1') },
    })
    await waitAndSee('controller')
    act(() => void vi.advanceTimersByTime(TWO_MINUTES + 1_000))
    expect(screen.getByTestId('controller-raise')).toHaveTextContent('--set maxAccessTier=1')
    expect(screen.getByTestId('still-waiting')).toHaveTextContent('lower access level than tier 1')
  })

  test('no command to raise anything when the server did not offer one', async () => {
    createToken.mockResolvedValue({
      token: 'ikh_x', install: SERVED, meta: { id: 't1', name: 'cluster-1', tier: 2, createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 3_600_000).toISOString() },
      controller: { ...CONTROLLER, raise: '' },
    })
    await waitAndSee('controller')
    act(() => void vi.advanceTimersByTime(TWO_MINUTES + 1_000))
    expect(screen.getByTestId('still-waiting')).toHaveTextContent('get ikhnosagent')
    expect(screen.queryByTestId('controller-raise')).toBeNull()
  })

  test('is not shown once the agent has connected, however long it took', async () => {
    waitingFor('pending')
    await waitAndSee()
    act(() => void vi.advanceTimersByTime(TWO_MINUTES + 1_000))
    expect(screen.queryByTestId('still-waiting')).toBeNull()
  })
})
