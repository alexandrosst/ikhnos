import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it, beforeEach, vi } from 'vitest'
import AccountMenu from '@/features/accounts/components/AccountMenu'
import { useServer } from '@/store/server'
import { useWorkspace } from '@/store/workspace'
import type { User } from '@/lib/api'

const user: User = {
  id: 'u1',
  username: 'alexandrosst',
  mustChangePassword: false,
  createdAt: new Date().toISOString(),
  twoFactorEnabled: true, // avoids the two-factor nudge banner, which is unrelated to this test
  emailVerified: false,
  emailOtpEnabled: false,
  mailConfigured: false,
  canManageMail: false,
  passkeys: [],
}

function seedConnected(over: Record<string, unknown> = {}) {
  useServer.setState({
    status: 'connected',
    user,
    orgs: [{ id: 'org1', name: 'Acme', role: 'admin' }],
    orgId: 'org1',
    role: 'admin',
    registration: 'invite',
    checked: true,
    pendingMethods: [],
    ...over,
  } as never)
  useWorkspace.setState({ status: 'off' } as never)
}

// The sidebar this menu lives in always carries a CSS transform (for its mobile slide-in), which makes it
// the containing block for any `position: fixed` descendant - reproduced here because that's exactly the
// layout condition that broke the old full-screen overlay in production, even though it isn't what makes
// this test pass or fail (the fix no longer relies on any element's position at all).
function TransformedSidebar({ children }: { children: React.ReactNode }) {
  return <div style={{ transform: 'translateX(0)' }}>{children}</div>
}

describe('AccountMenu', () => {
  beforeEach(() => {
    seedConnected()
  })

  it('opens on click and closes when clicking elsewhere on the page, even inside a transformed ancestor', () => {
    render(
      <MemoryRouter>
        <TransformedSidebar>
          <AccountMenu />
        </TransformedSidebar>
        <button>Somewhere else entirely on the page</button>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByTestId('account').querySelector('button')!)
    expect(screen.getByTestId('account-menu')).toBeInTheDocument()

    // A real outside click is a mousedown followed by a click; the close logic runs on mousedown.
    fireEvent.mouseDown(screen.getByText('Somewhere else entirely on the page'))
    expect(screen.queryByTestId('account-menu')).not.toBeInTheDocument()
  })

  it('does not close when clicking inside the menu itself', () => {
    render(
      <MemoryRouter>
        <TransformedSidebar>
          <AccountMenu />
        </TransformedSidebar>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByTestId('account').querySelector('button')!)
    const menu = screen.getByTestId('account-menu')
    fireEvent.mouseDown(menu)
    expect(screen.getByTestId('account-menu')).toBeInTheDocument()
  })

  it('closes on Escape', () => {
    render(
      <MemoryRouter>
        <AccountMenu />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByTestId('account').querySelector('button')!)
    expect(screen.getByTestId('account-menu')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByTestId('account-menu')).not.toBeInTheDocument()
  })
})

describe('AccountMenu: what opens from your name', () => {
  const open = () => fireEvent.click(screen.getByTestId('account-button'))
  const renderMenu = () =>
    render(
      <MemoryRouter>
        <AccountMenu />
      </MemoryRouter>,
    )
  beforeEach(() => seedConnected())

  it('is short: who you are, your organisation, Members & access, Settings and Sign out', () => {
    renderMenu()
    open()
    const menu = screen.getByTestId('account-menu')
    expect(within(menu).getByTestId('account-identity')).toHaveTextContent('alexandrosst')
    expect(within(menu).getAllByRole('menuitem').map((i) => i.textContent?.trim())).toEqual(['Acme', 'Members & access', 'Settings', 'Sign out'])
  })

  it('has moved what is personal to Settings: no password, two-factor, tokens, theme or activity entries', () => {
    renderMenu()
    open()
    const menu = screen.getByTestId('account-menu')
    for (const text of [/password/i, /two-factor/i, /token/i, /who did what/i, /dark/i]) expect(within(menu).queryByText(text)).toBeNull()
  })

  it('shows the email under the name when there is one, the role when there is not', () => {
    seedConnected({ user: { ...user, email: 'alex@lab.example' } })
    const { unmount } = renderMenu()
    open()
    expect(screen.getByTestId('account-identity')).toHaveTextContent('alex@lab.example')
    unmount()
    seedConnected()
    renderMenu()
    open()
    expect(screen.getByTestId('account-identity')).toHaveTextContent('Administrator')
  })

  it('keeps organisations inside the organisation row: the list, New organisation and Join with a code', () => {
    seedConnected({ orgs: [{ id: 'org1', name: 'Acme', role: 'admin' }, { id: 'org2', name: 'Beta', role: 'viewer' }], registration: 'open' })
    renderMenu()
    open()
    expect(screen.queryByTestId('org-list')).toBeNull()
    fireEvent.click(screen.getByTestId('org-switch'))
    const list = screen.getByTestId('org-list')
    expect(within(list).getAllByTestId('org-option').map((o) => o.textContent)).toEqual(['Acme', 'Beta'])
    expect(within(list).getByRole('menuitemradio', { name: 'Acme' })).toHaveAttribute('aria-checked', 'true')
    expect(within(list).getByTestId('new-org-open')).toBeInTheDocument()
    expect(within(list).getByTestId('join-open')).toBeInTheDocument()
  })

  it('offers New organisation only when the server takes open sign-up', () => {
    renderMenu()
    open()
    fireEvent.click(screen.getByTestId('org-switch'))
    expect(screen.queryByTestId('new-org-open')).toBeNull()
    expect(screen.getByTestId('join-open')).toBeInTheDocument()
  })

  it('picking another organisation switches to it and closes the menu', () => {
    const selectOrg = vi.fn(async () => undefined)
    seedConnected({ orgs: [{ id: 'org1', name: 'Acme', role: 'admin' }, { id: 'org2', name: 'Beta', role: 'viewer' }], selectOrg })
    renderMenu()
    open()
    fireEvent.click(screen.getByTestId('org-switch'))
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Beta' }))
    expect(selectOrg).toHaveBeenCalledWith('org2')
    expect(screen.queryByTestId('account-menu')).toBeNull()
  })

  it('names the organisation under your name on the card, without a second switcher', () => {
    renderMenu()
    expect(screen.getByTestId('account-role')).toHaveTextContent('Administrator · Acme')
    expect(screen.queryByTestId('org-switch')).toBeNull()
  })

  it('Settings goes to Settings', () => {
    function Where() {
      return <span data-testid="where">{useLocation().pathname}</span>
    }
    render(
      <MemoryRouter>
        <Routes>
          <Route path="*" element={<><AccountMenu /><Where /></>} />
        </Routes>
      </MemoryRouter>,
    )
    open()
    fireEvent.click(screen.getByTestId('nav-settings'))
    expect(screen.getByTestId('where')).toHaveTextContent('/settings')
  })

  it('the second-step nudge points to Settings, and dismissing it is remembered', () => {
    localStorage.clear()
    seedConnected({ user: { ...user, twoFactorEnabled: false } })
    renderMenu()
    expect(screen.getByTestId('two-factor-nudge-setup')).toHaveAttribute('href', '/settings#account')
    fireEvent.click(screen.getByTestId('two-factor-nudge-dismiss'))
    expect(screen.queryByTestId('two-factor-nudge')).toBeNull()
    expect(localStorage.getItem('ikhnos:2fa-nudge-dismissed:u1')).toBe('1')
  })
})
