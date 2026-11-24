import { Copy, Link2, LogOut, Pencil, Trash2, UserPlus } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useHoldReload } from '@/lib/useHoldReload'
import { Button, Callout, Checkbox, CopyValue, Disclosure, ErrorBanner, Field, ICON_SM, Input, Modal, PageHeader, Pill, SectionHeading, Select, Table, TableSkeleton, Td, Th } from '@/components/ui/primitives'
import { Group, RowMore, SettingRow } from '@/components/ui/SettingsParts'
import { api, ApiError, grantable, ROLE_HELP, ROLE_LABEL, type Conn, type FusionAccessToken, type Invite, type Member, type Role } from '@/lib/api'
import { useServer } from '@/store/server'

const when = (iso?: string) => (iso ? new Date(iso).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : 'never')
const problem = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

function ErrorLine({ text }: { text: string }) {
  return text ? <ErrorBanner className="mb-4">{text}</ErrorBanner> : null
}

/** The invitation, shown once: the server keeps only a hash, so this is the only chance to copy it. */
function InviteCreated({ token, invite, url, org, onClose }: { token: string; invite: Invite; url: string; org: string; onClose: () => void }) {
  const link = `${url || window.location.origin}/?invite=${token}`
  // Shown once: not closable by a backdrop click or Escape, and the page does not reload itself under it.
  useHoldReload()
  return (
    <Modal open onClose={onClose} dismissible={false} title="Invitation created" width="max-w-lg" footer={<Button variant="primary" onClick={onClose}>Done</Button>}>
      <p className="text-sm text-nb-400">
        Send this to {invite.label ? <strong className="text-nb-300">{invite.label}</strong> : 'the person you are inviting'}. It lets one person join <strong className="text-nb-300">{org}</strong> as {ROLE_LABEL[invite.role].toLowerCase()}, once, until {when(invite.expiresAt)}.
        It is shown only now; if it is lost, make another.
      </p>
      <div className="mt-4 space-y-3">
        <div>
          <div className="mb-1 text-xs text-nb-500">Link</div>
          <CopyValue value={link} testId="invite-link" icon={<Link2 size={ICON_SM} />} />
        </div>
        <div>
          <div className="mb-1 text-xs text-nb-500">Or just the code, for someone who already has an account</div>
          <CopyValue value={token} testId="invite-code" icon={<Copy size={ICON_SM} />} />
        </div>
      </div>
    </Modal>
  )
}

