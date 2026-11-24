import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { StrictMode } from 'react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import { APP_GONE_HINT, APPLICATIONS_HINT, creatorGoneHint, expiryText, FusionAccess, fusionDocsUrl, fusionStatusCurl, selfSignedLikely, tokenApplications, previewMembers, membersSummary } from '@/features/fusion/components/FusionAccess'
import { isReloadHeld } from '@/lib/staleBuild'
import type { CreatedFusionAccessToken, FusionAccessToken, FusionApplication, FusionApplications } from '@/lib/api'

const listFusionTokens = vi.fn()
const createFusionToken = vi.fn()
const revokeFusionToken = vi.fn()
const getFusionApplications = vi.fn()
// conn must be the same function on every render, as the real store's is: the list is read again whenever it changes.
const conn = () => ({ url: 'https://ikhnos.example', org: 'o' })
vi.mock('@/store/server', () => ({ useServer: (sel: (s: { conn: typeof conn }) => unknown) => sel({ conn }) }))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      listFusionTokens: (...a: unknown[]) => listFusionTokens(...a),
      createFusionToken: (...a: unknown[]) => createFusionToken(...a),
      revokeFusionToken: (...a: unknown[]) => revokeFusionToken(...a),
      getFusionApplications: (...a: unknown[]) => getFusionApplications(...a),
    },
  }
})

const day = 86_400_000
const token = (over: Partial<FusionAccessToken> = {}): FusionAccessToken => ({
  id: 'fk-1', name: 'Decision engine', signals: ['metrics', 'logs', 'traces'], namespaces: [], clusters: [], applications: [], applicationMembers: [], createdBy: 'alex', createdById: 'u-alex', creator: 'admin',
  createdAt: new Date(Date.now() - 2 * day).toISOString(), expiresAt: new Date(Date.now() + 29.5 * day).toISOString(), ...over,
})

const app = (id: string, name: string, namespaces: string[], services: number): FusionApplication => ({
  id, name, namespaces, clusters: ['cl-1'], signals: [],
  services: Array.from({ length: services }, (_, i) => ({ id: `${id}-s${i}`, name: `${name.toLowerCase()}-${i}`, kind: 'Deployment', namespace: namespaces[i % namespaces.length], cluster: 'cl-1', signals: [] })),
})
const applicationsDoc = (applications: FusionApplication[]): FusionApplications => ({ applications, sources: {}, warnings: [], from: '', to: '' })

beforeEach(() => {
  listFusionTokens.mockReset()
  createFusionToken.mockReset()
  revokeFusionToken.mockReset()
  getFusionApplications.mockReset()
  getFusionApplications.mockResolvedValue(applicationsDoc([]))
})

describe('expiryText', () => {
  test('counts whole days, and says so when it is today or over', () => {
    const now = Date.parse('2026-10-05T12:00:00Z')
    expect(expiryText('2026-10-20T12:00:00Z', now)).toBe('expires in 15 days')
    expect(expiryText('2026-10-05T18:00:00Z', now)).toBe('expires today')
    expect(expiryText('2026-10-05T11:00:00Z', now)).toBe('expired')
  })
})

describe('the API reference link', () => {
  test('is the server\'s own docs page, however the address is written', () => {
    expect(fusionDocsUrl('https://ikhnos.example')).toBe('https://ikhnos.example/api/v1/fusion/docs')
    expect(fusionDocsUrl('https://ikhnos.example/')).toBe('https://ikhnos.example/api/v1/fusion/docs')
  })
})

