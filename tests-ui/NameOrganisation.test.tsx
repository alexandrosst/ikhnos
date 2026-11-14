import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import NameOrganisation from '@/features/accounts/components/NameOrganisation'
import { ApiError } from '@/lib/api'
import { useServer } from '@/store/server'

const renameOrg = vi.fn<(name: string) => Promise<void>>()

function seed(over: { name?: string; role?: string; status?: string } = {}) {
  useServer.setState({
    status: over.status ?? 'connected',
    orgs: [{ id: 'org1', name: over.name ?? 'Default', role: over.role ?? 'owner' }],
    orgId: 'org1',
    role: over.role ?? 'owner',
    renameOrg,
  } as never)
}

beforeEach(() => {
  localStorage.clear()
  renameOrg.mockReset()
  renameOrg.mockResolvedValue(undefined)
})

describe('NameOrganisation', () => {
  test('asks the owner of an organisation still called Default, with the name selected', async () => {
    seed()
    render(<NameOrganisation />)
    expect(screen.getByRole('dialog', { name: 'Name your organisation' })).toBeInTheDocument()
    expect(screen.getByTestId('org-name-input')).toHaveValue('Default')
  })

  test.each([
    ['a name that was already chosen', { name: 'Netmode lab' }],
    ['an administrator who is not the owner', { role: 'admin' }],
    ['a viewer', { role: 'viewer' }],
    ['a page not yet signed in', { status: 'signin' }],
  ])('does not ask for %s', (_label, over) => {
    seed(over)
    render(<NameOrganisation />)
    expect(screen.queryByTestId('org-name-dialog')).not.toBeInTheDocument()
  })

  test('saving sends the trimmed name and closes it', async () => {
    seed()
    const user = userEvent.setup()
    render(<NameOrganisation />)
    await user.clear(screen.getByTestId('org-name-input'))
    await user.type(screen.getByTestId('org-name-input'), '  Netmode lab ')
    await user.click(screen.getByTestId('org-name-save'))
    await waitFor(() => expect(renameOrg).toHaveBeenCalledWith('Netmode lab'))
    await waitFor(() => expect(screen.queryByTestId('org-name-dialog')).not.toBeInTheDocument())
  })

  test('Save needs a name', async () => {
    seed()
    const user = userEvent.setup()
    render(<NameOrganisation />)
    await user.clear(screen.getByTestId('org-name-input'))
    expect(screen.getByTestId('org-name-save')).toBeDisabled()
  })

  test('keeping the name as it is just closes, without asking the server', async () => {
    seed()
    const user = userEvent.setup()
    render(<NameOrganisation />)
    await user.click(screen.getByTestId('org-name-save'))
    expect(renameOrg).not.toHaveBeenCalled()
    expect(screen.queryByTestId('org-name-dialog')).not.toBeInTheDocument()
  })

  test('Later closes it, and it is not asked again in this browser', async () => {
    seed()
    const user = userEvent.setup()
    const first = render(<NameOrganisation />)
    await user.click(screen.getByTestId('org-name-later'))
    expect(screen.queryByTestId('org-name-dialog')).not.toBeInTheDocument()
    first.unmount()
    render(<NameOrganisation />)
    expect(screen.queryByTestId('org-name-dialog')).not.toBeInTheDocument()
  })

  test('a refusal stays on screen with its message and keeps the dialog open', async () => {
    seed()
    renameOrg.mockRejectedValue(new ApiError(400, 'name too long'))
    const user = userEvent.setup()
    render(<NameOrganisation />)
    await user.clear(screen.getByTestId('org-name-input'))
    await user.type(screen.getByTestId('org-name-input'), 'x')
    await user.click(screen.getByTestId('org-name-save'))
    expect(await screen.findByText('name too long')).toBeInTheDocument()
    expect(screen.getByTestId('org-name-dialog')).toBeInTheDocument()
  })
})