/** The organisation's name, locked until its owner presses the pencil, as a record's name is in every other panel. */
function OrganisationGroup({ owner }: { owner: boolean }) {
  const info = useServer((s) => s.info)
  const renameOrg = useServer((s) => s.renameOrg)
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const current = info?.orgName ?? ''
  const start = () => { setName(current); setError(''); setEditing(true) }
  const save = async () => {
    setBusy(true)
    try {
      setError('')
      await renameOrg(name)
      setEditing(false)
    } catch (e) {
      setError(problem(e, 'Could not rename.'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Group title="Organisation" id="organisation">
      <SettingRow
        label="Name"
        value={current}
        valueTestId="org-name"
        action={owner && !editing ? (
          <Button variant="ghost" size="sm" aria-label="Rename the organisation" title="Rename the organisation" onClick={start} data-testid="org-rename"><Pencil size={ICON_SM} /></Button>
        ) : undefined}
      />
      {editing && (
        <RowMore testId="org-rename-form">
          <form className="flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (name.trim().length >= 2 && name.trim() !== current) void save() }}>
            <Field label="New name" className="max-w-sm flex-1"><Input value={name} onChange={(e) => setName(e.target.value)} autoFocus data-testid="org-name-edit" /></Field>
            <Button type="submit" variant="primary" disabled={busy || name.trim().length < 2 || name.trim() === current} data-testid="org-rename-save">Save</Button>
            <Button onClick={() => setEditing(false)} data-testid="org-rename-cancel">Cancel</Button>
          </form>
          {error && <ErrorBanner>{error}</ErrorBanner>}
        </RowMore>
      )}
    </Group>
  )
}

function Danger() {
  const conn = useServer((s) => s.conn)
  const info = useServer((s) => s.info)
  const reloadOrgs = useServer((s) => s.reloadOrgs)
  const [confirm, setConfirm] = useState('')
  const [open, setOpen] = useState(false)
  const [error, setError] = useState('')

  const remove = async () => {
    const c = conn()
    if (!c) return
    try {
      setError('')
      await api.deleteOrg(c, confirm)
      setOpen(false)
      await reloadOrgs()
    } catch (e) {
      setError(problem(e, 'Could not delete.'))
    }
  }

  return (
    <section className="mt-6" aria-label="Delete this organisation">
      <ErrorLine text={error} />
      <Callout tone="bad" className="max-w-lg">
        <div className="text-sm font-medium text-bad">Delete this organisation</div>
        <p className="mt-1 text-xs text-nb-400">Erases its agents, topology, history and memberships for good. The people keep their accounts and their other organisations. The audit trail of who deleted it is kept.</p>
        <Button className="mt-3" variant="danger" onClick={() => { setConfirm(''); setOpen(true) }} data-testid="org-delete"><Trash2 size={ICON_SM} /> Delete organisation…</Button>
      </Callout>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Delete this organisation?"
        description="This cannot be undone."
        width="max-w-md"
        footer={<><Button onClick={() => setOpen(false)}>Cancel</Button><Button variant="danger" onClick={remove} disabled={confirm.trim() !== info?.orgName} data-testid="org-delete-confirm">Delete for good</Button></>}
      >
        <Field label={`Type “${info?.orgName ?? ''}” to confirm`}><Input value={confirm} onChange={(e) => setConfirm(e.target.value)} autoFocus data-testid="org-delete-name" /></Field>
      </Modal>
    </section>
  )
}

/** A row's one action, quiet until the row is hovered or focused: the same as the other lists'. */
function RowAction({ label, onClick, testId, disabled }: { label: string; onClick: () => void; testId?: string; disabled?: boolean }) {
  return (
    <div className="flex justify-end opacity-60 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
      <Button variant="ghost" size="sm" aria-label={label} title={label} onClick={onClick} disabled={disabled} data-testid={testId}><Trash2 size={ICON_SM} /></Button>
    </div>
  )
}

const administers = (r: Role) => r === 'admin' || r === 'owner'

/** What the token list says about one person's FUSION tokens. `none`: there is nothing to say (this organisation has no FUSION,
 *  or FUSION cannot be reached from here). `listed`: the tokens they made, and how many of the organisation's tokens are from
 *  before makers were recorded, so might be theirs. `unchecked`: the list could not be read for another reason, so nobody can
 *  say, and the change is not made without saying so. */
type TokenCheck = { kind: 'none' } | { kind: 'listed'; mine: FusionAccessToken[]; unknown: number } | { kind: 'unchecked' }

/** A member who stops being an administrator (removed, or given a role below administrator), with what is known of their FUSION
 *  tokens. `role` is the new role; absent, they are being removed. `c` is the connection, and so the organisation, the change
 *  was asked in: the confirmation acts on that one even if another organisation was opened meanwhile. */
type Leaving = { member: Member; role?: Role; check: Exclude<TokenCheck, { kind: 'none' }>; c: Conn }

/** Reads the token list for what it says about one person. Only the server's main organisation has FUSION tokens: anywhere else,
 *  and where FUSION is not available, the list is refused (403, 404, or 409 "unavailable"), which is the expected answer and
 *  means there is nothing to offer. Any other failure is reported as unchecked, never as "none". */
async function checkTokens(c: Conn, id: string): Promise<TokenCheck> {
  try {
    const all = await api.listFusionTokens(c)
    return { kind: 'listed', mine: all.filter((t) => t.createdById === id), unknown: all.filter((t) => t.creator === 'unknown').length }
  } catch (e) {
    if (e instanceof ApiError && [403, 404, 409].includes(e.status)) return { kind: 'none' }
    return { kind: 'unchecked' }
  }
}

/** Whether the change needs the confirmation: they made tokens, some tokens might be theirs (made before makers were recorded),
 *  or nobody could tell. */
const worthAsking = (check: TokenCheck): check is Leaving['check'] =>
  check.kind === 'unchecked' || (check.kind === 'listed' && (check.mine.length > 0 || check.unknown > 0))

/** The step that takes an administrator's standing away, when FUSION tokens may be left behind: it says how many they made and
 *  names them, and offers to revoke them too. The offer starts unticked, because a token belongs to the organisation and keeps
 *  working without its maker (the FUSION page marks it); revoking is a choice of its own. */
function LeavingAdminDialog({ leaving, onConfirm, onClose }: { leaving: Leaving; onConfirm: (revoke: boolean) => void; onClose: () => void }) {
  const [revoke, setRevoke] = useState(false)
  const { member, role, check } = leaving
  const mine = check.kind === 'listed' ? check.mine : []
  const n = mine.length
  const what = n === 1 ? 'the FUSION token' : `the ${n} FUSION tokens`
  return (
    <Modal
      open
      onClose={onClose}
      title={role ? `Make ${member.username} ${ROLE_LABEL[role].toLowerCase()}?` : `Remove ${member.username}?`}
      width="max-w-md"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant={role ? 'primary' : 'danger'} onClick={() => onConfirm(n > 0 && revoke)} data-testid="leaving-confirm">
            {role ? 'Change role' : 'Remove'}
          </Button>
        </>
      }
    >
      <div className="space-y-3 text-sm text-nb-400" data-testid="leaving-admin">
        {check.kind === 'unchecked' ? (
          <p data-testid="leaving-unchecked">Their FUSION tokens could not be checked; see the Access tab on the FUSION page.</p>
        ) : (
          <>
            {n > 0 ? (
              <>
                <p>
                  {member.username} made {n === 1 ? 'a FUSION API token. It belongs' : `${n} FUSION API tokens. They belong`} to the organisation and keep{n === 1 ? 's' : ''} working once {member.username} is no longer an administrator; the FUSION page marks {n === 1 ? 'it' : 'them'}.
                </p>
                <ul className="flex flex-wrap gap-1" aria-label="Their FUSION tokens">
                  {mine.map((t) => <li key={t.id}><Pill className="text-xs text-nb-300">{t.name}</Pill></li>)}
                </ul>
                <label className="flex cursor-pointer items-center gap-2">
                  <Checkbox checked={revoke} onChange={(e) => setRevoke(e.target.checked)} data-testid="leaving-revoke-tokens" />
                  <span className="text-nb-300">Also revoke {what} they made</span>
                </label>
              </>
            ) : (
              <p>{member.username} made none of the FUSION API tokens ikhnos knows the maker of.</p>
            )}
            {check.unknown > 0 && (
              <p data-testid="leaving-unknown">
                {check.unknown === 1 ? '1 older token was' : `${check.unknown} older tokens were`} made before ikhnos recorded who made them; check {check.unknown === 1 ? 'it' : 'them'} in the Access tab.
              </p>
            )}
          </>
        )}
      </div>
    </Modal>
  )
}

/** Who is in this organisation, what each may do, and how to bring someone in. */
export default function TeamPage() {
  const conn = useServer((s) => s.conn)
  const role = useServer((s) => s.role)
  const info = useServer((s) => s.info)
  const url = useServer((s) => s.url)
  const reloadOrgs = useServer((s) => s.reloadOrgs)
  const [members, setMembers] = useState<Member[]>([])
  const [invites, setInvites] = useState<Invite[]>([])
  // Without this, the member/invite fetch this page fires on every mount left a brief but real window
  // where `members` was still its initial `[]` - reading, wrongly, as "this org has no one in it" rather
  // than "still loading", exactly the scenario PipelinePage's own operatorsLoaded flag exists to
  // avoid for the identical kind of org-scoped fetch.
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [inviting, setInviting] = useState(false)
  const [invRole, setInvRole] = useState<Role>('viewer')
  const [label, setLabel] = useState('')
  const [created, setCreated] = useState<{ token: string; invite: Invite } | null>(null)
  const [leaving, setLeaving] = useState<Leaving | null>(null)
  // The member whose change is under way (the token lookup, the confirmation, the change itself): their row's controls are off
  // meanwhile, so a second click cannot start a second change.
  const [busy, setBusyState] = useState<string | null>(null)
  // The same, read synchronously: two clicks in one tick both see the state from before either of them.
  const busyNow = useRef<string | null>(null)
  const setBusy = (id: string | null) => {
    busyNow.current = id
    setBusyState(id)
  }
  const mine = grantable(role)
  const canInvite = mine.length > 0

  // Only the newest request may land: after an organisation switch the previous one's members and invites must not appear in the new view.
  const newest = useRef(0)
  const load = useCallback(async () => {
    const c = conn()
    if (!c) return
    const mine = ++newest.current
    try {
      const m = await api.members(c)
      if (mine !== newest.current) return
      setMembers(m)
      const i = canInvite ? await api.invites(c) : []
      if (mine !== newest.current) return
      setInvites(i)
      setError('')
    } catch (e) {
      if (mine !== newest.current) return
      setError(problem(e, 'Could not load the members.'))
    } finally {
      if (mine === newest.current) setLoaded(true)
    }
  }, [conn, canInvite])
  useEffect(() => {
    void load()
  }, [load, info?.orgId])

  const act = async (f: () => Promise<void>, fallback: string) => {
    try {
      setError('')
      await f()
      await load()
    } catch (e) {
      setError(problem(e, fallback))
    }
  }

  /** Removes m (role absent) or gives them role. An administrator who stops being one is asked first when FUSION tokens may be
   *  left behind (LeavingAdminDialog); anyone else changes at once, as before. */
  const change = async (m: Member, to?: Role) => {
    const c = conn()
    if (!c || busyNow.current) return
    setBusy(m.id)
    let asked = false
    try {
      const check: TokenCheck = administers(m.role) && !(to && administers(to)) ? await checkTokens(c, m.id) : { kind: 'none' }
      if (worthAsking(check)) {
        asked = true // the row stays busy until the confirmation is answered
        setLeaving({ member: m, role: to, check, c })
        return
      }
      await act(async () => { await (to ? api.setMemberRole(c, m.id, to) : api.removeMember(c, m.id)) }, to ? 'Could not change the role.' : 'Could not remove the member.')
    } finally {
      if (!asked) setBusy(null)
    }
  }

  const cancelLeaving = () => {
    setLeaving(null)
    setBusy(null)
  }

  /** The member's change comes first and the revoke only once it has been made: a change the server refuses (the last owner,
   *  say) then revokes nothing. A revoke that fails part of the way leaves some tokens working, as they would be without the
   *  offer, which is safe, and the page says so. Both act on the connection the change was asked in. */
  const leave = async (l: Leaving, revoke: boolean) => {
    setLeaving(null)
    const { c } = l
    try {
      try {
        setError('')
        await (l.role ? api.setMemberRole(c, l.member.id, l.role) : api.removeMember(c, l.member.id))
      } catch (e) {
        setError(problem(e, l.role ? 'Could not change the role.' : 'Could not remove the member.'))
        return
      }
      let failed = ''
      if (revoke) {
        try {
          await api.revokeFusionTokensMadeBy(c, l.member.id)
        } catch (e) {
          const done = e instanceof ApiError && Array.isArray(e.body?.revoked) ? e.body.revoked.length : 0
          failed = `${l.role ? 'The role was changed' : `${l.member.username} was removed`}, but some of their FUSION tokens may not have been revoked${done > 0 ? ` (${done} were)` : ''}: ${problem(e, 'the server refused.')} See the Access tab on the FUSION page.`
        }
      }
      await load()
      if (failed) setError(failed) // after the reload, which clears the line when it succeeds
    } finally {
      setBusy(null)
    }
  }

  const invite = () =>
    act(async () => {
      const c = conn()
      if (!c) return
      const r = await api.createInvite(c, invRole, label.trim())
      setInviting(false)
      setLabel('')
      setCreated(r)
    }, 'Could not send the invite.')

  if (!role) return null
  const open = invites.filter((i) => !i.used && !i.expired)
  const past = invites.filter((i) => i.used || i.expired)

  return (
    <>
      <PageHeader
        title="Members & access"
        description={`People in ${info?.orgName ?? 'this organisation'}. What belongs to it is visible to nobody outside it.`}
        actions={canInvite ? <Button variant="primary" onClick={() => { setInvRole(mine.includes('viewer') ? 'viewer' : mine[0]); setInviting(true) }} data-testid="invite-open"><UserPlus size={ICON_SM} /> Invite someone</Button> : undefined}
      />
      <ErrorLine text={error} />

      <OrganisationGroup owner={role === 'owner'} />

      <section className="mt-8" aria-labelledby="mem-h">
      <SectionHeading id="mem-h" className="mb-3">Members</SectionHeading>
      {!loaded ? (
        <TableSkeleton cols={['', '', '', '', '']} />
      ) : (
      <Table>
        <thead>
          <tr><Th>Person</Th><Th>Role</Th><Th>Joined</Th><Th>Last sign-in</Th><Th /></tr>
        </thead>
        <tbody>
          {members.map((m) => {
            const manageable = !m.you && mine.includes(m.role)
            return (
              <tr key={m.id} className="group hover:bg-nb-930/60" data-testid={`member-${m.username}`}>
                <Td><span className="text-nb-300">{m.username}</span>{m.you && <span className="ml-2 text-xs text-nb-500">(you)</span>}</Td>
                <Td>
                  {manageable ? (
                    <Select value={m.role} aria-label={`Role of ${m.username}`} onChange={(e) => void change(m, e.target.value as Role)} disabled={busy === m.id} className="h-8 w-36">
                      {mine.map((r) => (
                        <option key={r} value={r}>{ROLE_LABEL[r]}</option>
                      ))}
                    </Select>
                  ) : (
                    <Pill>{ROLE_LABEL[m.role]}</Pill>
                  )}
                </Td>
                <Td className="text-nb-500">{when(m.joinedAt)}</Td>
                <Td className="text-nb-500">{when(m.lastLogin)}</Td>
                <Td className="text-right">
                  {manageable && (
                    <RowAction label={`Remove ${m.username}`} testId={`remove-${m.username}`} onClick={() => void change(m)} disabled={busy === m.id} />
                  )}
                  {m.you && (
                    <Button size="sm" onClick={() => void act(async () => { const c = conn(); if (!c) return; await api.leaveOrg(c); await reloadOrgs() }, 'Could not leave the organisation.')} data-testid="leave-org"><LogOut size={ICON_SM} /> Leave</Button>
                  )}
                </Td>
              </tr>
            )
          })}
        </tbody>
      </Table>
      )}
      </section>

      {canInvite && (
        <section className="mt-8" aria-labelledby="inv-h">
          <SectionHeading id="inv-h" className="mb-3">Invitations</SectionHeading>
          {!loaded ? (
            <TableSkeleton cols={['', '', '', '', '']} />
          ) : invites.length === 0 ? (
            <p className="text-sm text-nb-500">None yet. An invitation is one link: it works once and expires after seven days.</p>
          ) : (
            <Table>
              <thead>
                <tr><Th>For</Th><Th>Role</Th><Th>Made by</Th><Th>Status</Th><Th /></tr>
              </thead>
              <tbody>
                {[...open, ...past].map((i) => (
                  <tr key={i.id} className="group hover:bg-nb-930/60" data-testid={`invite-${i.label || i.id}`}>
                    <Td className="text-nb-300">{i.label || <span className="text-nb-500">anyone with the link</span>}</Td>
                    <Td><Pill>{ROLE_LABEL[i.role]}</Pill></Td>
                    <Td className="text-nb-500">{i.createdBy}</Td>
                    <Td><Pill>{i.used ? `used by ${i.usedBy || 'someone'}` : i.expired ? 'expired' : `open until ${when(i.expiresAt)}`}</Pill></Td>
                    <Td className="text-right">
                      {!i.used && !i.expired && mine.includes(i.role) && (
                        <RowAction label="Withdraw this invitation" testId={`withdraw-${i.label || i.id}`} onClick={() => void act(async () => { const c = conn(); if (c) await api.revokeInvite(c, i.id) }, 'Could not withdraw the invite.')} />
                      )}
                    </Td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </section>
      )}

      <Disclosure title="What each role can do" className="mt-8 max-w-3xl" testId="roles-help">
        <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[8rem_1fr]">
          {(['viewer', 'editor', 'admin', 'owner'] as Role[]).map((r) => (
            <div key={r} className="contents">
              <dt className="text-nb-300">{ROLE_LABEL[r]}</dt>
              <dd className="text-nb-400">{ROLE_HELP[r]}</dd>
            </div>
          ))}
        </dl>
      </Disclosure>

      {role === 'owner' && <Danger />}

      <Modal
        open={inviting}
        onClose={() => setInviting(false)}
        title="Invite someone"
        description="They join this organisation with the role you pick. There is no email: you send them the link yourself."
        width="max-w-md"
        footer={<><Button onClick={() => setInviting(false)}>Cancel</Button><Button variant="primary" onClick={invite} data-testid="invite-create">Create invitation</Button></>}
      >
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); void invite() }}>
          <Field label="Role" hint={ROLE_HELP[invRole]}>
            <Select value={invRole} onChange={(e) => setInvRole(e.target.value as Role)} data-testid="invite-role">
              {mine.map((r) => (
                <option key={r} value={r}>{ROLE_LABEL[r]}</option>
              ))}
            </Select>
          </Field>
          <Field label="Who is it for?" hint="Optional. Only a note for you, so you can tell invitations apart."><Input value={label} onChange={(e) => setLabel(e.target.value)} maxLength={80} data-testid="invite-label" /></Field>
        </form>
      </Modal>
      {leaving && <LeavingAdminDialog leaving={leaving} onConfirm={(revoke) => void leave(leaving, revoke)} onClose={cancelLeaving} />}
      {created && <InviteCreated token={created.token} invite={created.invite} url={url} org={info?.orgName ?? 'this organisation'} onClose={() => setCreated(null)} />}
    </>
  )
}
