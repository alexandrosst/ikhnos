import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import TeamPage from '@/features/accounts/TeamPage'
import { api } from '@/lib/api'
import { useServer } from '@/store/server'

const renameOrg = vi.fn<(name: string) => Promise<void>>()

const members = [
  { id: 'm1', username: 'ada', role: 'owner', you: true, joinedAt: '2026-01-01T00:00:00Z', lastLogin: '2026-02-01T00:00:00Z' },
  { id: 'm2', username: 'grace', role: 'viewer', you: false, joinedAt: '2026-01-02T00:00:00Z', lastLogin: '' },
]

function seed(role: string) {
  useServer.setState({
    status: 'connected',
    user: { id: 'u-1', username: 'ada' },
    role,
    orgId: 'org1',
    orgs: [{ id: 'org1', name: 'Netmode lab', role }],
    info: { orgId: 'org1', orgName: 'Netmode lab' },
    url: 'https://ikhnos.example',
    conn: () => ({ url: 'https://ikhnos.example', token: 't' }),
    renameOrg,
    reloadOrgs: vi.fn(),
  } as never)
}

const show = () => render(<MemoryRouter><TeamPage /></MemoryRouter>)

beforeEach(() => {
  renameOrg.mockReset()
  renameOrg.mockResolvedValue(undefined)
  vi.spyOn(api, 'members').mockResolvedValue(members as never)
  vi.spyOn(api, 'invites').mockResolvedValue([])
})

describe('Members & access', () => {
  test('shows the organisation name locked, and only the owner gets the pencil', async () => {
    seed('admin')
    show()
    expect(await screen.findByTestId('org-name')).toHaveTextContent('Netmode lab')
    expect(screen.queryByTestId('org-rename')).toBeNull()
  })

  test('the owner presses the pencil, edits the name and saves it', async () => {
    seed('owner')
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('org-rename'))
    expect(screen.queryByTestId('org-rename')).toBeNull()
    expect(screen.getByTestId('org-rename-save')).toBeDisabled()
    const input = screen.getByTestId('org-name-edit')
    expect(input).toHaveValue('Netmode lab')
    await user.clear(input)
    await user.type(input, '  Edge group ')
    await user.click(screen.getByTestId('org-rename-save'))
    expect(renameOrg).toHaveBeenCalledWith('  Edge group ')
    await waitFor(() => expect(screen.queryByTestId('org-rename-form')).toBeNull())
    expect(screen.getByTestId('org-rename')).toBeInTheDocument()
  })

  test('Cancel leaves the name as it was', async () => {
    seed('owner')
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('org-rename'))
    await user.type(screen.getByTestId('org-name-edit'), 'x')
    await user.click(screen.getByTestId('org-rename-cancel'))
    expect(renameOrg).not.toHaveBeenCalled()
    expect(screen.getByTestId('org-name')).toHaveTextContent('Netmode lab')
  })

  test('a refused rename says why and stays open', async () => {
    seed('owner')
    renameOrg.mockRejectedValue(new Error('no'))
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('org-rename'))
    await user.type(screen.getByTestId('org-name-edit'), '2')
    await user.click(screen.getByTestId('org-rename-save'))
    expect(await screen.findByText('Could not rename.')).toBeInTheDocument()
    expect(screen.getByTestId('org-rename-form')).toBeInTheDocument()
  })

  test('lists the members, with Leave on your own row and Remove on the others', async () => {
    seed('owner')
    show()
    expect(await screen.findByTestId('member-ada')).toBeInTheDocument()
    expect(screen.getByTestId('leave-org')).toBeInTheDocument()
    expect(screen.getByTestId('remove-grace')).toHaveAccessibleName('Remove grace')
    expect(screen.queryByTestId('remove-ada')).toBeNull()
  })

  test('only an owner sees the delete box', async () => {
    seed('admin')
    show()
    await screen.findByTestId('member-ada')
    expect(screen.queryByTestId('org-delete')).toBeNull()
  })
})
