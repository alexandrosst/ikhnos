import { BookOpen, Plus } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { CopyCommand } from '@/components/ui/CopyCommand'
import { ConfirmModal } from '@/components/forms'
import { FusionSection, ROWS } from '@/features/fusion/components/FusionSection'
import { buttonClass } from '@/components/ui/buttonClass'
import { Button, CheckboxList, CopyValue, EmptyState, ErrorBanner, Field, ICON_SM, Input, Modal, Pill, Select, TagsInput } from '@/components/ui/primitives'
import { shArg, shQuote } from '@/lib/install'
import { useHoldReload } from '@/lib/useHoldReload'
import { api, ApiError, type CreatedFusionAccessToken, type FusionAccessToken, type FusionApplication, type FusionSignal } from '@/lib/api'
import { ago } from '@/lib/observed'
import { useServer } from '@/store/server'

const SIGNALS: { value: FusionSignal; label: string; hint: string }[] = [
  { value: 'metrics', label: 'Metrics', hint: 'Prometheus - series over time' },
  { value: 'logs', label: 'Logs', hint: 'Loki - log lines, with their trace and span ids' },
  { value: 'traces', label: 'Traces', hint: 'Tempo - spans, and the trace joined to its logs and metrics' },
]

/** Where the data API's reference page is: the same server, so the same address the rest of the app talks to. It needs no sign-in (it only describes the API). */
export function fusionDocsUrl(base: string): string {
  return `${base.replace(/\/+$/, '')}/api/v1/fusion/docs`
}

const EXPIRY_DAYS = [30, 90, 180, 365]

/** Whether the address is one a certificate from a public authority is not issued for: an IP address or localhost. A server reached that way is on
 *  its own (self-signed) certificate, the chart's default, which curl refuses. */
export function selfSignedLikely(base: string): boolean {
  try {
    const host = new URL(base).hostname.toLowerCase()
    return host === 'localhost' || host.endsWith('.localhost') || /^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.startsWith('[')
  } catch {
    return false
  }
}

/** The curl line that tries a freshly minted token. The header keeps its double quotes while the token is plain (the usual case); anything
 *  else in it is single-quoted, so what the server hands out can never end the line early. At an address a certificate is not issued for (see
 *  `selfSignedLikely`) it carries -k, since the line would otherwise fail the first time it is run. */
export function fusionStatusCurl(token: string, base: string): string {
  const header = `Authorization: Bearer ${token}`
  return `curl ${selfSignedLikely(base) ? '-k ' : ''}-H ${/^[A-Za-z0-9_.~+/=: -]+$/.test(header) ? `"${header}"` : shQuote(header)} ${shArg(`${base}/api/v1/fusion/status`)}`
}

/** "expires in 12 days", "expires today", "expired", from the token's own expiry time. */
export function expiryText(iso: string, now = Date.now()): string {
  const ms = new Date(iso).getTime() - now
  if (ms <= 0) return 'expired'
  const days = Math.ceil(ms / 86_400_000)
  return days <= 1 ? 'expires today' : `expires in ${days} days`
}

/** The tooltip of the mark on a token whose maker is no longer an administrator here (`creator` is "gone"). The token itself is
 *  unchanged: it belongs to the organisation, so the mark only says that nobody who looks after it any more made it. */
export function creatorGoneHint(t: FusionAccessToken): string {
  return `Made by ${t.createdBy || 'someone'}, who is no longer an administrator of this organisation (removed, given a lower role, or their account disabled or deleted). It keeps working: a token belongs to the organisation. Revoke it if nothing needs it any more.`
}

/** The tooltip of an application a token is limited to that no longer exists in ikhnos. */
export const APP_GONE_HINT = 'This application no longer exists in ikhnos, so it gives the token nothing. A token none of whose applications is left sees nothing.'

/** What the form says about limiting a token to applications: what it then sees, and that it follows the applications as they are edited. */
export const APPLICATIONS_HINT =
  'Optionally limit it to these applications: it then sees only what the services listed below send, each in the namespace and cluster it runs in now. ' +
  'It is made with exactly those services. Editing an application later can take services from it (one that leaves or moves), never add one: to widen it, make a new token. Such a token cannot send PromQL, LogQL or TraceQL of its own.'

