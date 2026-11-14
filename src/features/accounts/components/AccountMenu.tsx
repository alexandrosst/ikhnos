import { ChevronsUpDown, KeyRound, LogOut, Monitor, Moon, Plus, ScrollText, ShieldCheck, Sun, Terminal, Ticket, Users, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { Button, ErrorBanner, Field, ICON_MD, ICON_SM, Input, Modal, Select } from '@/components/ui/primitives'
import { useSecurityDialogs } from '@/components/SecurityModals'
import { atLeast, ROLE_LABEL } from '@/lib/api'
import { getThemePreference, setThemePreference, type ThemePreference } from '@/lib/theme'
import { useServer } from '@/store/server'
import { useWorkspace, type SyncStatus } from '@/store/workspace'

const SYNC_LABEL: Record<SyncStatus, { text: string; tone: string }> = {
  off: { text: '', tone: '' },
  loading: { text: 'Loading workspace…', tone: 'text-nb-500' },
  saved: { text: 'All changes saved', tone: 'text-ok' },
  dirty: { text: 'Unsaved changes…', tone: 'text-warn' },
  saving: { text: 'Saving…', tone: 'text-nb-400' },
  conflict: { text: 'Changed elsewhere', tone: 'text-bad' },
  error: { text: 'Could not save. Retrying…', tone: 'text-bad' },
  readonly: { text: 'Read-only access', tone: 'text-nb-400' },
  choose: { text: 'Waiting for your choice', tone: 'text-warn' },
}

function NewOrgModal({ onClose }: { onClose: () => void }) {
  const createOrg = useServer((s) => s.createOrg)
  const error = useServer((s) => s.error)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    const ok = await createOrg(name)
    setBusy(false)
    if (ok) onClose()
  }
  return (
    <Modal
      open
      onClose={onClose}
      title="New organisation"
      description="A separate, private topology with its own clusters, history and members. You will be its owner."
      width="max-w-md"
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" onClick={submit} disabled={busy || name.trim().length < 2}>Create</Button></>}
    >
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (name.trim().length >= 2) void submit() }}>
        <Field label="Name"><Input value={name} onChange={(e) => setName(e.target.value)} autoFocus data-testid="new-org-name" /></Field>
        {error && <ErrorBanner>{error}</ErrorBanner>}
      </form>
    </Modal>
  )
}

function JoinModal({ onClose }: { onClose: () => void }) {
  const join = useServer((s) => s.joinWithInvite)
  const [code, setCode] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    setBusy(true)
    const m = await join(code)
    setBusy(false)
    if (m) setMsg(m)
    else onClose()
  }
  return (
    <Modal
      open
      onClose={onClose}
      title="Join with an invitation"
      description="Paste the code an administrator of the organisation gave you."
      width="max-w-md"
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" onClick={submit} disabled={busy || !code.trim()}>Join</Button></>}
    >
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (code.trim()) void submit() }}>
        <Field label="Invitation code"><Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="cni_…" spellCheck={false} autoFocus data-testid="join-code" /></Field>
        {msg && <ErrorBanner>{msg}</ErrorBanner>}
      </form>
    </Modal>
  )
}

/** Bottom of the sidebar: who is signed in and where, whether their work is saved, and the account actions. */
// twoFactorNudgeKey namespaces the "don't ask again" flag per account, in this browser only: dismissing it
// signed in as one person on one machine should not silence it for anyone else, and a shared machine with
// several accounts should not have one account's dismissal hide the nudge for the next person who signs in.
const twoFactorNudgeKey = (userId: string) => `ikhnos:2fa-nudge-dismissed:${userId}`

