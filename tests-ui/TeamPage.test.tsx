import { render, screen, waitFor, within } from '@testing-library/react'
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
  { id: 'm3', username: 'dora', role: 'admin', you: false, joinedAt: '2026-01-03T00:00:00Z', lastLogin: '' },
]

const fusionToken = (id: string, name: string, createdById?: string) => ({
  id, name, signals: ['metrics'], namespaces: [], clusters: [], createdBy: 'dora', createdById, creator: createdById ? 'admin' : 'unknown',
  createdAt: '2026-01-04T00:00:00Z', expiresAt: '2026-12-04T00:00:00Z',
})
// dora made two; ada one; the legacy token names no one and is never counted as dora's.
const fusionTokens = [fusionToken('fk-1', 'Decision engine', 'm3'), fusionToken('fk-2', 'Shop dashboard', 'm3'), fusionToken('fk-3', 'Ada script', 'm1'), fusionToken('fk-4', 'Legacy')]

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

/** Picks a role in a member's role list (a listbox of its own, not a native select). */
async function pickRole(user: ReturnType<typeof userEvent.setup>, who: string, label: string) {
  await user.click(await screen.findByLabelText(`Role of ${who}`))
  await user.click(screen.getByRole('option', { name: label }))
}

beforeEach(() => {
  renameOrg.mockReset()
  renameOrg.mockResolvedValue(undefined)
  vi.spyOn(api, 'members').mockResolvedValue(members as never)
  vi.spyOn(api, 'invites').mockResolvedValue([])
  vi.spyOn(api, 'removeMember').mockReset().mockResolvedValue(undefined)
  vi.spyOn(api, 'setMemberRole').mockReset().mockResolvedValue(undefined)
  vi.spyOn(api, 'listFusionTokens').mockReset().mockResolvedValue(fusionTokens as never)
  vi.spyOn(api, 'revokeFusionTokensMadeBy').mockReset().mockResolvedValue({ revoked: ['fk-1', 'fk-2'] })
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

describe('an administrator who made FUSION tokens stops being one', () => {
  test('removing them says how many they made and names them, and by default revokes none', async () => {
    seed('owner')
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    const dialog = await screen.findByRole('dialog', { name: 'Remove dora?' })
    expect(within(dialog).getByText(/dora made 2 FUSION API tokens/)).toBeInTheDocument()
    const names = within(within(dialog).getByRole('list', { name: 'Their FUSION tokens' })).getAllByRole('listitem').map((li) => li.textContent)
    expect(names).toEqual(['Decision engine', 'Shop dashboard'])
    const box = within(dialog).getByRole('checkbox', { name: 'Also revoke the 2 FUSION tokens they made' })
    expect(box).not.toBeChecked()
    // The organisation also has a token from before makers were recorded: it might be theirs, so the dialog says so.
    expect(within(dialog).getByTestId('leaving-unknown')).toHaveTextContent('1 older token was made before ikhnos recorded who made them; check it in the Access tab.')
    expect(api.removeMember).not.toHaveBeenCalled() // nothing happens until it is confirmed
    await user.click(within(dialog).getByTestId('leaving-confirm'))
    await waitFor(() => expect(api.removeMember).toHaveBeenCalledWith(expect.anything(), 'm3'))
    expect(api.revokeFusionTokensMadeBy).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  test('ticking the box revokes their tokens too, once they have been removed', async () => {
    seed('owner')
    const user = userEvent.setup()
    const order: string[] = []
    vi.mocked(api.removeMember).mockImplementation(async () => { order.push('remove') })
    vi.mocked(api.revokeFusionTokensMadeBy).mockImplementation(async () => { order.push('revoke'); return { revoked: ['fk-1', 'fk-2'] } })
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    await user.click(await screen.findByTestId('leaving-revoke-tokens'))
    await user.click(screen.getByTestId('leaving-confirm'))
    await waitFor(() => expect(api.revokeFusionTokensMadeBy).toHaveBeenCalledWith(expect.anything(), 'm3'))
    expect(order).toEqual(['remove', 'revoke'])
  })

  test('the confirmation acts on the organisation it was asked in, even if another one was opened meanwhile', async () => {
    seed('owner')
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    await user.click(await screen.findByTestId('leaving-revoke-tokens'))
    const other = { url: 'https://ikhnos.example', token: 't', org: 'org2' }
    useServer.setState({ conn: () => other } as never)
    await user.click(screen.getByTestId('leaving-confirm'))
    await waitFor(() => expect(api.revokeFusionTokensMadeBy).toHaveBeenCalled())
    const asked = { url: 'https://ikhnos.example', token: 't' }
    expect(api.removeMember).toHaveBeenCalledWith(asked, 'm3')
    expect(api.revokeFusionTokensMadeBy).toHaveBeenCalledWith(asked, 'm3')
  })

  test('a demotion below administrator asks the same, and a refused one revokes nothing', async () => {
    seed('owner')
    const user = userEvent.setup()
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.setMemberRole).mockRejectedValueOnce(new ApiError(409, 'the organisation must keep at least one owner'))
    show()
    await pickRole(user, 'dora', 'Editor')
    const dialog = await screen.findByRole('dialog', { name: 'Make dora editor?' })
    await user.click(within(dialog).getByTestId('leaving-revoke-tokens'))
    await user.click(within(dialog).getByTestId('leaving-confirm'))
    expect(await screen.findByText('the organisation must keep at least one owner')).toBeInTheDocument()
    expect(api.setMemberRole).toHaveBeenCalledWith(expect.anything(), 'm3', 'editor')
    expect(api.revokeFusionTokensMadeBy).not.toHaveBeenCalled()
  })

  test('a revoke that fails part of the way says the change was made and some tokens may not have been revoked', async () => {
    seed('owner')
    const user = userEvent.setup()
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.revokeFusionTokensMadeBy).mockRejectedValueOnce(
      new ApiError(500, '1 of the tokens were revoked, then revoking the next one failed: internal error', { error: 'x', revoked: ['fk-1'] }))
    show()
    await pickRole(user, 'dora', 'Viewer')
    await user.click(await screen.findByTestId('leaving-revoke-tokens'))
    await user.click(screen.getByTestId('leaving-confirm'))
    expect(await screen.findByText(/^The role was changed, but some of their FUSION tokens may not have been revoked \(1 were\): 1 of the tokens were revoked.*See the Access tab on the FUSION page\.$/)).toBeInTheDocument()
    expect(api.setMemberRole).toHaveBeenCalledWith(expect.anything(), 'm3', 'viewer')
  })

  test('one token is named as one', async () => {
    seed('owner')
    vi.mocked(api.listFusionTokens).mockResolvedValue([fusionTokens[0]] as never)
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    expect(await screen.findByRole('checkbox', { name: 'Also revoke the FUSION token they made' })).not.toBeChecked()
    expect(screen.queryByTestId('leaving-unknown')).toBeNull() // no older tokens in this organisation
  })

  test('when only older tokens of unknown maker exist, it asks with the note and no box', async () => {
    seed('owner')
    vi.mocked(api.listFusionTokens).mockResolvedValue([fusionTokens[2], fusionTokens[3], fusionToken('fk-5', 'Older', undefined)] as never)
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    const dialog = await screen.findByRole('dialog', { name: 'Remove dora?' })
    expect(within(dialog).getByTestId('leaving-unknown')).toHaveTextContent('2 older tokens were made before ikhnos recorded who made them; check them in the Access tab.')
    expect(within(dialog).queryByRole('checkbox')).toBeNull()
    await user.click(within(dialog).getByTestId('leaving-confirm'))
    await waitFor(() => expect(api.removeMember).toHaveBeenCalledWith(expect.anything(), 'm3'))
    expect(api.revokeFusionTokensMadeBy).not.toHaveBeenCalled()
  })

  test('a list that cannot be read for an unexpected reason still asks, says so, and offers no box', async () => {
    seed('owner')
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.listFusionTokens).mockRejectedValueOnce(new ApiError(500, 'internal error'))
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    const dialog = await screen.findByRole('dialog', { name: 'Remove dora?' })
    expect(within(dialog).getByTestId('leaving-unchecked')).toHaveTextContent('Their FUSION tokens could not be checked; see the Access tab')
    expect(within(dialog).queryByRole('checkbox')).toBeNull()
    expect(api.removeMember).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(api.removeMember).not.toHaveBeenCalled()
    expect(screen.getByTestId('remove-dora')).toBeEnabled() // cancelling frees the row again
  })

  test('nothing is asked when they made none, when FUSION is refused or unavailable here, or when they stay an administrator', async () => {
    seed('owner')
    const user = userEvent.setup()
    const { ApiError } = await import('@/lib/api')
    vi.mocked(api.listFusionTokens).mockResolvedValueOnce([fusionTokens[2]] as never)
    show()
    await user.click(await screen.findByTestId('remove-dora'))
    await waitFor(() => expect(api.removeMember).toHaveBeenCalledWith(expect.anything(), 'm3'))
    // Outside the main organisation the token list is refused, and where FUSION is not available it is a conflict: nothing to offer.
    for (const [status, label] of [[403, 'Viewer'], [404, 'Editor'], [409, 'Viewer']] as const) {
      vi.mocked(api.setMemberRole).mockClear()
      vi.mocked(api.listFusionTokens).mockRejectedValueOnce(new ApiError(status, 'refused'))
      await pickRole(user, 'dora', label)
      await waitFor(() => expect(api.setMemberRole).toHaveBeenCalledWith(expect.anything(), 'm3', label.toLowerCase()))
    }
    vi.mocked(api.listFusionTokens).mockClear()
    await pickRole(user, 'dora', 'Owner')
    await waitFor(() => expect(api.setMemberRole).toHaveBeenCalledWith(expect.anything(), 'm3', 'owner'))
    expect(api.listFusionTokens).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(api.revokeFusionTokensMadeBy).not.toHaveBeenCalled()
  })

  test("the row is busy while its change is under way, so a second click starts nothing", async () => {
    seed('owner')
    let answer: (v: never) => void = () => undefined
    vi.mocked(api.listFusionTokens).mockImplementationOnce(() => new Promise((r) => { answer = r }))
    const user = userEvent.setup()
    show()
    const remove = await screen.findByTestId('remove-dora')
    await user.click(remove)
    await waitFor(() => expect(remove).toBeDisabled())
    expect(screen.getByLabelText('Role of dora')).toBeDisabled()
    await user.click(remove)
    expect(api.listFusionTokens).toHaveBeenCalledTimes(1)
    answer([] as never)
    await waitFor(() => expect(api.removeMember).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByTestId('remove-dora')).toBeEnabled())
  })

  test('a viewer who leaves is not asked about tokens', async () => {
    seed('owner')
    const user = userEvent.setup()
    show()
    await user.click(await screen.findByTestId('remove-grace'))
    await waitFor(() => expect(api.removeMember).toHaveBeenCalledWith(expect.anything(), 'm2'))
    expect(api.listFusionTokens).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