describe('FusionAccess', () => {
  test('offers the API reference in a new tab, even before there is a token', async () => {
    listFusionTokens.mockResolvedValue([])
    render(<FusionAccess />)
    const link = await screen.findByTestId('fusion-api-docs')
    expect(link).toHaveAttribute('href', 'https://ikhnos.example/api/v1/fusion/docs')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
  })

  test('lists each token with what it may read, when it expires and when it was last used', async () => {
    listFusionTokens.mockResolvedValue([
      token(),
      token({ id: 'fk-2', name: 'Shop dashboard', signals: ['traces', 'logs'], namespaces: ['shop', 'pay'], clusters: ['cl-1'], lastUsedAt: new Date(Date.now() - 3 * 3600_000).toISOString() }),
    ])
    render(<FusionAccess />)
    const first = await screen.findByTestId('fusion-token-fk-1')
    expect(within(first).getByText('Decision engine')).toBeInTheDocument()
    expect(within(first).getByText('all signals')).toBeInTheDocument()
    expect(within(first).getByText('all namespaces')).toBeInTheDocument()
    expect(within(first).getByText(/never used/)).toBeInTheDocument()
    const second = screen.getByTestId('fusion-token-fk-2')
    expect(within(second).getByText('traces + logs')).toBeInTheDocument()
    expect(within(second).getByText('namespaces: shop, pay')).toBeInTheDocument()
    expect(within(second).getByText('clusters: cl-1')).toBeInTheDocument()
    expect(within(second).getByText(/last used 3 h ago/)).toBeInTheDocument()
  })

  test('marks a token whose maker is no longer an administrator, and only that one, with a tooltip that says it still works', async () => {
    listFusionTokens.mockResolvedValue([
      token(),
      token({ id: 'fk-2', name: 'Old script', createdBy: 'dora', createdById: 'u-dora', creator: 'gone' }),
      token({ id: 'fk-3', name: 'Legacy', createdBy: 'root', createdById: undefined, creator: 'unknown' }),
    ])
    render(<FusionAccess />)
    const gone = await screen.findByTestId('fusion-token-fk-2')
    const mark = within(gone).getByText('creator no longer an admin')
    expect(mark).toHaveAttribute('title', creatorGoneHint(token({ createdBy: 'dora' })))
    expect(mark.getAttribute('title')).toMatch(/^Made by dora, who is no longer an administrator/)
    expect(mark.getAttribute('title')).toMatch(/It keeps working/)
    // Still an administrator, or not known either way: no mark.
    expect(within(screen.getByTestId('fusion-token-fk-1')).queryByText('creator no longer an admin')).toBeNull()
    expect(within(screen.getByTestId('fusion-token-fk-3')).queryByText('creator no longer an admin')).toBeNull()
    // The mark changes nothing: the token can be revoked as any other.
    expect(within(gone).getByRole('button', { name: 'Revoke Old script' })).toBeInTheDocument()
  })

  test('an empty list says so, and a failed one says why', async () => {
    listFusionTokens.mockResolvedValueOnce([])
    const { unmount } = render(<FusionAccess />)
    expect(await screen.findByTestId('fusion-token-empty')).toBeInTheDocument()
    unmount()
    const { ApiError } = await import('@/lib/api')
    listFusionTokens.mockRejectedValueOnce(new ApiError(403, 'Only an administrator can do that.'))
    render(<FusionAccess />)
    expect(await screen.findByText('Only an administrator can do that.')).toBeInTheDocument()
  })

  test('makes a token from the name, the signals, the namespaces and the lifetime, and shows its secret once', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    const created: CreatedFusionAccessToken = { token: 'cnf_SECRETSECRETSECRET', details: token({ id: 'fk-9', name: 'Shop dashboard', namespaces: ['shop'] }) }
    createFusionToken.mockResolvedValue(created)
    render(<FusionAccess />)
    await screen.findByTestId('fusion-token-empty')
    await user.click(screen.getByTestId('fusion-token-new'))

    const create = screen.getByTestId('fusion-token-create')
    expect(create).toBeDisabled() // no name yet
    await user.type(screen.getByTestId('fusion-token-name'), 'Shop dashboard')
    expect(create).toBeEnabled()
    await user.click(screen.getByTestId('checkbox-metrics')) // not metrics
    await user.type(screen.getByTestId('fusion-token-namespaces'), 'shop ')
    expect(screen.getByText(/can use the structured filters only/)).toBeInTheDocument()
    await user.click(create)

    expect(createFusionToken).toHaveBeenCalledWith(expect.anything(), { name: 'Shop dashboard', signals: ['logs', 'traces'], namespaces: ['shop'], clusters: [], expiresInDays: 90 })
    const secret = await screen.findByTestId('fusion-token-secret')
    expect(within(secret).getByText('cnf_SECRETSECRETSECRET')).toBeInTheDocument()
    expect(screen.getByTestId('fusion-token-curl')).toHaveTextContent('curl -H "Authorization: Bearer cnf_SECRETSECRETSECRET" https://ikhnos.example/api/v1/fusion/status')
    // The first thing a person tries fails on a default install (a self-signed certificate), so the dialog says what to add.
    expect(screen.getByTestId('fusion-token-selfsigned')).toHaveTextContent('-k')
    expect(screen.getByTestId('fusion-token-selfsigned')).toHaveTextContent('--cacert')
    await waitFor(() => expect(listFusionTokens).toHaveBeenCalledTimes(2)) // the list is read again
    await user.click(screen.getByRole('button', { name: 'Done' }))
    expect(screen.queryByText('cnf_SECRETSECRETSECRET')).not.toBeInTheDocument()
  })

  test('a token must be allowed to read something', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    await user.type(screen.getByTestId('fusion-token-name'), 'x')
    for (const s of ['metrics', 'logs', 'traces']) await user.click(screen.getByTestId(`checkbox-${s}`))
    expect(screen.getByTestId('fusion-token-create')).toBeDisabled()
  })

  test("the server's refusal is shown in the form", async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    const { ApiError } = await import('@/lib/api')
    createFusionToken.mockRejectedValue(new ApiError(400, '"shop;drop" is not a valid namespace'))
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    await user.type(screen.getByTestId('fusion-token-name'), 'x')
    await user.click(screen.getByTestId('fusion-token-create'))
    expect(await screen.findByText(/is not a valid namespace/)).toBeInTheDocument()
    expect(screen.queryByTestId('fusion-token-secret')).not.toBeInTheDocument()
  })

  test('revoking asks first, then ends the token and reads the list again', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValueOnce([token()]).mockResolvedValue([])
    revokeFusionToken.mockResolvedValue(undefined)
    render(<FusionAccess />)
    await user.click(await screen.findByRole('button', { name: 'Revoke Decision engine' }))
    expect(revokeFusionToken).not.toHaveBeenCalled()
    expect(screen.getByText(/stops being able to read FUSION at once/)).toBeInTheDocument()
    expect(screen.getByText(/It has never been used\./)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(revokeFusionToken).toHaveBeenCalledWith(expect.anything(), 'fk-1'))
    expect(await screen.findByTestId('fusion-token-empty')).toBeInTheDocument()
  })
})

