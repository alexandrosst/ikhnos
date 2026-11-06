import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import ApprovalCard, { KUBECTL_UID } from '@/features/discovery/components/ApprovalCard'
import { CODE_LOG_COMMAND } from '@/features/discovery/lib/approvalCode'

// The card that lets an agent in. For an agent that prints an approval code the code is the whole decision, so it is what the card shows;
// the cluster's fingerprint is the check only for an older agent that has no code, and stays up front for that one.

const CONN = { url: '', org: 'org-1' }
const approve = vi.fn()
const reject = vi.fn()
const refresh = vi.fn(async () => undefined)
const server = { conn: () => CONN, refresh }
vi.mock('@/store/server', () => ({
  useServer: (selector?: (s: typeof server) => unknown) => (selector ? selector(server) : server),
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, approve: (...a: unknown[]) => approve(...a), reject: (...a: unknown[]) => reject(...a) } }
})

const agent = (over: Record<string, unknown> = {}) =>
  ({ id: 'a1', name: 'edge-patras', status: 'pending', fingerprint: 'c0ffee12-3456-789a-bcde-f01234567890', kubernetesVersion: 'v1.31.2', connectingIp: '10.0.0.7', version: '0.9.3', installedTier: 2, tierCap: 2, ...over }) as never

beforeEach(() => {
  approve.mockReset()
  approve.mockResolvedValue(undefined)
  reject.mockReset()
})

describe('ApprovalCard: an agent with an approval code', () => {
  test('shows the code to type and the command that reads it, and not the fingerprint', () => {
    render(<ApprovalCard agent={agent()} />)
    expect(screen.getByTestId('approval-code')).toBeVisible()
    expect(screen.getByTestId('approval-log-command')).toHaveTextContent(CODE_LOG_COMMAND)
    expect(screen.queryByTestId('approval-fingerprint')).toBeNull()
    expect(screen.queryByText('Copy kubectl command')).toBeNull()
    expect(KUBECTL_UID).toContain('kube-system')
  })

  test('Approve waits for a whole code, then sends it as the server reads it, with the access level', async () => {
    const user = userEvent.setup()
    render(<ApprovalCard agent={agent()} />)
    const button = screen.getByRole('button', { name: 'Approve' })
    expect(button).toBeDisabled()
    await user.type(screen.getByTestId('approval-code'), 'k7qm4tx')
    expect(button).toBeDisabled()
    await user.type(screen.getByTestId('approval-code'), 'd')
    expect(screen.getByTestId('approval-code')).toHaveValue('K7QM-4TXD')
    await user.click(button)
    await waitFor(() => expect(approve).toHaveBeenCalledWith(CONN, 'a1', 'K7QM4TXD', 2))
  })

  test('the access level can be lowered here, and the line for it can be left out', async () => {
    const user = userEvent.setup()
    const { unmount } = render(<ApprovalCard agent={agent()} />)
    expect(screen.getByTestId('approval-tier')).toHaveTextContent('Access:')
    await user.click(screen.getByTestId('approval-tier').querySelector('summary')!)
    await user.click(screen.getByTestId('approval-tier-pick-1'))
    await user.type(screen.getByTestId('approval-code'), 'k7qm4txd')
    await user.click(screen.getByRole('button', { name: 'Approve' }))
    await waitFor(() => expect(approve).toHaveBeenCalledWith(CONN, 'a1', 'K7QM4TXD', 1))
    unmount()
    render(<ApprovalCard agent={agent()} showAccess={false} />)
    expect(screen.queryByTestId('approval-tier')).toBeNull()
  })

  test('Reject asks the server to reject', async () => {
    const user = userEvent.setup()
    reject.mockResolvedValue(undefined)
    render(<ApprovalCard agent={agent()} />)
    await user.click(screen.getByRole('button', { name: 'Reject' }))
    await waitFor(() => expect(reject).toHaveBeenCalledWith(CONN, 'a1', 'rejected in the UI'))
  })
})

describe('ApprovalCard: an older agent with no code', () => {
  test('keeps the fingerprint and the kubectl command up front and asks for the start of the fingerprint', async () => {
    const user = userEvent.setup()
    render(<ApprovalCard agent={agent({ legacyEnrollment: true })} />)
    expect(screen.getByTestId('approval-fingerprint')).toHaveTextContent('c0ffee12-3456-789a-bcde-f01234567890')
    expect(screen.getByText('Copy kubectl command')).toBeInTheDocument()
    expect(screen.queryByTestId('approval-code')).toBeNull()
    const button = screen.getByRole('button', { name: 'Approve' })
    expect(button).toBeDisabled()
    await user.type(screen.getByLabelText('Fingerprint confirmation'), 'c0ffee12')
    await user.click(button)
    await waitFor(() => expect(approve).toHaveBeenCalledWith(CONN, 'a1', 'c0ffee12', 2))
  })
})
