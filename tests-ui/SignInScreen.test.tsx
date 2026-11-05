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
    expect(screen.getByText('See your clusters, from cloud to far edge.')).toBeInTheDocument()
    expect(screen.queryByText(/^Server:/)).toBeNull()
    expect(screen.queryByText(/without a server/i)).toBeNull()
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
})
