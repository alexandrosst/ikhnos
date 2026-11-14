import { Fingerprint, Mail, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Button, CopyButton, CopyValue, ErrorBanner, Field, ICON_SM, Input, Modal, PasswordInput } from '@/components/ui/primitives'
import { PasswordRequirements, passwordRules } from '@/components/PasswordRequirements'
import { useHoldReload } from '@/lib/useHoldReload'
import { api, ApiError, type ApiToken, type CreatedApiToken, type Passkey } from '@/lib/api'
import { bareIpHost, passkeysSupported } from '@/lib/webauthn'
import { useServer } from '@/store/server'

/** A scannable QR code for an otpauth:// (or any) URI, generated entirely client-side - no image request
 * leaves the browser, which matters here since the secret is embedded in the URL. `undefined` while it is
 * still being drawn or if `value` is empty; a caller shows the text fallback (already needed for apps
 * without a camera) either way. */
function useQrDataUrl(value: string): string | undefined {
  const [url, setUrl] = useState<string>()
  useEffect(() => {
    if (!value) return // initial state is already undefined; nothing to derive yet
    let live = true
    // Loaded on demand, not at module scope: everyone who opens the account menu used to pay for this
    // library in their initial bundle even though only someone setting up TOTP ever needs it.
    import('qrcode')
      .then(({ default: QRCode }) =>
        // Always render as solid black-on-white, in its own fixed-white box below (not tinted to the app's
        // own theme): some phone camera scanners are unreliable on inverted or low-contrast QR codes, and a
        // code that has to stay scannable is not the place to experiment with theme-matching colors.
        QRCode.toDataURL(value, { margin: 1, width: 176, color: { dark: '#000000ff', light: '#ffffffff' } }),
      )
      .then((u) => { if (live) setUrl(u) })
      .catch(() => { if (live) setUrl(undefined) })
    return () => {
      live = false
    }
  }, [value])
  return url
}

export function ChangePasswordModal({ onClose }: { onClose: () => void }) {
  const { changePassword, error, user } = useServer()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const meetsPolicy = passwordRules(next, user?.username ?? '').every((r) => r.ok)
  const bad = !meetsPolicy || next !== again || !current

  const submit = async () => {
    setBusy(true)
    const ok = await changePassword(current, next)
    setBusy(false)
    if (ok) setDone(true)
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Change password"
      width="max-w-md"
      footer={
        done ? (
          <Button variant="primary" onClick={onClose}>Close</Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            <Button variant="primary" onClick={submit} disabled={busy || bad}>
              {busy ? 'Saving…' : 'Change password'}
            </Button>
          </>
        )
      }
    >
      {done ? (
        <p className="text-sm text-ok">Password changed. Your other browsers were signed out.</p>
      ) : (
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (!bad) void submit() }}>
          <Field label="Current password"><PasswordInput value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" autoFocus /></Field>
          <Field label="New password"><PasswordInput value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" /></Field>
          <PasswordRequirements password={next} username={user?.username ?? ''} />
          <Field label="New password again"><PasswordInput value={again} onChange={(e) => setAgain(e.target.value)} autoComplete="new-password" /></Field>
          {error && <ErrorBanner>{error}</ErrorBanner>}
        </form>
      )}
    </Modal>
  )
}