export default function AccountMenu() {
  const user = useServer((s) => s.user)
  const status = useServer((s) => s.status)
  const orgs = useServer((s) => s.orgs)
  const orgId = useServer((s) => s.orgId)
  const role = useServer((s) => s.role)
  const registration = useServer((s) => s.registration)
  const signOut = useServer((s) => s.signOut)
  const selectOrg = useServer((s) => s.selectOrg)
  const sync = useWorkspace((s) => s.status)
  const security = useSecurityDialogs()
  const [newOrg, setNewOrg] = useState(false)
  const [join, setJoin] = useState(false)
  const [open, setOpen] = useState(false)
  const [theme, setTheme] = useState<ThemePreference>(getThemePreference)
  // Nudges to set up a second sign-in step: shown from first login onward (there is nothing to have
  // dismissed yet the first time) until either a method is turned on or this account dismisses it on this
  // browser - a light, ongoing reminder rather than a one-shot that is easy to miss and never see again.
  // Read directly during render (a plain, synchronous localStorage lookup, not a subscription to anything
  // external) rather than mirrored into state through an effect; `dismissedNow` layers today's own dismiss
  // click on top without waiting for a re-render to see it written back.
  const userId = user?.id
  const storedDismissed = useMemo(() => {
    if (!userId) return false
    try { return localStorage.getItem(twoFactorNudgeKey(userId)) === '1' } catch { return false }
  }, [userId])
  const [dismissedNow, setDismissedNow] = useState(false)
  const nudgeDismissed = storedDismissed || dismissedNow
  const { pathname } = useLocation()
  // close the menu when the page changes (adjusting state while rendering, not in an effect)
  const [seenPath, setSeenPath] = useState(pathname)
  if (seenPath !== pathname) { setSeenPath(pathname); setOpen(false) }
  const button = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const items = () => [...(menu.current?.querySelectorAll<HTMLElement>('[role=menuitem]') ?? [])]
    // A menu takes focus when it opens, moves with the arrow keys, and hands focus back to its button when it closes.
    items()[0]?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false)
        button.current?.focus()
      } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Home' || e.key === 'End') {
        const list = items()
        if (!list.length) return
        e.preventDefault()
        const at = list.indexOf(document.activeElement as HTMLElement)
        const next = e.key === 'Home' ? 0 : e.key === 'End' ? list.length - 1 : (at + (e.key === 'ArrowDown' ? 1 : -1) + list.length) % list.length
        list[next].focus()
      }
    }
    // Same pattern as Select's outside-click close (primitives.tsx): a document-level listener checked
    // against the trigger and menu refs, rather than a full-screen click-catcher element. The sidebar this
    // menu lives in is always transformed (translate-x-0/-full for the mobile slide-in), and any non-none
    // transform on an ancestor turns it into the containing block for `position: fixed` descendants - so a
    // `fixed inset-0` overlay here would only ever cover the sidebar's own ~240px, never the rest of the
    // page, and clicking anywhere outside the sidebar would never reach it at all.
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (!button.current?.contains(t) && !menu.current?.contains(t)) setOpen(false)
    }
    window.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDown)
    return () => {
      window.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDown)
    }
  }, [open])
  if (status !== 'connected' || !user) return null
  const label = SYNC_LABEL[sync]
  const item = 'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm text-nb-300 hover:bg-nb-940'
  const twoFactorMethodsOn = [user.twoFactorEnabled, user.emailOtpEnabled, user.passkeys.length > 0].filter(Boolean).length
  const showTwoFactorNudge = twoFactorMethodsOn === 0 && !nudgeDismissed
  const dismissTwoFactorNudge = () => {
    setDismissedNow(true)
    try { localStorage.setItem(twoFactorNudgeKey(user.id), '1') } catch { /* a private window or blocked storage just means it asks again next time */ }
  }
  const pick = (fn: () => void) => () => {
    setOpen(false)
    fn()
  }

  return (
    <div className="relative mb-2" data-testid="account">
      {showTwoFactorNudge && (
        <div className="mb-2 flex items-start gap-2 rounded-lg border border-warn/30 bg-warn-soft p-2.5 text-xs text-nb-300" data-testid="two-factor-nudge">
          <ShieldCheck size={ICON_SM} className="mt-0.5 shrink-0 text-warn" aria-hidden />
          <p className="flex-1">
            Add a second sign-in step so a leaked password alone can't get in.{' '}
            <button type="button" className="font-medium underline hover:text-warn" onClick={security.open.twoFactor} data-testid="two-factor-nudge-setup">Set up now</button>
          </p>
          <button type="button" aria-label="Dismiss" className="shrink-0 text-warn/70 hover:text-warn" onClick={dismissTwoFactorNudge} data-testid="two-factor-nudge-dismiss">
            <X size={ICON_MD} />
          </button>
        </div>
      )}
      {open && (
        <div ref={menu} role="menu" aria-label="Account" className="absolute inset-x-0 bottom-full z-20 mb-2 rounded-lg border border-nb-850 bg-nb-920 p-1 shadow-xl" data-testid="account-menu">
            <div className="px-2.5 pb-1 pt-1 text-[11px] font-medium uppercase tracking-wide text-nb-600" aria-hidden>Organisation</div>
            {orgId && (
              <NavLink to="/team" className={item} role="menuitem" data-testid="nav-team">
                <Users size={ICON_SM} className="text-nb-500" /> Members &amp; access
              </NavLink>
            )}
            {orgId && atLeast(role, 'admin') && (
              <NavLink to="/activity" className={item} role="menuitem" data-testid="nav-activity">
                <ScrollText size={ICON_SM} className="text-nb-500" /> Who did what
              </NavLink>
            )}
            {registration === 'open' && (
              <button onClick={pick(() => setNewOrg(true))} className={item} role="menuitem"><Plus size={ICON_SM} className="text-nb-500" /> New organisation</button>
            )}
            <button onClick={pick(() => setJoin(true))} className={item} role="menuitem" data-testid="join-open"><Ticket size={ICON_SM} className="text-nb-500" /> Join with a code</button>
            <div className="my-1 border-t border-nb-850" />
            <div className="px-2.5 pb-1 pt-1 text-[11px] font-medium uppercase tracking-wide text-nb-600" aria-hidden>Account &amp; security</div>
            <button onClick={pick(security.open.password)} className={item} role="menuitem"><KeyRound size={ICON_SM} className="text-nb-500" /> Change password</button>
            <button onClick={pick(security.open.twoFactor)} className={item} role="menuitem" data-testid="two-factor-open">
              <ShieldCheck size={ICON_SM} className="text-nb-500" />
              Two-factor authentication
              {twoFactorMethodsOn > 0 && <span className="ml-auto text-xs text-nb-500">{twoFactorMethodsOn} on</span>}
            </button>
            <button onClick={pick(security.open.tokens)} className={item} role="menuitem" data-testid="api-tokens-open">
              <Terminal size={ICON_SM} className="text-nb-500" /> Personal API tokens
            </button>
            <div className="my-1 border-t border-nb-850" />
            <div className="px-2.5 pb-1 pt-1 text-[11px] font-medium uppercase tracking-wide text-nb-600" aria-hidden>Appearance</div>
            <div className="flex gap-1 px-2.5 pb-1.5" role="radiogroup" aria-label="Theme">
              {(
                [
                  { value: 'system' as const, label: 'System', icon: Monitor },
                  { value: 'light' as const, label: 'Light', icon: Sun },
                  { value: 'dark' as const, label: 'Dark', icon: Moon },
                ]
              ).map(({ value, label, icon: Icon }) => (
                <button
                  key={value}
                  type="button"
                  role="radio"
                  aria-checked={theme === value}
                  onClick={() => { setThemePreference(value); setTheme(value) }}
                  className={`flex flex-1 flex-col items-center gap-1 rounded-md border py-1.5 text-xs ${theme === value ? 'border-accent/60 bg-accent-soft text-nb-300' : 'border-nb-850 text-nb-500 hover:bg-nb-940 hover:text-nb-300'}`}
                  data-testid={`theme-${value}`}
                >
                  <Icon size={ICON_SM} />
                  {label}
                </button>
              ))}
            </div>
            <div className="my-1 border-t border-nb-850" />
            <button onClick={pick(() => void signOut())} className={item} role="menuitem" data-testid="sign-out"><LogOut size={ICON_SM} className="text-nb-500" /> Sign out</button>
        </div>
      )}
      <div className="rounded-lg border border-nb-850 bg-nb-925 p-2">
        <button
          ref={button}
          onClick={() => setOpen((o) => !o)}
          aria-haspopup="menu"
          aria-expanded={open}
          aria-label="Account menu"
          className="flex w-full items-center gap-2.5 rounded-md p-1 text-left hover:bg-nb-930"
          data-testid="account-button"
        >
          <span className="grid size-8 shrink-0 place-items-center rounded-full bg-accent-soft text-sm font-semibold uppercase text-accent" aria-hidden>{user.username.slice(0, 1)}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium text-nb-300" data-testid="account-name">{user.username}</span>
            <span className="block text-[11px] text-nb-500" data-testid="account-role">{role ? ROLE_LABEL[role] : 'No organisation'}</span>
          </span>
          <ChevronsUpDown size={ICON_SM} className="shrink-0 text-nb-500" aria-hidden />
        </button>
        {orgs.length > 0 && (
          <div className="mt-2">
            <Select value={orgId ?? ''} onChange={(e) => void selectOrg(e.target.value)} aria-label="Organisation" className="h-8 text-xs" data-testid="org-switch">
              {orgs.map((o) => (
                <option key={o.id} value={o.id}>{o.name}</option>
              ))}
            </Select>
          </div>
        )}
        {label.text && <div className={`mt-2 px-1 text-[11px] ${label.tone}`} role="status" data-testid="sync-status">{label.text}</div>}
      </div>
      {security.dialogs}
      {newOrg && <NewOrgModal onClose={() => setNewOrg(false)} />}
      {join && <JoinModal onClose={() => setJoin(false)} />}
    </div>
  )
}