/** A service a token would be, or was, made with: by id, in its application, where it runs. */
export type TokenMember = { application: string; id: string; name: string; kind?: string; namespace: string; cluster: string }

/** The services the picked applications give a new token, as the server pins them: each with an id, a name, a namespace and a
 *  cluster, within the token's namespaces and clusters (empty: every one), in the order of the applications and their services. */
export function previewMembers(apps: FusionApplication[], picked: string[], namespaces: string[], clusters: string[]): TokenMember[] {
  const out: TokenMember[] = []
  for (const a of apps) {
    if (!picked.includes(a.id)) continue
    for (const m of a.services) {
      if (!m.id || !m.name || !m.namespace || !m.cluster) continue
      if (namespaces.length > 0 && !namespaces.includes(m.namespace)) continue
      if (clusters.length > 0 && !clusters.includes(m.cluster)) continue
      out.push({ application: a.id, id: m.id, name: m.name, kind: m.kind, namespace: m.namespace, cluster: m.cluster })
    }
  }
  return out
}

/** One service as the form and the list name it. */
export function memberText(m: TokenMember): string {
  return `${m.name}${m.kind ? ` (${m.kind})` : ''} in ${m.namespace} / ${m.cluster}`
}

/** "3 services in 2 places", for the summary of a token's members. */
export function membersSummary(members: TokenMember[]): string {
  const places = new Set(members.map((m) => `${m.namespace}/${m.cluster}`)).size
  return `${members.length} service${members.length === 1 ? '' : 's'} in ${places} place${places === 1 ? '' : 's'}`
}

/** The applications a token is limited to, as the list shows them: each by its name now, or by its id when it is gone (or the names could not be read). */
export function tokenApplications(t: FusionAccessToken): { key: string; label: string; gone: boolean }[] {
  return (t.applications ?? []).map((a) => ({ key: a.id, label: a.gone ? `${a.id} (gone)` : a.name || a.id, gone: a.gone === true }))
}

function scopeChips(t: FusionAccessToken) {
  const signals = t.signals.length === SIGNALS.length ? 'all signals' : t.signals.join(' + ')
  return [signals, t.namespaces.length ? `namespaces: ${t.namespaces.join(', ')}` : 'all namespaces', ...(t.clusters.length ? [`clusters: ${t.clusters.join(', ')}`] : [])]
}

/**
 * Who else may read what FUSION saved: the API tokens the shared data API accepts. A token is read-only, limited to
 * the signal types and namespaces it is given, and expires; its secret is shown once, when it is made. Shown on the
 * Access tab of the server's main organisation, whatever state the switch is in (the API says plainly when the stores
 * are off).
 */