/** Turning two-factor authentication on: generate a secret, confirm one code from it, show the recovery codes once. */
export function TwoFactorSetupModal({ onClose }: { onClose: () => void }) {
  const { setupTwoFactor, enableTwoFactor, error } = useServer()
  const [step, setStep] = useState<'loading' | 'confirm' | 'recovery'>('loading')
  const [secret, setSecret] = useState('')
  const [otpauthUrl, setOtpauthUrl] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [codes, setCodes] = useState<string[]>([])
  const qr = useQrDataUrl(otpauthUrl)

  useEffect(() => {
    void setupTwoFactor().then((r) => {
      if (r) {
        setSecret(r.secret)
        setOtpauthUrl(r.otpauthUrl)
        setStep('confirm')
      }
    })
  }, [setupTwoFactor])

  const submit = async () => {
    setBusy(true)
    const recovery = await enableTwoFactor(code)
    setBusy(false)
    if (recovery) {
      setCodes(recovery)
      setStep('recovery')
    } else {
      setCode('')
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Two-factor authentication"
      width="max-w-md"
      footer={
        step === 'recovery' ? (
          <Button variant="primary" onClick={onClose}>Done</Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            {step === 'confirm' && (
              <Button variant="primary" onClick={submit} disabled={busy || code.trim().length !== 6}>
                {busy ? 'Checking…' : 'Turn on'}
              </Button>
            )}
          </>
        )
      }
    >
      {step === 'loading' && <p className="text-sm text-nb-500">Generating a secret…</p>}
      {step === 'confirm' && (
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (code.trim().length === 6) void submit() }}>
          <p className="text-sm text-nb-400">Scan this with an authenticator app (Google Authenticator, 1Password, Authy, …), or add the key by hand, then enter the 6-digit code it shows.</p>
          {qr && (
            <div className="flex justify-center">
              <img src={qr} alt="Scan with your authenticator app" width={176} height={176} className="rounded-md border border-nb-800 bg-white p-2" data-testid="totp-qr" />
            </div>
          )}
          <Field label="Secret key">
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded-md border border-nb-800 bg-nb-925 px-3 py-2 text-sm text-nb-200" data-testid="totp-secret">{secret}</code>
              <CopyButton text={secret} />
            </div>
          </Field>
          <details className="text-xs text-nb-500">
            <summary className="cursor-pointer select-none">Or use a setup link</summary>
            <div className="mt-2 flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate rounded-md border border-nb-800 bg-nb-925 px-3 py-2 text-[11px] text-nb-400">{otpauthUrl}</code>
              <CopyButton text={otpauthUrl} />
            </div>
          </details>
          <Field label="Code from the app">
            <Input value={code} onChange={(e) => setCode(e.target.value)} autoFocus inputMode="numeric" autoComplete="one-time-code" placeholder="123456" data-testid="totp-code" />
          </Field>
          {error && <ErrorBanner>{error}</ErrorBanner>}
        </form>
      )}
      {step === 'recovery' && (
        <div className="space-y-4">
          <p className="text-sm text-ok">Two-factor authentication is on.</p>
          <p className="text-sm text-nb-400">
            Save these recovery codes somewhere safe. Each works once, in place of a code from your app, if you ever lose access to it. They will not be shown again.
          </p>
          <div className="grid grid-cols-2 gap-1.5 rounded-md border border-nb-800 bg-nb-925 p-3 font-mono text-sm text-nb-200" data-testid="recovery-codes">
            {codes.map((c) => <div key={c}>{c}</div>)}
          </div>
          <CopyButton text={codes.join('\n')} label="Copy all" />
        </div>
      )}
    </Modal>
  )
}

/** Turning two-factor authentication off: needs the current password, the same as changing it. */
export function TwoFactorDisableModal({ onClose }: { onClose: () => void }) {
  const { disableTwoFactor, error } = useServer()
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    const ok = await disableTwoFactor(password)
    setBusy(false)
    if (ok) onClose()
    else setPassword('')
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Turn off two-factor authentication"
      description="Your account will only need a password to sign in from then on."
      width="max-w-md"
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" onClick={submit} disabled={busy || !password}>{busy ? 'Turning off…' : 'Turn off'}</Button></>}
    >
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (password) void submit() }}>
        <Field label="Current password"><PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" autoFocus /></Field>
        {error && <ErrorBanner>{error}</ErrorBanner>}
      </form>
    </Modal>
  )
}

/**
 * Turning on email as a second factor. Starts on 'confirm' when an address is already on file (whether or
 * not it is verified yet - the step itself tells those two apart), otherwise 'address', so re-opening this
 * after a partial attempt never asks for an address already set. "Confirm" and "turn on" are chained under
 * one button: from here, the only reason to verify an address is to use it for codes, so there is no
 * separate step where it sits verified but still off.
 */
