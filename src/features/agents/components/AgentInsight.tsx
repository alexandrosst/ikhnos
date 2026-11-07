import clsx from 'clsx'
import { StatusBadge, type StatusTone } from '@/components/ui/Status'
import { Info } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Button, Field, TagsInput } from '@/components/ui/primitives'
import { CopyCommand } from '@/components/ui/CopyCommand'
import { ServerCommand } from '@/components/ui/ServerCommand'
import TierLevels from '@/components/TierLevels'
import { upgradeRequest, useAgentUpgradeCommand } from '@/lib/agentUpgrade'
import { api, ApiError, type ServerInfo } from '@/lib/api'
import { COLLECTORS, collectorState, consentChange, discoveryStatus, effectiveNote, healthSummary, inForce, parseExclusions, scopeWords, sortProblems, tierName, uptimeWords, type AgentConsent, type AgentDiagnostics, type AgentProblem, type HealthSummary, type Severity } from '@/lib/consent'
import type { Agent, AccessTier } from '@/lib/types'
import { useServer } from '@/store/server'

const SEVERITY_STYLE: Record<Severity, { tone: StatusTone; label: string }> = {
  error: { tone: 'down', label: 'Error' },
  warn: { tone: 'attention', label: 'Warning' },
  info: { tone: 'notice', label: 'Notice' },
}

const LEVEL_TONE: Record<HealthSummary['level'], StatusTone> = { unknown: 'unknown', healthy: 'healthy', warn: 'attention', error: 'down' }

/** The agent's health in a few words, for a table row: "Healthy", "2 problems". */
export function HealthChip({ diagnostics, connected }: { diagnostics?: AgentDiagnostics; connected?: boolean }) {
  const h = healthSummary(diagnostics, { now: Date.now(), connected })
  return (
    <StatusBadge
      tone={LEVEL_TONE[h.level]}
      label={h.line}
      data-testid="health-chip"
      data-level={h.level}
      title={h.level === 'unknown' ? (diagnostics ? 'The agent’s last self-report is old, or the agent is not connected: what it said then may no longer be true.' : 'The agent has not sent a self-report yet (or this is an older agent).') : 'From the agent’s own account of itself'}
    />
  )
}

/**
 * Whether the agent has finished its first full look at the cluster - every watch it runs synced at least
 * once. This is a one-time milestone, not how healthy the agent is (that's HealthChip) and not a live count
 * (those keep changing once discovery is done): it disappears once there is nothing left to report.
 */
export function DiscoveryChip({ diagnostics }: { diagnostics?: AgentDiagnostics }) {
  const s = discoveryStatus(diagnostics)
  if (s.state === 'unknown') return null
  if (s.state === 'done') {
    return <StatusBadge tone="healthy" label="Fully discovered" data-testid="discovery-chip" data-state="done" title={`Every watch this agent runs (${s.total}) has completed its first full read of the cluster.`} />
  }
  return <StatusBadge tone="setup" label={`Discovering ${s.synced}/${s.total}`} data-testid="discovery-chip" data-state="discovering" title="Still doing its first full read of the cluster (a one-time pass, separate from how busy it is once caught up)." />
}

/** What the agent says is wrong, worst first, each with what to do about it. */
export function Problems({ problems }: { problems: AgentProblem[] }) {
  if (problems.length === 0) return null
  return (
    <ul className="space-y-2" data-testid="agent-problems">
      {sortProblems(problems).map((p, i) => {
        const s = SEVERITY_STYLE[p.severity]
        return (
          <li key={`${p.code}-${i}`} className="rounded-md border border-nb-850 bg-nb-950/60 px-3 py-2" data-testid="agent-problem" data-code={p.code}>
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge tone={s.tone} label={s.label} />
              <code className="font-mono text-xs text-nb-300">{p.code}</code>
              {p.since && <span className="text-xs text-nb-600">since {new Date(p.since).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })}</span>}
            </div>
            <p className="mt-1 break-words text-xs leading-relaxed text-nb-300">{p.message}</p>
          </li>
        )
      })}
    </ul>
  )
}

const TONE: Record<string, string> = { ok: 'text-ok', warn: 'text-warn', paused: 'text-info', off: 'text-nb-500' }