export function FusionAccess() {
  const conn = useServer((s) => s.conn)
  const [tokens, setTokens] = useState<FusionAccessToken[] | null>(null)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState<FusionAccessToken | null>(null)
  const alive = useRef(true)
  // Set back to true on every mount, not only cleared on unmount: React's StrictMode (main.tsx) mounts, unmounts and mounts again, and a flag only
  // ever cleared would stay false for the real mount, so the token list would never be filled in.
  useEffect(() => {
    alive.current = true
    return () => { alive.current = false }
  }, [])

  const load = useCallback(async () => {
    const c = conn()
    if (!c) return
    try {
      const list = await api.listFusionTokens(c)
      if (alive.current) {
        setTokens(list)
        setError('')
      }
    } catch (e) {
      if (alive.current) setError(e instanceof ApiError ? e.message : 'Could not load the API tokens.')
    }
  }, [conn])
  useEffect(() => {
    void load()
  }, [load])

  const revoke = async (t: FusionAccessToken) => {
    const c = conn()
    if (!c) return
    try {
      await api.revokeFusionToken(c, t.id)
      await load()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not revoke the token.')
    }
  }

  return (
    <FusionSection
      title="API tokens"
      description="Another system reads what FUSION saved with a token: read-only, limited to the signals, namespaces and applications you give it, and expiring. The stores are never exposed; every read goes through this server."
      testId="fusion-access"
      actions={
        <>
          <a className={buttonClass('secondary')} href={fusionDocsUrl(conn()?.url || window.location.origin)} target="_blank" rel="noreferrer" data-testid="fusion-api-docs">
            <BookOpen size={ICON_SM} aria-hidden /> API reference
          </a>
          <Button variant="primary" onClick={() => setCreating(true)} data-testid="fusion-token-new">
            <Plus size={ICON_SM} aria-hidden /> New API token
          </Button>
        </>
      }
    >
      {error && <ErrorBanner className="mb-3">{error}</ErrorBanner>}
      {tokens && tokens.length === 0 && !error && <EmptyState data-testid="fusion-token-empty" description="No API tokens yet." />}
      {tokens && tokens.length > 0 && (
        <ul className={ROWS} data-testid="fusion-token-list">
          {tokens.map((t) => (
            <li key={t.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3" data-testid={`fusion-token-${t.id}`}>
              <div className="min-w-0 flex-1">
                <div className="truncate text-nb-300">{t.name}</div>
                <div className="mt-1.5 flex flex-wrap gap-1">
                  {scopeChips(t).map((c) => <Pill key={c} className="text-xs text-nb-400">{c}</Pill>)}
                  {tokenApplications(t).map((a) => (
                    <span key={`app-${a.key}`} className="inline-flex" data-testid={`fusion-token-app-${a.key}`}>
                      <Pill title={a.gone ? APP_GONE_HINT : undefined} className={a.gone ? 'text-xs text-warn' : 'text-xs text-nb-400'}>{`application: ${a.label}`}</Pill>
                    </span>
                  ))}
                  {t.creator === 'gone' && <Pill title={creatorGoneHint(t)} className="text-xs text-warn">creator no longer an admin</Pill>}
                </div>
                {(t.applicationMembers ?? []).length > 0 && (
                  <details className="mt-1.5 text-xs text-nb-400" data-testid={`fusion-token-members-${t.id}`}>
                    <summary className="cursor-pointer text-nb-500" title="The services it was made with. It never sees more; make a new token to widen it.">
                      {`made with ${membersSummary(t.applicationMembers)}`}
                    </summary>
                    <ul className="mt-1 space-y-0.5 pl-3">
                      {t.applicationMembers.map((m) => <li key={`${m.application}/${m.id}`}>{memberText(m)}</li>)}
                    </ul>
                  </details>
                )}
                <div className="mt-1.5 text-xs text-nb-500">
                  {expiryText(t.expiresAt)}, {t.lastUsedAt ? `last used ${ago(t.lastUsedAt)}` : 'never used'}
                </div>
              </div>
              <Button size="sm" variant="danger" onClick={() => setRevoking(t)} aria-label={`Revoke ${t.name}`}>Revoke</Button>
            </li>
          ))}
        </ul>
      )}
      {creating && <NewTokenModal onClose={() => setCreating(false)} onCreated={() => void load()} />}
      {revoking && (
        <ConfirmModal
          title={`Revoke ${revoking.name}?`}
          message={`${revoking.lastUsedAt ? `It was last used ${ago(revoking.lastUsedAt)}.` : 'It has never been used.'} Whatever uses this token stops being able to read FUSION at once. This cannot be undone; make a new token to give it access again.`}
          confirmLabel="Revoke"
          onConfirm={() => void revoke(revoking)}
          onClose={() => setRevoking(null)}
        />
      )}
    </FusionSection>
  )
}

function NewTokenModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const conn = useServer((s) => s.conn)
  const [name, setName] = useState('')
  const [signals, setSignals] = useState<FusionSignal[]>(SIGNALS.map((s) => s.value))
  const [namespaces, setNamespaces] = useState<string[]>([])
  const [clusters, setClusters] = useState<string[]>([])
  const [applications, setApplications] = useState<string[]>([])
  // The organisation's applications, as the data API knows them (the ids a token is checked against); null while they load.
  const [known, setKnown] = useState<FusionApplication[] | null>(null)
  const [knownError, setKnownError] = useState('')
  const [days, setDays] = useState('90')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [created, setCreated] = useState<CreatedFusionAccessToken | null>(null)
  // The token is shown once: while it is on screen nothing may reload the page under it (see the created view's Modal).
  useHoldReload(created !== null)
  const preview = previewMembers(known ?? [], applications, namespaces, clusters)
  const empty = known ? applications.filter((id) => !preview.some((m) => m.application === id)) : []

  useEffect(() => {
    const c = conn()
    if (!c) return
    let live = true
    api.getFusionApplications(c).then(
      (doc) => { if (live) setKnown(doc.applications) },
      (e) => { if (live) setKnownError(e instanceof ApiError ? e.message : 'Could not load the applications.') },
    )
    return () => { live = false }
  }, [conn])

  const create = async () => {
    const c = conn()
    if (!c || busy) return
    setBusy(true)
    setError('')
    try {
      // The services shown are the ones approved: the token is pinned to those alone, and refused if the applications changed.
      const approved = applications.length ? { applications, members: [...new Set(preview.map((m) => m.id))] } : {}
      setCreated(await api.createFusionToken(c, { name: name.trim(), signals, namespaces, clusters, ...approved, expiresInDays: Number(days) }))
      onCreated()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not create the token.')
    } finally {
      setBusy(false)
    }
  }

  if (created) {
    const base = conn()?.url || window.location.origin
    return (
      <Modal open onClose={onClose} dismissible={false} title="API token created" width="max-w-lg" footer={<Button variant="primary" onClick={onClose} data-testid="fusion-token-done">Done</Button>}>
        <div className="space-y-3">
          <div className="space-y-2 rounded-md border border-accent/30 bg-accent-soft p-3" data-testid="fusion-token-secret">
            <p className="text-xs text-nb-300">
              Copy <strong className="text-nb-200">{created.details.name}</strong> now - for your own safety, it won&apos;t be shown again.
            </p>
            <CopyValue value={created.token} testId="fusion-token-value" />
          </div>
          <div>
            <p className="text-xs text-nb-500">Try it:</p>
            <CopyCommand text={fusionStatusCurl(created.token, base)} testId="fusion-token-curl" />
            {base.startsWith('https://') && (
              // The default install serves a self-signed certificate, which curl refuses: the line carries -k where that is likely, and says what it does.
              <p className="mt-1.5 text-xs leading-relaxed text-nb-500" data-testid="fusion-token-selfsigned">
                {selfSignedLikely(base) ? (
                  <>
                    <code className="font-mono text-nb-400">-k</code> skips checking the server&apos;s certificate, which is self-signed unless you installed one. To check it, drop{' '}
                    <code className="font-mono text-nb-400">-k</code> and add <code className="font-mono text-nb-400">--cacert &lt;file&gt;</code> with that certificate.
                  </>
                ) : (
                  <>
                    If curl says the certificate is self-signed (the default install), add <code className="font-mono text-nb-400">-k</code> to try it, or{' '}
                    <code className="font-mono text-nb-400">--cacert &lt;file&gt;</code> with the server&apos;s certificate to check it.
                  </>
                )}
              </p>
            )}
          </div>
          <p className="text-xs leading-relaxed text-nb-500">
            {expiryText(created.details.expiresAt)}. Read a trace with its logs and metrics joined:{' '}
            <code className="font-mono text-nb-400">/api/v1/fusion/traces/&lt;trace-id&gt;?fused=true</code>.{' '}
            <a className="text-accent underline" href={fusionDocsUrl(base)} target="_blank" rel="noreferrer" data-testid="fusion-token-docs">Every call is in the API reference</a>.
          </p>
        </div>
      </Modal>
    )
  }

  return (
    <Modal
      open
      onClose={onClose}
      // Once the request is out the token exists and is on its way to this dialog: closing now would lose it.
      dismissible={!busy}
      title="New API token"
      description="A read-only credential for another system. It sees only what you allow here."
      width="max-w-lg"
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>Cancel</Button>
          <Button variant="primary" onClick={() => void create()} disabled={busy || name.trim() === '' || signals.length === 0 || empty.length > 0} data-testid="fusion-token-create">
            {busy ? 'Creating…' : 'Create token'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Name" hint="The system or script it is for, so you can tell tokens apart later.">
          <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="Decision engine" maxLength={64} data-testid="fusion-token-name" />
        </Field>
        <Field label="Can read">
          <CheckboxList options={SIGNALS} value={signals} onChange={(v) => setSignals(SIGNALS.map((s) => s.value).filter((s) => v.includes(s)))} />
        </Field>
        <Field label="Namespaces" hint="Only telemetry from these namespaces is visible to it. Empty: every namespace. Telemetry with no namespace (node metrics) is then hidden.">
          <TagsInput value={namespaces} onChange={setNamespaces} placeholder="shop, payments" data-testid="fusion-token-namespaces" />
        </Field>
        <Field label="Clusters" hint="Optionally limit it to these cluster ids too. Empty: every cluster.">
          <TagsInput value={clusters} onChange={setClusters} placeholder="cl-1a2b3c4d5e" data-testid="fusion-token-clusters" />
        </Field>
        <Field label="Applications" hint={APPLICATIONS_HINT}>
          {knownError ? (
            <p className="text-xs text-nb-500" data-testid="fusion-token-apps-error">The applications could not be loaded ({knownError}), so the token cannot be limited to them now.</p>
          ) : known === null ? (
            <p className="text-xs text-nb-500">Loading the applications…</p>
          ) : (
            <div data-testid="fusion-token-apps">
              <CheckboxList
                options={known.map((a) => ({ value: a.id, label: a.name, hint: [a.namespaces.join(', '), `${a.services.length} service${a.services.length === 1 ? '' : 's'}`].filter(Boolean).join(' · ') }))}
                value={applications}
                onChange={(v) => setApplications(known.map((a) => a.id).filter((id) => v.includes(id)))}
                emptyLabel="There are no applications in ikhnos yet."
              />
            </div>
          )}
        </Field>
        <Field label="Expires">
          <Select value={days} onChange={(e) => setDays(e.target.value)} data-testid="fusion-token-days">
            {EXPIRY_DAYS.map((d) => <option key={d} value={String(d)}>{`After ${d} days`}</option>)}
          </Select>
        </Field>
        {applications.length > 0 && known && (
          <div className="space-y-1.5 rounded-md border border-nb-800 p-3 text-xs" data-testid="fusion-token-preview">
            <p className="text-nb-300">{`It will be made with these ${membersSummary(preview)}, and never see more:`}</p>
            {known.filter((a) => applications.includes(a.id)).map((a) => (
              <div key={a.id} data-testid={`fusion-token-preview-${a.id}`}>
                <div className="text-nb-400">{a.name}</div>
                {empty.includes(a.id) ? (
                  <p className="pl-3 text-warn">No service of it runs in a namespace and cluster ikhnos knows within the limits above, so it would give the token nothing.</p>
                ) : (
                  <ul className="pl-3 text-nb-500">
                    {preview.filter((m) => m.application === a.id).map((m) => <li key={m.id}>{memberText(m)}</li>)}
                  </ul>
                )}
              </div>
            ))}
            <p className="text-nb-500" data-testid="fusion-token-apps-note">
              A service that joins these applications later (a person adds it, or its labels do) is not the token&apos;s; one that leaves or moves is gone from it. Aliases from app labels never count.
            </p>
          </div>
        )}
        {(namespaces.length > 0 || clusters.length > 0) && (
          <p className="text-xs leading-relaxed text-nb-500">
            A token with a namespace or cluster limit can use the structured filters only. Raw PromQL, LogQL and TraceQL, and the Prometheus, Loki and Tempo APIs themselves, need a token with no such limit, because a limit cannot be enforced on a query written by hand.
          </p>
        )}
        {error && <ErrorBanner>{error}</ErrorBanner>}
      </div>
    </Modal>
  )
}