export function EmailSetupModal({ onClose }: { onClose: () => void }) {
  const { user, requestEmailVerification, confirmEmail, enableEmailOTP, error } = useServer()
  const [step, setStep] = useState<'address' | 'confirm' | 'done'>(user?.email ? 'confirm' : 'address')
  const [email, setEmail] = useState(user?.email ?? '')
  const [confirmedAddress, setConfirmedAddress] = useState(user?.email ?? '')
  const [alreadyVerified] = useState(!!user?.emailVerified)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [resent, setResent] = useState(false)

  const submitAddress = async () => {
    setBusy(true)
    const ok = await requestEmailVerification(email)
    setBusy(false)
    if (ok) {
      setConfirmedAddress(email)
      setStep('confirm')
    }
  }

  const resend = async () => {
    setBusy(true)
    const ok = await requestEmailVerification(confirmedAddress)
    setBusy(false)
    if (ok) setResent(true)
  }

  const submitCode = async () => {
    setBusy(true)
    // Skip straight to turning codes on when the address was already verified from an earlier attempt -
    // there is no fresh code to confirm in that case.
    const ok = alreadyVerified || (await confirmEmail(code))
    if (ok) {
      const enabled = await enableEmailOTP()
      setBusy(false)
      if (enabled) setStep('done')
      else setCode('')
    } else {
      setBusy(false)
      setCode('')
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Turn on email codes"
      width="max-w-md"
      footer={
        step === 'done' ? (
          <Button variant="primary" onClick={onClose}>Done</Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancel</Button>
            {step === 'address' && (
              <Button variant="primary" onClick={submitAddress} disabled={busy || !email.trim()}>
                {busy ? 'Sending…' : 'Send a code'}
              </Button>
            )}
            {step === 'confirm' && (
              <Button variant="primary" onClick={submitCode} disabled={busy || (!alreadyVerified && code.trim().length !== 6)}>
                {busy ? 'Checking…' : alreadyVerified ? 'Turn on' : 'Confirm & turn on'}
              </Button>
            )}
          </>
        )
      }
    >
      {step === 'address' && (
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (email.trim()) void submitAddress() }}>
          <p className="text-sm text-nb-400">We will email a code to this address whenever it is used to finish signing in.</p>
          <Field label="Email address">
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus autoComplete="email" placeholder="you@example.com" data-testid="email-address" />
          </Field>
          {error && <ErrorBanner>{error}</ErrorBanner>}
        </form>
      )}
      {step === 'confirm' && (
        <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (alreadyVerified || code.trim().length === 6) void submitCode() }}>
          {alreadyVerified ? (
            <p className="text-sm text-nb-400"><strong className="text-nb-200">{confirmedAddress}</strong> is already verified. Turn codes on to use it as a second factor.</p>
          ) : (
            <>
              <p className="text-sm text-nb-400">Enter the code we emailed to <strong className="text-nb-200">{confirmedAddress}</strong>.</p>
              <Field label="Code">
                <Input value={code} onChange={(e) => setCode(e.target.value)} autoFocus inputMode="numeric" autoComplete="one-time-code" placeholder="123456" data-testid="email-code" />
              </Field>
              <button type="button" onClick={() => void resend()} disabled={busy} className="text-xs text-accent hover:text-accent/80 disabled:opacity-50">
                {resent ? 'Code sent — send another' : 'Resend code'}
              </button>
            </>
          )}
          <div>
            <button type="button" onClick={() => setStep('address')} className="text-xs text-nb-500 hover:text-nb-300">
              Use a different address
            </button>
          </div>
          {error && <ErrorBanner>{error}</ErrorBanner>}
        </form>
      )}
      {step === 'done' && <p className="text-sm text-ok">Email codes are on. Signing in will now offer a code sent to {confirmedAddress}.</p>}
    </Modal>
  )
}

/** Turning email codes off: needs the current password, the same as TOTP. Leaves the address itself verified. */
export function EmailDisableModal({ onClose }: { onClose: () => void }) {
  const { disableEmailOTP, error } = useServer()
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    const ok = await disableEmailOTP(password)
    setBusy(false)
    if (ok) onClose()
    else setPassword('')
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Turn off email codes"
      description="Your verified address stays on file, but signing in will not offer a mailed code again until you turn it back on."
      width="max-w-md"
      footer={<><Button onClick={onClose}>Cancel</Button><Button variant="primary" onClick={submit} disabled={busy || !password}>{busy ? 'Turning off…' : 'Turn off'}</Button></>}
    >
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (password) void submit() }}>
        <Field label="Current password"><PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" autoFocus /></Field>
        {error && <ErrorBanner>{error}</ErrorBanner>}
      </form>
    </Modal>
  )
}

