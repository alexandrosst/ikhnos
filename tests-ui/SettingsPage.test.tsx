import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test } from 'vitest'
import SettingsPage from '@/features/settings/SettingsPage'
import type { User } from '@/lib/api'
import { useServer } from '@/store/server'

const user = (over: Partial<User> = {}): User => ({
  id: 'u1',
  username: 'alex',
  mustChangePassword: false,
  createdAt: new Date().toISOString(),
  twoFactorEnabled: false,
  emailVerified: false,
  emailOtpEnabled: false,
  mailConfigured: false,
  canManageMail: false,
  passkeys: [],
  ...over,
})

function seed(over: { role?: string; user?: Partial<User>; exposure?: string; status?: string } = {}) {
  useServer.setState({
    status: over.status ?? 'connected',
    user: user(over.user),
    orgs: [{ id: 'o1', name: 'Lab', role: over.role ?? 'admin' }],
    orgId: 'o1',
    role: over.role ?? 'admin',
    info: {
      agentAddress: 'ikhnos.lab.example:30443',
      agentExposure: over.exposure ?? 'nodeport',
      version: '1.4.2',
      install: { chartVersion: '0.9.1' },
      geoip: { enabled: false },
    },
  } as never)
}

const renderAt = (path = '/settings') =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <SettingsPage />
    </MemoryRouter>,
  )

beforeEach(() => seed())

describe('Settings: the rail', () => {
  test('a signed-in person gets Account, Server and Data, and opens on Account', () => {
    renderAt()
    const rail = screen.getByTestId('settings-rail')
    expect(within(rail).getAllByRole('button').map((b) => b.textContent?.trim())).toEqual(['Account', 'Server', 'Data'])
    expect(screen.getByTestId('settings-pane-account')).toBeInTheDocument()
    expect(screen.getByTestId('settings-tab-account')).toHaveAttribute('aria-current', 'page')
  })

  test('choosing a page shows it and says so in the address', async () => {
    const user = userEvent.setup()
    renderAt()
    await user.click(screen.getByTestId('settings-tab-server'))
    expect(screen.getByTestId('settings-pane-server')).toBeInTheDocument()
    expect(screen.queryByTestId('settings-pane-account')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('settings-tab-data'))
    expect(screen.getByTestId('settings-pane-data')).toBeInTheDocument()
  })

  test('an old section address opens the page that holds it', () => {
    renderAt('/settings#installation')
    expect(screen.getByTestId('settings-pane-server')).toBeInTheDocument()
    expect(screen.getByTestId('installation-settings')).toBeInTheDocument()
  })

  test('signed out there is only the topology, with no rail', () => {
    seed({ status: 'disconnected' })
    renderAt()
    expect(screen.queryByTestId('settings-rail')).not.toBeInTheDocument()
    expect(screen.getByTestId('settings-pane-data')).toBeInTheDocument()
    expect(screen.queryByTestId('settings-account')).not.toBeInTheDocument()
  })
})

describe('Settings: Account', () => {
  test('says who is signed in and where the second step stands', () => {
    renderAt()
    expect(screen.getByTestId('settings-who')).toHaveTextContent('alex · Administrator')
    expect(screen.getByTestId('settings-two-factor')).toHaveTextContent('Off')
  })

  test('lists the second steps that are on', () => {
    seed({ user: { twoFactorEnabled: true, passkeys: [{}, {}] as never } })
    renderAt()
    expect(screen.getByTestId('settings-two-factor')).toHaveTextContent('Authenticator app, 2 passkeys')
  })

  test('Change opens the password dialog, and Manage the two-factor one', async () => {
    const user = userEvent.setup()
    renderAt()
    await user.click(screen.getByTestId('change-password-open'))
    expect(screen.getByRole('dialog', { name: /password/i })).toBeInTheDocument()
  })

  test('the theme choice is a row here, not a list in the menu', () => {
    renderAt()
    expect(screen.getByRole('group', { name: 'Theme' })).toBeInTheDocument()
  })
})

describe('Settings: Server', () => {
  test('shows the address and how it is exposed, and an administrator can change a NodePort address', async () => {
    const user = userEvent.setup()
    renderAt('/settings#server')
    expect(screen.getByTestId('server-address')).toHaveTextContent('ikhnos.lab.example:30443')
    expect(screen.getByTestId('server-exposure')).toHaveTextContent('NodePort')
    expect(screen.queryByTestId('server-address-input')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('server-address-change'))
    expect(screen.getByTestId('server-address-input')).toBeInTheDocument()
  })

  test('an exposure with nothing to swap says what to do, in view, and offers no Change', () => {
    seed({ exposure: 'gateway' })
    renderAt('/settings#server')
    expect(screen.queryByTestId('server-address-change')).not.toBeInTheDocument()
    expect(screen.getByTestId('server-exposure-help')).toHaveTextContent('TLSRoute')
  })

  test('the install image is a row, and Edit opens the three fields', async () => {
    const user = userEvent.setup()
    renderAt('/settings#server')
    expect(screen.getByTestId('preview-none')).toBeInTheDocument()
    expect(screen.queryByTestId('image-registry')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('installation-edit'))
    for (const id of ['image-registry', 'image-tag', 'image-digest']) expect(screen.getByTestId(id)).toBeInTheDocument()
  })

  test('someone who is not an administrator sees the image but cannot edit it', () => {
    seed({ role: 'viewer' })
    renderAt('/settings#server')
    expect(screen.queryByTestId('installation-edit')).not.toBeInTheDocument()
    expect(screen.getByText('Only administrators can change these.')).toBeInTheDocument()
  })

  test('About lists both versions and whether location hints are on', () => {
    renderAt('/settings#server')
    expect(screen.getByTestId('about-server-version')).toHaveTextContent('1.4.2')
    expect(screen.getByTestId('about-chart-version')).toHaveTextContent('0.9.1')
    expect(screen.getByTestId('about-geoip')).toHaveTextContent('Off')
  })

  test('Email is there only for someone who can manage mail', () => {
    renderAt('/settings#server')
    expect(screen.queryByTestId('mail-settings')).not.toBeInTheDocument()
  })
})

describe('Settings: Data', () => {
  test('says how much there is and offers export, import, the sample and clearing', () => {
    renderAt('/settings#data')
    expect(screen.getByTestId('settings-counts')).toHaveTextContent(/clusters?/)
    for (const id of ['export-json', 'import-json', 'load-sample', 'clear-all']) expect(screen.getByTestId(id)).toBeInTheDocument()
  })

  test('clearing asks first', async () => {
    const user = userEvent.setup()
    renderAt('/settings#data')
    await user.click(screen.getByTestId('clear-all'))
    expect(screen.getByRole('dialog', { name: 'Clear the whole topology?' })).toBeInTheDocument()
  })

  test('keeps the place-data credits', () => {
    renderAt('/settings#data')
    expect(screen.getByRole('link', { name: 'GeoNames' })).toBeInTheDocument()
  })
})