describe('FusionAccess under React StrictMode (main.tsx renders the app inside it)', () => {
  test('the token list still fills in: the flag that stops a late answer is set back on the second mount', async () => {
    listFusionTokens.mockResolvedValue([token()])
    render(<StrictMode><FusionAccess /></StrictMode>)
    expect(await screen.findByTestId('fusion-token-fk-1')).toBeInTheDocument()
  })
})

describe('a token that is shown once', () => {
  const created: CreatedFusionAccessToken = { token: 'cnf_SECRETSECRETSECRET', details: token({ id: 'fk-9', name: 'Shop dashboard' }) }

  test('the created view ignores Escape and a click outside, holds the page from reloading, and Done is the way out', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    createFusionToken.mockResolvedValue(created)
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    await user.type(screen.getByTestId('fusion-token-name'), 'Shop dashboard')
    await user.click(screen.getByTestId('fusion-token-create'))
    const dialog = await screen.findByRole('dialog', { name: 'API token created' })
    expect(isReloadHeld()).toBe(true)
    await user.keyboard('{Escape}')
    fireEvent.mouseDown(dialog.parentElement!)
    expect(screen.getByRole('dialog', { name: 'API token created' })).toBeInTheDocument()
    expect(screen.getByText('cnf_SECRETSECRETSECRET')).toBeInTheDocument()
    await user.click(screen.getByTestId('fusion-token-done'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(isReloadHeld()).toBe(false)
  })

  test('while the token is being made there is no way out of the form: Escape, a click outside and Cancel do nothing', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    let finish: (c: CreatedFusionAccessToken) => void = () => undefined
    createFusionToken.mockImplementation(() => new Promise<CreatedFusionAccessToken>((r) => { finish = r }))
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    await user.type(screen.getByTestId('fusion-token-name'), 'Shop dashboard')
    await user.click(screen.getByTestId('fusion-token-create'))
    const dialog = screen.getByRole('dialog', { name: 'New API token' })
    await user.keyboard('{Escape}')
    fireEvent.mouseDown(dialog.parentElement!)
    expect(screen.getByRole('dialog', { name: 'New API token' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    await user.click(screen.getByTestId('fusion-token-create')) // a second press while it runs
    expect(createFusionToken).toHaveBeenCalledTimes(1)
    finish(created)
    expect(await screen.findByRole('dialog', { name: 'API token created' })).toBeInTheDocument()
  })

  test('before anything is sent the form closes the ordinary ways', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

describe('the certificate the curl line meets', () => {
  test('an IP address or localhost is on a self-signed certificate: the line carries -k; a name does not', () => {
    for (const base of ['https://147.102.13.108:8080', 'https://localhost:8080', 'https://[::1]:8080', 'https://ikhnos.localhost']) expect(selfSignedLikely(base)).toBe(true)
    for (const base of ['https://ikhnos.example', 'https://ikhnos.example:8080', 'not a url', '']) expect(selfSignedLikely(base)).toBe(false)
    expect(fusionStatusCurl('cnf_abc', 'https://147.102.13.108:8080')).toBe('curl -k -H "Authorization: Bearer cnf_abc" https://147.102.13.108:8080/api/v1/fusion/status')
    expect(fusionStatusCurl('cnf_abc', 'https://ikhnos.example')).not.toContain('-k')
  })
})

describe('fusionStatusCurl', () => {
  test('keeps the familiar double-quoted header for a plain token, and quotes anything else for the shell', () => {
    expect(fusionStatusCurl('cnf_abc', 'https://ikhnos.example')).toBe('curl -H "Authorization: Bearer cnf_abc" https://ikhnos.example/api/v1/fusion/status')
    expect(fusionStatusCurl('a"; rm -rf ~ #', 'https://x')).toBe(`curl -H 'Authorization: Bearer a"; rm -rf ~ #' https://x/api/v1/fusion/status`)
    expect(fusionStatusCurl('t', 'https://x/a b')).toBe(`curl -H "Authorization: Bearer t" 'https://x/a b/api/v1/fusion/status'`)
  })
})

describe('tokens limited to ikhnos applications', () => {
  test('the list names each application by its name now, and marks one that is gone', async () => {
    listFusionTokens.mockResolvedValue([
      token({ id: 'fk-2', name: 'Shop only', applications: [{ id: 'app-shop', name: 'Shop' }, { id: 'app-old', gone: true }, { id: 'app-unread' }],
        applicationMembers: [{ application: 'app-shop', id: 's-cart', name: 'cart', kind: 'Deployment', namespace: 'shop', cluster: 'cl-1' },
          { application: 'app-shop', id: 's-db', name: 'db', namespace: 'data', cluster: 'cl-2' }] }),
      token(),
    ])
    render(<FusionAccess />)
    const row = await screen.findByTestId('fusion-token-fk-2')
    expect(within(row).getByTestId('fusion-token-app-app-shop')).toHaveTextContent('application: Shop')
    // The services it was made with, collapsed under a summary.
    const members = within(row).getByTestId('fusion-token-members-fk-2')
    expect(members.tagName).toBe('DETAILS')
    expect(members).not.toHaveAttribute('open')
    expect(within(members).getByText('made with 2 services in 2 places')).toHaveAttribute('title', expect.stringMatching(/make a new token/))
    expect(within(members).getByText('cart (Deployment) in shop / cl-1')).toBeInTheDocument()
    expect(within(members).getByText('db in data / cl-2')).toBeInTheDocument()
    expect(screen.queryByTestId('fusion-token-members-fk-1')).toBeNull()
    const gone = within(row).getByText('application: app-old (gone)')
    expect(gone).toHaveAttribute('title', APP_GONE_HINT)
    expect(gone.className).toContain('text-warn')
    // Names that could not be read: the id alone, not said to be gone.
    expect(within(row).getByTestId('fusion-token-app-app-unread')).toHaveTextContent('application: app-unread')
    expect(within(row).getByTestId('fusion-token-app-app-unread').querySelector('[title]')).toBeNull()
    // A token not limited to applications shows none.
    expect(within(screen.getByTestId('fusion-token-fk-1')).queryByText(/application:/)).toBeNull()
    expect(tokenApplications(token({ applications: undefined as unknown as [] }))).toEqual([])
  })

  test('the form offers the organisation\'s applications, explains what they do, and sends the ids picked', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    getFusionApplications.mockResolvedValue(applicationsDoc([app('app-shop', 'Shop', ['shop'], 2), app('app-pay', 'Pay', ['pay'], 1)]))
    createFusionToken.mockResolvedValue({ token: 'cnf_SECRET', details: token({ id: 'fk-9', applications: [{ id: 'app-pay', name: 'Pay' }] }) } satisfies CreatedFusionAccessToken)
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    const list = await screen.findByTestId('fusion-token-apps')
    expect(within(list).getByText('Shop')).toBeInTheDocument()
    expect(within(list).getByText('shop · 2 services')).toBeInTheDocument()
    expect(within(list).getByText('pay · 1 service')).toBeInTheDocument()
    expect(screen.getByText(APPLICATIONS_HINT)).toBeInTheDocument()
    expect(screen.queryByTestId('fusion-token-preview')).toBeNull()
    await user.type(screen.getByTestId('fusion-token-name'), 'Pay dashboard')
    // Picked in any order, sent in the list's order.
    await user.click(screen.getByTestId('checkbox-app-pay'))
    await user.click(screen.getByTestId('checkbox-app-shop'))
    await user.click(screen.getByTestId('checkbox-app-shop'))
    // Before it is made, the form says exactly which services, where, the token will be made with.
    const preview = screen.getByTestId('fusion-token-preview')
    expect(preview).toHaveTextContent('It will be made with these 1 service in 1 place, and never see more')
    expect(within(screen.getByTestId('fusion-token-preview-app-pay')).getByText('pay-0 (Deployment) in pay / cl-1')).toBeInTheDocument()
    expect(screen.queryByTestId('fusion-token-preview-app-shop')).toBeNull()
    expect(screen.getByTestId('fusion-token-apps-note')).toHaveTextContent(/joins these applications later/)
    await user.click(screen.getByTestId('fusion-token-create'))
    // What was shown is what is approved.
    expect(createFusionToken).toHaveBeenCalledWith(expect.anything(), {
      name: 'Pay dashboard', signals: ['metrics', 'logs', 'traces'], namespaces: [], clusters: [], applications: ['app-pay'], members: ['app-pay-s0'], expiresInDays: 90,
    })
    expect(await screen.findByTestId('fusion-token-secret')).toBeInTheDocument()
  })

  test('with none picked the token is not limited to applications, and none in ikhnos says so', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    createFusionToken.mockResolvedValue({ token: 'cnf_SECRET', details: token() } satisfies CreatedFusionAccessToken)
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    expect(await screen.findByText('There are no applications in ikhnos yet.')).toBeInTheDocument()
    await user.type(screen.getByTestId('fusion-token-name'), 'x')
    await user.click(screen.getByTestId('fusion-token-create'))
    expect(createFusionToken.mock.calls[0][1]).not.toHaveProperty('applications')
  })

  test('applications that cannot be loaded are said, and the token can still be made without them', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    const { ApiError } = await import('@/lib/api')
    getFusionApplications.mockRejectedValue(new ApiError(503, 'FUSION is off.'))
    createFusionToken.mockResolvedValue({ token: 'cnf_SECRET', details: token() } satisfies CreatedFusionAccessToken)
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    expect(await screen.findByTestId('fusion-token-apps-error')).toHaveTextContent('FUSION is off.')
    await user.type(screen.getByTestId('fusion-token-name'), 'x')
    await user.click(screen.getByTestId('fusion-token-create'))
    await waitFor(() => expect(createFusionToken).toHaveBeenCalledTimes(1))
  })
})

describe('the services a new token is made with', () => {
  test('are each picked application\'s services with an id and a place, within the namespaces and clusters asked for', () => {
    const shop: FusionApplication = { ...app('app-shop', 'Shop', ['shop', 'data'], 3) }
    shop.services.push({ name: 'nowhere', signals: [] }, { id: 's-x', name: 'x', namespace: 'shop', signals: [] })
    expect(previewMembers([shop], ['app-shop'], [], []).map((m) => m.id)).toEqual(['app-shop-s0', 'app-shop-s1', 'app-shop-s2'])
    expect(previewMembers([shop], ['app-shop'], ['data'], []).map((m) => m.id)).toEqual(['app-shop-s1'])
    expect(previewMembers([shop], ['app-shop'], [], ['cl-9'])).toEqual([])
    expect(previewMembers([shop], [], [], [])).toEqual([])
    expect(membersSummary(previewMembers([shop], ['app-shop'], [], []))).toBe('3 services in 2 places')
  })

  test('an application that would give nothing within the limits is said so, and the token is not offered', async () => {
    const user = userEvent.setup()
    listFusionTokens.mockResolvedValue([])
    getFusionApplications.mockResolvedValue(applicationsDoc([app('app-shop', 'Shop', ['shop', 'data'], 2)]))
    render(<FusionAccess />)
    await user.click(await screen.findByTestId('fusion-token-new'))
    await user.type(screen.getByTestId('fusion-token-name'), 'x')
    await user.click(await screen.findByTestId('checkbox-app-shop'))
    expect(within(screen.getByTestId('fusion-token-preview-app-shop')).getByText('shop-0 (Deployment) in shop / cl-1')).toBeInTheDocument()
    expect(within(screen.getByTestId('fusion-token-preview-app-shop')).getByText('shop-1 (Deployment) in data / cl-1')).toBeInTheDocument()
    await user.type(screen.getByTestId('fusion-token-namespaces'), 'pay ')
    expect(screen.getByTestId('fusion-token-preview-app-shop')).toHaveTextContent(/would give the token nothing/)
    expect(screen.getByTestId('fusion-token-create')).toBeDisabled()
  })
})
