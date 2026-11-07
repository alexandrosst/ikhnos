import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import AuthGate from '@/features/accounts/components/AuthGate'

// The sign-in page says what the product is for, not which server it talks to (that is the address in the browser), offers no way into a
// sample landscape, and tells someone with no account where to get one only when sign-up is by invitation.

let serverState: Record<string, unknown> = {}
vi.mock('@/store/server', () => ({
  useServer: (selector?: (s: Record<string, unknown>) => unknown) => (selector ? selector(serverState) : serverState),
}))

const base = { checked: true, status: 'signin', url: window.location.origin, error: undefined, signIn: vi.fn(), registration: 'closed', invite: undefined }
const show = (over: Record<string, unknown> = {}) => {
  serverState = { ...base, ...over }
  return render(<AuthGate><div>app</div></AuthGate>)
}
afterEach(() => { serverState = {} })

describe('the sign-in page', () => {
  test('the mark is centred above the name, and there is no server line or way into the sample landscape', () => {
    show()
    expect(screen.getByTestId('auth-brand')).toHaveClass('flex-col', 'items-center')
    expect(screen.getByTestId('brand-mark')).toHaveClass('h-16')
    // The tagline is under the name, outside the card; the card has its own title and the form.
    const tagline = screen.getByTestId('auth-tagline')
    expect(tagline).toHaveTextContent('See your clusters, from cloud to far edge.')
    expect(screen.getByTestId('auth-brand')).toContainElement(tagline)
    const title = screen.getByRole('heading', { name: 'Welcome' })
    expect(title).not.toHaveClass('sr-only')
    expect(title.closest('.auth-card')).not.toBeNull()
    expect(screen.queryByText('Welcome back')).toBeNull()
    expect(screen.queryByText(/^Server:/)).toBeNull()
    expect(screen.queryByText(/without a server/i)).toBeNull()
  })

  test('the version of the server is at the foot of the page, above the credits, as the server says it', () => {
    const first = show({ serverVersion: '0.1.0' })
    expect(screen.getByTestId('auth-version')).toHaveTextContent('Version 0.1.0')
    expect(screen.getByTestId('copyright').parentElement).toContainElement(screen.getByTestId('auth-version'))
    expect(screen.getByTestId('auth-version').compareDocumentPosition(screen.getByTestId('copyright')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    first.unmount()
    show({ serverVersion: 'edge-3fa9c1' })
    expect(screen.getByTestId('auth-version')).toHaveTextContent('Version edge-3fa9c1')
  })

  test('no version line while the server has not said one', () => {
    show()
    expect(screen.queryByTestId('auth-version')).toBeNull()
  })

  test('the server is named only when it is not the one the page came from', () => {
    show({ url: 'https://other.example:8080' })
    expect(screen.getByTestId('auth-server')).toHaveTextContent('Server: https://other.example:8080')
  })

  test('a server that takes accounts by invitation says where to get one', () => {
    show({ registration: 'invite' })
    expect(screen.getByTestId('invite-only')).toHaveTextContent('Ask an administrator for an invitation link')
  })

  test('a server that takes open sign-up offers it and says nothing of invitations', () => {
    show({ registration: 'open' })
    expect(screen.getByTestId('to-register')).toBeInTheDocument()
    expect(screen.queryByTestId('invite-only')).toBeNull()
  })

  test('a faint network sits behind the form, drawn the same way every time and from the theme\'s accent', () => {
    const first = show()
    const backdrop = screen.getByTestId('auth-backdrop')
    expect(backdrop).toHaveAttribute('aria-hidden', 'true')
    expect(backdrop).toHaveClass('pointer-events-none')
    expect(backdrop.querySelectorAll('circle').length).toBeGreaterThan(30)
    expect(backdrop.querySelectorAll('line').length).toBeGreaterThan(30)
    expect(backdrop.innerHTML).not.toMatch(/#[0-9a-f]{3,6}/i)
    const drawn = backdrop.innerHTML
    first.unmount()
    show()
    expect(screen.getByTestId('auth-backdrop').innerHTML).toBe(drawn)
  })

  test('the credits are at the foot of the page, outside the card, on the sign-in page and on the other screens', () => {
    const first = show()
    const copyright = screen.getByTestId('copyright')
    expect(copyright.closest('.auth-card')).toBeNull()
    expect(screen.getByTestId('auth-brand').closest('.auth-card')).toBeNull()
    first.unmount()
    show({ registration: 'open', status: 'twofactor', pendingMethods: ['totp'] })
    expect(screen.getByTestId('copyright').closest('.auth-card')).toBeNull()
  })
})