/** One registered passkey, either shown plainly with rename/remove buttons, or swapped for a small inline
 *  form while one of those is in progress - a passkey has no single "are you sure" dialog the way disabling
 *  two-factor entirely does, since removing one of several is much lower-stakes than turning the whole thing
 *  off, but it still needs the password check RemovePasskey requires. */
function PasskeyRow({ passkey, busy, setBusy }: { passkey: Passkey; busy: boolean; setBusy: (b: boolean) => void }) {
  const { renamePasskey, removePasskey, error } = useServer()
  const [mode, setMode] = useState<'idle' | 'rename' | 'remove'>('idle')
  const [name, setName] = useState(passkey.name)
  const [password, setPassword] = useState('')

  const saveRename = async () => {
    setBusy(true)
    const ok = await renamePasskey(passkey.id, name)
    setBusy(false)
    if (ok) setMode('idle')
  }

  const confirmRemove = async () => {
    setBusy(true)
    const ok = await removePasskey(passkey.id, password)
    setBusy(false)
    setPassword('')
    if (ok) setMode('idle')
  }

  if (mode === 'rename') {
    return (
      <li className="rounded-md border border-nb-800 bg-nb-925 p-3">
        <form className="flex items-center gap-2" onSubmit={(e) => { e.preventDefault(); if (name.trim()) void saveRename() }}>
          <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus className="h-8 flex-1 text-sm" />
          <Button type="submit" size="sm" variant="primary" disabled={busy || !name.trim()}>Save</Button>
          <Button type="button" size="sm" onClick={() => { setMode('idle'); setName(passkey.name) }}>Cancel</Button>
        </form>
        {error && <ErrorBanner className="mt-2">{error}</ErrorBanner>}
      </li>
    )
  }
  if (mode === 'remove') {
    return (
      <li className="rounded-md border border-nb-800 bg-nb-925 p-3">
        <form className="space-y-2" onSubmit={(e) => { e.preventDefault(); if (password) void confirmRemove() }}>
          <p className="text-xs text-nb-400">Enter your password to remove <strong className="text-nb-200">{passkey.name}</strong>.</p>
          <div className="flex items-center gap-2">
            <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} autoFocus autoComplete="current-password" className="h-8 flex-1 text-sm" />
            <Button type="submit" size="sm" variant="danger" disabled={busy || !password}>Remove</Button>
            <Button type="button" size="sm" onClick={() => { setMode('idle'); setPassword('') }}>Cancel</Button>
          </div>
        </form>
        {error && <ErrorBanner className="mt-2">{error}</ErrorBanner>}
      </li>
    )
  }
  return (
    <li className="flex items-center justify-between gap-2 rounded-md border border-nb-800 bg-nb-925 p-3">
      <div className="min-w-0">
        <div className="truncate text-sm text-nb-200">{passkey.name}</div>
        <div className="text-[11px] text-nb-500">
          Added {new Date(passkey.createdAt).toLocaleDateString()}
          {passkey.lastUsedAt ? `, last used ${new Date(passkey.lastUsedAt).toLocaleDateString()}` : ', never used to sign in'}
        </div>
      </div>
      <div className="flex shrink-0 gap-1">
        <Button size="sm" onClick={() => setMode('rename')}>Rename</Button>
        <Button size="sm" variant="danger" onClick={() => setMode('remove')}>Remove</Button>
      </div>
    </li>
  )
}

/**
 * Passkeys, unlike TOTP and email, have no single on/off switch to toggle - an account can hold several at
 * once (a laptop, a phone, a hardware key), so this is a small management list rather than a two-step setup
 * flow. Adding one runs the browser's own create() ceremony (see lib/webauthn) between asking for a name and
 * actually registering it.
 */