const INTENT_WORDS =
  "This is the agent's observability intent: the scope of what it reads and reports. It can only reduce what this agent shares. What it may read at most is set by whoever owns the cluster, when it was installed (its access tier, permissions and scope), and this server cannot exceed it: the agent checks that itself and ignores anything that would widen it."

function Switch({ on, onChange, label, testId }: { on: boolean; onChange: (on: boolean) => void; label: string; testId?: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={clsx('relative h-5 w-9 shrink-0 cursor-pointer rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent', on ? 'bg-accent' : 'bg-nb-700')}
      data-testid={testId}
    >
      <span className={clsx('absolute top-0.5 size-4 rounded-full bg-white transition-all', on ? 'left-[18px]' : 'left-0.5')} aria-hidden />
    </button>
  )
}

/**
 * What an agent may read and report, and what it does now, in one place. The level it is approved for, what that leaves in view (namespaces,
 * the optional collectors) and the three things that can only REDUCE it: a lower level, a collector switched off, namespaces left out. The agent
 * enforces all of it for itself: what is above the ceiling its install sets is shown, locked, with the command the cluster's owner runs to raise it.
 * `editable` is whether this person may change it; without it the same panel only says what is so.
 */
export function AccessPanel({ agent, diagnostics: d, consent, editable }: { agent: Agent; diagnostics?: AgentDiagnostics; consent?: AgentConsent; editable: boolean }) {
  const server = useServer()
  const info: ServerInfo | undefined = server.info
  // The widen command names the chart this server currently resolves: read it again now, so a long-lived tab does not print an old version.
  const reloadInfo = server.reloadInfo
  useEffect(() => {
    void reloadInfo()
  }, [reloadInfo])
  const installed = (agent.installedTier ?? d?.installedTier ?? agent.accessTier) as AccessTier
  const implemented = Math.min(info?.implementedTier ?? 2, 2) as AccessTier
  const tierLadder = useMemo(() => Array.from({ length: implemented + 1 }, (_, i) => i as AccessTier), [implemented])
  const stored = consent ?? { pausedCollectors: [], excludedNamespaces: [], confirmed: true, unknownNamespaces: [] }

  const [tier, setTier] = useState<AccessTier>(agent.accessTier)
  const [paused, setPaused] = useState<string[]>(stored.pausedCollectors)
  const [excl, setExcl] = useState<string[]>(stored.excludedNamespaces)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [helm, setHelm] = useState('')
  const [saved, setSaved] = useState(false)
  const [asked, setAsked] = useState<AccessTier | null>(null) // an option above the ceiling that was clicked

  // Follow the server while nothing has been edited (another editor may have changed it, or the agent confirmed).
  const storedKey = `${agent.accessTier}|${stored.pausedCollectors.join(',')}|${stored.excludedNamespaces.join(',')}`
  const [seen, setSeen] = useState(storedKey)
  const parsed = useMemo(() => parseExclusions(excl.join(', ')), [excl])
  const draft = { tier, paused, excluded: parsed.names }
  const change = consentChange(draft, agent.accessTier, stored)
  const dirty = change.tier || change.overrides
  if (storedKey !== seen) {
    // Adjusting state while rendering, the documented way to follow a prop: only when nothing has been edited.
    setSeen(storedKey)
    if (!dirty) {
      setTier(agent.accessTier)
      setPaused(stored.pausedCollectors)
      setExcl(stored.excludedNamespaces)
    }
  }

  const toShow = editable ? (asked ?? (installed < implemented ? ((installed + 1) as AccessTier) : undefined)) : undefined
  // The command that raises the ceiling to it is written by the server, which knows the chart it serves.
  const widenRequest = useMemo(() => (toShow === undefined ? null : upgradeRequest({ tier: toShow, target: { namespace: agent.namespace, release: agent.releaseName } })), [toShow, agent.namespace, agent.releaseName])
  const widen = useAgentUpgradeCommand(widenRequest)
  const valid = parsed.problems.length === 0
  // The server's own confirmed/setAt take precedence once present (they read correctly on a fresh page
  // load, before any diagnostics have come in this session); inForce is the fallback for an older server
  // that has not started sending them yet.
  const confirmed = stored.confirmed ?? inForce(consent, agent.accessTier, d)
  const narrowed = stored.pausedCollectors.length > 0 || stored.excludedNamespaces.length > 0
  const waitingWords = !confirmed && narrowed && stored.setAt ? `${uptimeWords((Date.now() - Date.parse(stored.setAt)) / 1000)} ago` : undefined

  // The optional collectors: those in the cluster get a row (and, for an editor, a switch); the rest are one quiet line.
  const collectors = COLLECTORS.map((c) => {
    const live = d?.collectors.find((x) => x.name === c.id)
    return { c, state: d ? collectorState(live) : undefined, present: d ? !!live?.configured : true }
  })
  const rows = collectors.filter((x) => x.present || paused.includes(x.c.id))
  const missing = collectors.filter((x) => !rows.includes(x))
  const note = d && !dirty ? effectiveNote(d) : undefined

  const save = async () => {
    const c = server.conn()
    if (!c || busy || !valid || !dirty) return
    setBusy(true)
    setError('')
    setHelm('')
    setSaved(false)
    try {
      if (change.tier) await api.setAgentTier(c, agent.id, tier)
      if (change.overrides) await api.setAgentConsent(c, agent.id, { pausedCollectors: paused, excludedNamespaces: parsed.names })
      setSaved(true)
      await (stored.pausedCollectors.some((id) => !paused.includes(id)) ? server.boostPolling() : server.refresh()) // a resumed collector starts sending within a window
    } catch (e) {
      if (e instanceof ApiError) {
        setError(e.message.split('\n')[0])
        const h = e.body?.helm
        if (typeof h === 'string') setHelm(h)
      } else setError('Could not save.')
      await server.refresh().catch(() => undefined) // the first of two changes may have been applied
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="rounded-lg border border-nb-850 bg-nb-925 p-4" data-testid="agent-access" data-editable={editable}>
      <div className="text-sm font-medium text-nb-300" id="tier-label">Access</div>
      <p className="mb-3 mt-0.5 text-xs text-nb-500">
        What this agent may read and report. It can only reduce what the install allows.{' '}
        <span className="inline-flex align-text-bottom" tabIndex={0} role="img" title={INTENT_WORDS} aria-label={INTENT_WORDS}>
          <Info size={14} aria-hidden />
        </span>
      </p>

      <TierLevels
        layout="row"
        tiers={tierLadder}
        value={editable ? tier : ((d?.effectiveTier ?? agent.accessTier) as AccessTier)}
        max={installed}
        markAt={editable ? undefined : (agent.accessTier as AccessTier)}
        markLabel={`Approved up to ${tierName(agent.accessTier).toLowerCase()}`}
        onSelect={
          editable
            ? (t) => {
                if (t > installed) setAsked(t)
                else {
                  setTier(t)
                  setSaved(false)
                }
              }
            : undefined
        }
        size="sm"
        data-testid="tier-option"
        aria-labelledby="tier-label"
      />
      {note && <p className="mt-1 text-xs text-warn" data-testid="tier-note">{note}</p>}
      {toShow !== undefined ? (
        <div className="mt-2 text-xs text-nb-500" data-testid="widen-hint">
          This install allows up to <span className="text-nb-300">{tierName(installed).toLowerCase()}</span>. To allow <span className="text-nb-300">{tierName(toShow).toLowerCase()}</span>, the cluster's owner runs this in that cluster, and then you can select it here:
          <ServerCommand command={widen} />
        </div>
      ) : (
        <p className="mt-1.5 text-xs text-nb-500" data-testid="ceiling-line">The install allows up to <span className="text-nb-300">{tierName(installed).toLowerCase()}</span>.</p>
      )}
      {editable && agent.hardenHelm && (
        <div className="mt-2 rounded-md border border-warn/20 bg-warn/5 px-3 py-2 text-xs leading-relaxed text-nb-500" data-testid="harden-hint">
          Approved access here is narrower than what this install allows in the cluster. The agent already stopped reading and reporting at the wider tier, but that is a software-level change only: its <span className="text-nb-300">Kubernetes RBAC still grants the wider tier</span> until the cluster's owner runs this there too:
          <CopyCommand text={agent.hardenHelm} />
        </div>
      )}

      <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        {d && (
          <>
            <dt className="text-nb-500">Namespaces</dt>
            <dd className="text-nb-300" data-testid="scope-words">{scopeWords(d)}</dd>
          </>
        )}
      </dl>

      {rows.length > 0 && (
        <ul className="mt-3 divide-y divide-nb-850 border-y border-nb-850" data-testid="collectors">
          {rows.map(({ c, state }) => (
            <li key={c.id} className="flex items-center gap-3 py-2 text-sm" data-testid={`collector-${c.id}`} data-tone={state?.tone}>
              <span className="min-w-0 flex-1">
                <span className="text-nb-300" title={c.what}>{c.label}</span>
                {state && <span className={clsx('ml-2 text-xs', TONE[state.tone])}>{state.label}</span>}
              </span>
              {editable && (
                <Switch
                  on={!paused.includes(c.id)}
                  onChange={(on) => {
                    setPaused(on ? paused.filter((x) => x !== c.id) : [...paused, c.id])
                    setSaved(false)
                  }}
                  label={`${c.label} on`}
                  testId={`collector-switch-${c.id}`}
                />
              )}
            </li>
          ))}
        </ul>
      )}
      {editable && rows.length > 0 && <p className="mt-1.5 text-xs text-nb-500">Switching one off stops it in the cluster and forgets what it held.</p>}
      {missing.length > 0 && (
        <p className="mt-2 text-xs text-nb-500" data-testid="collectors-missing">Not installed: {missing.map((x) => x.c.label.toLowerCase()).join(', ')}</p>
      )}

      {editable && (
        <div className="mt-4">
          <Field label="Left out" hint="Dropped by the agent before anything is sent, on top of what the install already leaves out. System namespaces cannot be left out.">
            <TagsInput value={excl} onChange={(v) => { setExcl(v); setSaved(false) }} placeholder="e.g. payments, batch" aria-invalid={!valid} aria-describedby={valid ? undefined : 'exclude-problems'} data-testid="exclude-input" />
          </Field>
          {!valid && (
            <ul id="exclude-problems" className="mt-1 text-xs text-bad" role="alert" data-testid="exclude-problems">
              {parsed.problems.map((p) => <li key={p}>{p}</li>)}
            </ul>
          )}
          {valid && !dirty && stored.unknownNamespaces && stored.unknownNamespaces.length > 0 && (
            <ul className="mt-1 text-xs text-warn" data-testid="exclude-unknown">
              {stored.unknownNamespaces.map((n) => (
                <li key={n}>“{n}” has not been reported by this agent yet — check the spelling, or it may not have rolled out.</li>
              ))}
            </ul>
          )}
        </div>
      )}

      {editable && (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <Button variant="primary" size="sm" disabled={!dirty || !valid || busy} onClick={() => void save()} data-testid="consent-save">{busy ? 'Saving…' : 'Save'}</Button>
          {dirty && !busy && <button type="button" className="text-xs text-nb-500 hover:text-nb-300" onClick={() => { setTier(agent.accessTier); setPaused(stored.pausedCollectors); setExcl(stored.excludedNamespaces); setError(''); setHelm('') }}>Discard changes</button>}
          {!dirty && !error && (saved || (narrowed && !confirmed)) && (
            <span className={clsx('text-xs', confirmed ? 'text-ok' : 'text-nb-400')} role="status" data-testid="consent-status">
              {confirmed
                ? saved
                  ? 'Saved, and the agent confirms it is in force.'
                  : 'The agent confirms this is in force.'
                : saved
                  ? 'Saved. The agent applies this within seconds; this updates when it does.'
                  : `Waiting on the agent to confirm this${waitingWords ? ` — asked ${waitingWords}` : ''}.`}
            </span>
          )}
        </div>
      )}
      {error && (
        <div role="alert" className="mt-2 rounded-md border border-bad/30 bg-bad/10 px-3 py-2 text-xs text-bad" data-testid="consent-error">
          {error}
          {helm && <CopyCommand text={helm} />}
        </div>
      )}
      {d?.partial && <p className="mt-3 text-xs text-nb-600">This is what the agent said when it connected; it has not finished looking at the cluster yet.</p>}
    </div>
  )
}