export function PasskeysModal({ onClose }: { onClose: () => void }) {
  const user = useServer((s) => s.user)
  const registerPasskey = useServer((s) => s.registerPasskey)
  const error = useServer((s) => s.error)
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const supported = passkeysSupported()
  const bareIp = bareIpHost()
  const passkeys = user?.passkeys ?? []

  const add = async () => {
    setBusy(true)
    const ok = await registerPasskey(name)
    setBusy(false)
    if (ok) {
      setAdding(false)
      setName('')
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Passkeys"
      description="A passkey or security key can finish signing in as your second factor, with no code to type."
      width="max-w-md"
      footer={<Button variant="primary" onClick={onClose}>Done</Button>}
    >
      <div className="space-y-4">
        {!supported && <ErrorBanner>Passkeys are not supported in this browser.</ErrorBanner>}
        {supported && bareIp && (
          <ErrorBanner>
            This server is reached at a bare IP address, and passkeys require a real domain name (browsers will not register one otherwise). Give it a hostname - even a private DNS
            entry or an /etc/hosts line works - or put it behind the SSO reverse-proxy setup, then come back here.
          </ErrorBanner>
        )}
        {passkeys.length === 0 && !adding && <p className="text-sm text-nb-500">No passkeys on this account yet.</p>}
        {passkeys.length > 0 && (
          <ul className="space-y-2" data-testid="passkey-list">
            {passkeys.map((p) => (
              <PasskeyRow key={p.id} passkey={p} busy={busy} setBusy={setBusy} />
            ))}
          </ul>
        )}
        {supported && !bareIp &&
          (adding ? (
            <form className="space-y-3 border-t border-nb-850 pt-4" onSubmit={(e) => { e.preventDefault(); void add() }}>
              <Field label="Name it" hint="So you can tell it apart later.">
                <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="MacBook Touch ID" data-testid="passkey-name" />
              </Field>
              <div className="flex gap-2">
                <Button type="submit" variant="primary" disabled={busy}>{busy ? 'Waiting for your passkey…' : 'Continue'}</Button>
                <Button type="button" onClick={() => setAdding(false)} disabled={busy}>Cancel</Button>
              </div>
              {error && <ErrorBanner>{error}</ErrorBanner>}
            </form>
          ) : (
            <button type="button" onClick={() => setAdding(true)} className="text-sm text-accent hover:text-accent/80" data-testid="add-passkey">
              + Add a passkey
            </button>
          ))}
      </div>
    </Modal>
  )
}

/** One issued token: its name and dates, or a small inline confirmation before it's gone for good - a
 *  token has no password check the way removing a passkey does, since revoking one of several scripts'
 *  credentials is routine, not a whole sign-in method disappearing. */
function ApiTokenRow({ token, onRevoked }: { token: ApiToken; onRevoked: () => void }) {
  const conn = useServer((s) => s.conn)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const revoke = async () => {
    const c = conn()
    if (!c) return
    setBusy(true)
    try {
      await api.revokeApiToken(c, token.id)
      onRevoked()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not revoke this token.')
      setBusy(false)
    }
  }

  return (
    <li className="rounded-md border border-nb-800 bg-nb-925 p-3">
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-sm text-nb-200">{token.name}</div>
          <div className="text-[11px] text-nb-500">
            Created {new Date(token.createdAt).toLocaleDateString()}
            {token.lastUsedAt ? `, last used ${new Date(token.lastUsedAt).toLocaleDateString()}` : ', never used'}
          </div>
        </div>
        {confirming ? (
          <div className="flex shrink-0 gap-1">
            <Button size="sm" variant="danger" disabled={busy} onClick={() => void revoke()}>{busy ? 'Revoking…' : 'Confirm'}</Button>
            <Button size="sm" onClick={() => setConfirming(false)} disabled={busy}>Cancel</Button>
          </div>
        ) : (
          <Button size="sm" variant="danger" className="shrink-0" onClick={() => setConfirming(true)}>Revoke</Button>
        )}
      </div>
      {error && <ErrorBanner className="mt-2">{error}</ErrorBanner>}
    </li>
  )
}

/**
 * Personal access tokens: long-lived secrets a script (a CI job, a curl one-liner) can use to call the
 * API without a signed-in browser, sent as `Authorization: Bearer ...` instead of the session cookie.
 * Unlike passkeys and two-factor these are not part of the session document `user` already carries, so
 * this modal loads and refreshes its own list rather than reading one off it - the same pattern
 * TeamPage uses for members and invitations. A freshly minted secret is shown once, in the same
 * "copy it now, it will not be shown again" shape as a freshly created invitation link.
 */
export function ApiTokensModal({ onClose }: { onClose: () => void }) {
  const conn = useServer((s) => s.conn)
  const [tokens, setTokens] = useState<ApiToken[]>([])
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<CreatedApiToken | null>(null)
  // While a token is on screen (shown once) or being made, Escape and a backdrop click leave it alone and the page does not reload itself
  // for a new version; Done is the way out.
  useHoldReload(created !== null)

  const load = useCallback(async () => {
    const c = conn()
    if (!c) return
    try {
      setTokens(await api.listApiTokens(c))
      setError('')
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not load your tokens.')
    } finally {
      setLoaded(true)
    }
  }, [conn])
  useEffect(() => {
    void load()
  }, [load])

  const create = async () => {
    const c = conn()
    if (!c || busy) return
    setBusy(true)
    try {
      const tok = await api.createApiToken(c, name.trim())
      setCreated(tok)
      setAdding(false)
      setName('')
      await load()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not create a token.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      dismissible={created === null && !busy}
      title="Personal API tokens"
      description="A long-lived secret for calling the API from a script or CI job, without a signed-in browser."
      width="max-w-md"
      footer={<Button variant="primary" onClick={onClose}>Done</Button>}
    >
      <div className="space-y-4">
        {created && (
          <div className="space-y-2 rounded-md border border-accent/30 bg-accent-soft p-3" data-testid="api-token-secret">
            <p className="text-xs text-nb-300">
              Copy <strong className="text-nb-200">{created.name}</strong> now - for your own safety, it won&apos;t be shown again.
            </p>
            <CopyValue value={created.token} testId="api-token-value" />
          </div>
        )}
        {error && <ErrorBanner>{error}</ErrorBanner>}
        {loaded && tokens.length === 0 && !adding && <p className="text-sm text-nb-500">No API tokens yet.</p>}
        {tokens.length > 0 && (
          <ul className="space-y-2" data-testid="api-token-list">
            {tokens.map((t) => (
              <ApiTokenRow key={t.id} token={t} onRevoked={() => { setCreated(null); void load() }} />
            ))}
          </ul>
        )}
        {adding ? (
          <form className="space-y-3 border-t border-nb-850 pt-4" onSubmit={(e) => { e.preventDefault(); void create() }}>
            <Field label="Name it" hint="So you can tell it apart later - the machine or script it's for.">
              <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="CI pipeline" data-testid="api-token-name" />
            </Field>
            <div className="flex gap-2">
              <Button type="submit" variant="primary" disabled={busy}>{busy ? 'Creating…' : 'Create'}</Button>
              <Button type="button" onClick={() => setAdding(false)} disabled={busy}>Cancel</Button>
            </div>
          </form>
        ) : (
          <button type="button" onClick={() => { setCreated(null); setAdding(true) }} className="text-sm text-accent hover:text-accent/80" data-testid="add-api-token">
            + Create a token
          </button>
        )}
      </div>
    </Modal>
  )
}

/**
 * The single entry point for "how does this account sign in with a second factor": one screen listing all
 * three methods with their current state, instead of three separately-worded menu items that each dropped
 * straight into their own setup flow (the confusing part - see TwoFactorSetupModal - was landing on a raw
 * secret key with no sense of the other choices). Picking a method closes this and opens its existing,
 * unchanged setup modal; nothing about how a method is turned on or off changes, only how a person gets there.
 */
/** One method's row in the hub above: its icon, name, current state, and the single action available on
 *  it (or none, when it is unavailable here). Module-scope, not nested in TwoFactorHubModal, so React
 *  never mistakes it for a fresh component type on every render. */
function TwoFactorMethodRow({
  icon, title, status, on, action, note,
}: { icon: ReactNode; title: string; status: string; on?: boolean; action: { label: string; onClick: () => void } | null; note?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-md border border-nb-800 bg-nb-925 p-3">
      <div className="flex min-w-0 gap-2.5">
        <span className="mt-0.5 shrink-0 text-nb-500" aria-hidden>{icon}</span>
        <div className="min-w-0">
          <div className="text-sm font-medium text-nb-200">{title}</div>
          <div className={`text-xs ${on ? 'text-ok' : 'text-nb-500'}`}>{status}</div>
          {note && <div className="mt-1 text-xs text-nb-500">{note}</div>}
        </div>
      </div>
      {action && <Button size="sm" className="shrink-0" onClick={action.onClick}>{action.label}</Button>}
    </div>
  )
}

export function TwoFactorHubModal({ onClose, open2FA, openEmail, openPasskeys }: { onClose: () => void; open2FA: () => void; openEmail: () => void; openPasskeys: () => void }) {
  const user = useServer((s) => s.user)
  const supported = passkeysSupported()
  const bareIp = bareIpHost()
  if (!user) return null
  const passkeyCount = user.passkeys.length

  return (
    <Modal
      open
      onClose={onClose}
      title="Two-factor authentication"
      description="A second factor for signing in. Set up one method, or several - any one of them is enough."
      width="max-w-md"
      footer={<Button variant="primary" onClick={onClose}>Done</Button>}
    >
      <div className="space-y-2.5">
        <TwoFactorMethodRow
          icon={<ShieldCheck size={ICON_SM} />}
          title="Authenticator app"
          status={user.twoFactorEnabled ? 'On' : 'Off - a code from an app like 1Password or Google Authenticator'}
          on={user.twoFactorEnabled}
          action={{ label: user.twoFactorEnabled ? 'Turn off' : 'Turn on', onClick: () => { onClose(); open2FA() } }}
        />
        <TwoFactorMethodRow
          icon={<Mail size={ICON_SM} />}
          title="Email code"
          status={!user.mailConfigured ? 'Not available' : user.emailOtpEnabled ? 'On' : 'Off - a code sent to your address'}
          on={user.mailConfigured && user.emailOtpEnabled}
          action={user.mailConfigured ? { label: user.emailOtpEnabled ? 'Turn off' : 'Turn on', onClick: () => { onClose(); openEmail() } } : null}
          note={!user.mailConfigured && (
            <>This server has no outgoing mail set up yet. <Link to="/settings" className="text-accent hover:underline" onClick={onClose}>Set it up in Settings</Link>.</>
          )}
        />
        <TwoFactorMethodRow
          icon={<Fingerprint size={ICON_SM} />}
          title="Passkey"
          status={!supported ? 'Not supported in this browser' : bareIp ? 'Not available' : passkeyCount > 0 ? `${passkeyCount} added - no code to type` : 'None added - no code to type'}
          on={passkeyCount > 0}
          action={supported && !bareIp ? { label: passkeyCount > 0 ? 'Manage' : 'Add', onClick: () => { onClose(); openPasskeys() } } : null}
          note={supported && bareIp && 'Needs a real domain name - this server is currently reached at a bare IP address.'}
        />
      </div>
    </Modal>
  )
}

/**
 * The dialogs behind a person's own sign-in: password, the second step (an authenticator app, an email code, passkeys) and personal API tokens.
 * One place holds which is open, so Settings and the two-factor reminder in the sidebar open the very same ones.
 */
export function useSecurityDialogs() {
  const user = useServer((s) => s.user)
  const [pw, setPw] = useState(false)
  const [hub, setHub] = useState(false)
  const [twoFA, setTwoFA] = useState(false)
  const [emailOTP, setEmailOTP] = useState(false)
  const [passkeys, setPasskeys] = useState(false)
  const [apiTokens, setApiTokens] = useState(false)
  const dialogs = user ? (
    <>
      {pw && <ChangePasswordModal onClose={() => setPw(false)} />}
      {hub && (
        <TwoFactorHubModal
          onClose={() => setHub(false)}
          open2FA={() => setTwoFA(true)}
          openEmail={() => setEmailOTP(true)}
          openPasskeys={() => setPasskeys(true)}
        />
      )}
      {twoFA && (user.twoFactorEnabled ? <TwoFactorDisableModal onClose={() => setTwoFA(false)} /> : <TwoFactorSetupModal onClose={() => setTwoFA(false)} />)}
      {emailOTP && (user.emailOtpEnabled ? <EmailDisableModal onClose={() => setEmailOTP(false)} /> : <EmailSetupModal onClose={() => setEmailOTP(false)} />)}
      {passkeys && <PasskeysModal onClose={() => setPasskeys(false)} />}
      {apiTokens && <ApiTokensModal onClose={() => setApiTokens(false)} />}
    </>
  ) : null
  return { dialogs, open: { password: () => setPw(true), twoFactor: () => setHub(true), tokens: () => setApiTokens(true) } }
}
