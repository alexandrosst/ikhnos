import { Tabs } from '@/components/ui/Tabs'
import { Activity, CheckCircle2, ChevronRight, Cpu, Loader2, MapPin, Network, Pin, Timer } from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Flag } from '@/components/ui/brand'
import { Button, CopyButton, ErrorBanner, Field, ICON_SM, InfoTip, Input, Modal, SectionLabel, TagsInput, WizardSteps } from '@/components/ui/primitives'
import TierLevels from '@/components/TierLevels'
import { api, ApiError, type CreatedToken } from '@/lib/api'
import { discoveryStatus, extrasOf } from '@/lib/consent'
import { effective } from '@/lib/effective'
import { previewImage } from '@/lib/image'
import { emptyScope, scopeActive, scopeProblems, shArg } from '@/lib/install'
import { findCities, nearestCity, suggestionFromCity, type City } from '@/lib/places'
import { usePlaceIndex } from '@/lib/places-data'
import { countryName } from '@/lib/present'
import type { AccessTier, Cluster } from '@/lib/types'
import { useServer } from '@/store/server'
import { useRawTopology } from '@/store/topology'
import ApprovalCard from './ApprovalCard'
import ControllerSteps from './ControllerSteps'

/**
 * Shown once a wizard-connected cluster has finished its first discovery and still has no place: none of
 * `placementCandidates`'s three automatic signals resolved (see places.ts) - no cloud region, no city
 * recognisable in a label, no usable GeoIP. Rather than leaving the cluster to sit unplaced until someone
 * finds it later on its own page, this asks once, right here, for a city (typed, or "Use my location" via
 * the browser) and applies it the same way accepting any other placement suggestion does - see
 * `suggestionFromCity`, which goes through the same `decideDerived` as `PlacementHint`'s "Use" button, so
 * it is declared, audited and reuses a nearby site instead of duplicating one.
 */
function WhereIsThis({ cluster }: { cluster: Cluster }) {
  const decide = useRawTopology((s) => s.decideDerived)
  const sites = useRawTopology((s) => s.sites)
  const places = usePlaceIndex()
  const [search, setSearch] = useState('')
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const hits = useMemo(() => (places ? findCities(places, search, 6) : []), [places, search])

  const pick = (c: City) => {
    decide(suggestionFromCity(cluster, c, sites), 'accepted')
    setSearch('')
    setNote('')
  }

  const useHere = () => {
    if (!places || !navigator.geolocation) return
    setNote('')
    setBusy(true)
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setBusy(false)
        const near = nearestCity(places, pos.coords.latitude, pos.coords.longitude, 60)
        if (near) pick(near.city)
        else setNote('Nothing in the city table is close enough to where your browser is right now.')
      },
      () => {
        setBusy(false)
        setNote('Location was not available, or you said no to sharing it.')
      },
      { timeout: 10_000 },
    )
  }

  return (
    <div className="mt-3 rounded-lg border border-nb-850 bg-nb-925 p-3" data-testid="wizard-where">
      <div className="mb-1 flex items-center gap-1.5 text-sm font-medium text-nb-300">
        <MapPin size={ICON_SM} className="text-accent" aria-hidden /> Where is this?
      </div>
      <p className="mb-2 text-xs text-nb-500">
        Nothing about its location could be worked out on its own: no cloud region, no city recognised in a label, and IP lookup found nothing (or is not set up on this server). Type a city, or leave it - you can always set it later from the cluster's own page.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={places ? 'Type a city name…' : 'Loading the city table…'} disabled={!places} aria-label="Find a city" className="min-w-48 flex-1" data-testid="wizard-where-search" />
        <Button
          size="sm"
          onClick={useHere}
          disabled={!places || busy || !navigator.geolocation}
          title="Uses your browser's own location, not the cluster's - only a fair guess if you happen to be where the cluster is"
          data-testid="wizard-where-here"
        >
          {busy ? 'Locating…' : 'Use my location'}
        </Button>
      </div>
      {hits.length > 0 && (
        <ul className="mt-1.5 overflow-hidden rounded-md border border-nb-800 bg-nb-930" role="listbox" aria-label="Matching cities">
          {hits.map((c) => (
            <li key={`${c.name}-${c.cc}-${c.lat}-${c.lng}`}>
              <button type="button" role="option" aria-selected={false} className="flex w-full items-center gap-1.5 px-3 py-1.5 text-left text-sm text-nb-300 hover:bg-nb-850 hover:text-nb-300" onClick={() => pick(c)} data-testid="wizard-where-hit">
                <Flag code={c.cc} /> {c.name}, <span className="text-nb-500">{countryName(c.cc)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {note && <p className="mt-1.5 text-xs text-warn" role="status">{note}</p>}
    </div>
  )
}

/** Where Settings → Installation lives. Inside the wizard's token step it opens in a new tab, so the command (shown once) is not lost. */
function SettingsLink({ onNavigate, newTab }: { onNavigate?: () => void; newTab?: boolean }) {
  return newTab ? (
    <a className="text-accent hover:underline" href="/settings#installation" target="_blank" rel="noopener noreferrer">Settings → Installation</a>
  ) : (
    <Link className="text-accent hover:underline" to="/settings#installation" onClick={onNavigate}>Settings → Installation</Link>
  )
}

/** The honest notice for a server with no image registry set anywhere: the chart's own names are used, and nobody publishes them for you. */
function NoRegistryNotice({ onNavigate, newTab }: { onNavigate?: () => void; newTab?: boolean }) {
  return (
    <p className="rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn" role="note" data-testid="no-registry-notice">
      No image registry configured: the command below uses the chart’s built-in image names, which you must have published yourself. Set your registry in <SettingsLink onNavigate={onNavigate} newTab={newTab} />.
    </p>
  )
}

/** A small on/off switch. A real checkbox underneath (so it is focusable, announced as a switch and has a test id), drawn as a track and a knob. */
function Switch({ testId, checked, disabled, onChange, label }: { testId: string; checked: boolean; disabled?: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <span className="relative inline-flex shrink-0 items-center">
      <input
        type="checkbox"
        role="switch"
        className="peer absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        aria-label={label}
        data-testid={testId}
      />
      <span className="h-5 w-9 rounded-full bg-nb-800 transition-colors peer-checked:bg-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent/60 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-nb-920" aria-hidden />
      <span className="pointer-events-none absolute left-0.5 h-4 w-4 rounded-full bg-nb-300 shadow transition-transform peer-checked:translate-x-4 peer-checked:bg-white" aria-hidden />
    </span>
  )
}

/** A small label saying what an option puts where; `warn` for the one that asks for more than the rest. */
function Tag({ children, warn }: { children: ReactNode; warn?: boolean }) {
  return <span className={`rounded border px-1.5 py-px text-[11px] font-normal ${warn ? 'border-warn/40 text-warn' : 'border-nb-800 text-nb-500'}`}>{children}</span>
}

/**
 * One optional thing the agent can do, as a row: an icon, a bold title with tags saying what it installs, one line
 * of what it does (the fuller account behind the info tooltip), and a switch. Rows that need more access than was
 * chosen stay in view, dimmed and shown off, with a pill saying what they need - nothing is hidden behind a fold.
 */
function OptionRow({ testId, icon, checked, needs, onChange, title, tags, summary, why, children }: { testId: string; icon: ReactNode; checked: boolean; /** What is missing for this row, if anything. */ needs?: string; onChange: (v: boolean) => void; title: string; tags?: ReactNode; summary: string; why: string; children?: ReactNode }) {
  const disabled = !!needs
  const on = checked && !disabled
  return (
    <div className={`rounded-lg border px-4 py-3 ${disabled ? 'border-dashed border-nb-850 bg-nb-925 opacity-60' : on ? 'border-accent/60 bg-accent-soft' : 'border-nb-850 bg-nb-925'}`}>
      <div className="flex items-start gap-3">
        <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-nb-850 bg-nb-930 text-nb-500" aria-hidden>{icon}</span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium text-nb-300">
            {title}
            {tags}
          </div>
          <p className="text-sm text-nb-500">
            {summary}
            <InfoTip>{why}</InfoTip>
          </p>
        </div>
        {needs ? (
          <span className="shrink-0 self-center rounded-full border border-dashed border-nb-700 px-2.5 py-0.5 text-xs text-nb-500" data-testid={`${testId}-needs`}>{needs}</span>
        ) : null}
        <div className={needs ? 'sr-only' : 'self-center'}>
          <Switch testId={testId} checked={on} disabled={disabled} onChange={onChange} label={title} />
        </div>
      </div>
      {children}
    </div>
  )
}

/**
 * The install command and what goes with it. While the agent has not connected yet it is the thing to do, so it is
 * open; once the agent has connected the command has been run, and the next thing is the approval, so it folds
 * down to one line (still there to read again) instead of pushing the code far down the dialog.
 */
function AfterRun({ collapsed, children }: { collapsed: boolean; children: ReactNode }) {
  if (!collapsed) return <div>{children}</div>
  return (
    <details className="group rounded-lg border border-nb-850 bg-nb-925" data-testid="install-collapsed">
      <summary className="flex cursor-pointer select-none items-center gap-2 px-4 py-2.5 text-sm text-nb-500 marker:content-none">
        <CheckCircle2 size={ICON_SM} className="text-ok" aria-hidden /> Install command run.
        <span className="flex items-center gap-1 text-nb-400">
          Show it again <ChevronRight size={ICON_SM} className="transition-transform group-open:rotate-90" aria-hidden />
        </span>
      </summary>
      <div className="border-t border-nb-850 p-4">{children}</div>
    </details>
  )
}

/**
 * What to look at when the agent has not shown up after a couple of minutes. Not shown before: most installs connect in under one,
 * and a list of things to check next to a spinner that is about to finish only makes it look like something is wrong. The commands
 * are for the way this cluster was asked: the pod's own log for helm; the resource the controller made, and the command that raises
 * the controller's ceiling if it says it was installed with a lower one, for the controller.
 */
function StillWaiting({ via, created }: { via: 'helm' | 'controller'; created: CreatedToken }) {
  const c = created.controller
  return (
    <div className="mt-3 rounded-lg border border-nb-850 bg-nb-925 px-4 py-3 text-xs text-nb-400" data-testid="still-waiting">
      <div className="mb-1.5 text-sm font-medium text-nb-300">Still waiting?</div>
      {via === 'helm' || !c.available ? (
        <>
          <p>Check that the agent started:</p>
          <CommandLine text={WAITING_PODS} />
          <p className="mt-2">If its pod is not running, its log says why:</p>
          <CommandLine text={WAITING_LOG} />
        </>
      ) : (
        <>
          <p>Check what the controller made of it:</p>
          <CommandLine text={WAITING_RESOURCE} />
          {c.raise && (
            <>
              <p className="mt-2">If its status says the controller was installed with a lower access level than tier {c.maxTier}, raise it:</p>
              <CommandLine text={c.raise} testId="controller-raise" />
            </>
          )}
        </>
      )}
    </div>
  )
}

/** How long the agent may take before the wizard says what to check. */
const STALLED_AFTER_MS = 120_000
const WAITING_PODS = 'kubectl -n ikhnos-system get pods'
const WAITING_LOG = 'kubectl -n ikhnos-system logs deploy/ikhnos-agent'
const WAITING_RESOURCE = 'kubectl -n ikhnos-system get ikhnosagent'

function CommandLine({ text, testId }: { text: string; testId?: string }) {
  return (
    <div className="mt-1 flex items-center gap-2">
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap rounded border border-nb-850 bg-nb-950 px-2 py-1.5 font-mono text-[11px] text-nb-300" data-testid={testId}>{text}</code>
      <CopyButton text={text} label="Copy" />
    </div>
  )
}

type Phase = 'form' | 'waiting' | 'approve' | 'discovering' | 'done' | 'stopped'
const STEPS: { key: Phase; label: string }[] = [
  { key: 'form', label: 'Set up' },
  { key: 'waiting', label: 'Agent connects' },
  { key: 'approve', label: 'You approve it' },
  { key: 'discovering', label: 'Discovering' },
  { key: 'done', label: 'Connected' },
]

/** Create a token, show the install command, then follow the agent through approval to first discovery. */
export default function ConnectClusterWizard({ open, onClose, nodeProbe }: { open: boolean; onClose: () => void; /** Opens with the node probe on: the central operator's card asks for it, since the probe is what reports a node's addresses. */ nodeProbe?: boolean }) {
  const conn = useServer((s) => s.conn)
  const baseUrl = useServer((s) => s.url)
  const refresh = useServer((s) => s.refresh)
  const state = useServer((s) => s.state)
  const info = useServer((s) => s.info)
  const reloadInfo = useServer((s) => s.reloadInfo)
  // Settings → Installation may have changed since the page loaded; the command must reflect it.
  useEffect(() => {
    if (open) void reloadInfo()
  }, [open, reloadInfo])
  const chartRef = info?.install?.chartRef ?? ''
  // The downloadable file matters only when the command names it.
  const chartFile = chartRef ? undefined : info?.install?.chartFile
  const imagesConfigured = !!info?.install?.imagesConfigured
  // The one image this command makes the cluster pull, as the server resolved it (the organisation's setting, else the server's flags).
  const img = previewImage({ registry: info?.install?.imageRegistry ?? '', tag: info?.install?.imageTag ?? '', digest: info?.install?.imageDigest ?? '' })
  const clusters = useRawTopology((s) => s.clusters)
  const agents = useRawTopology((s) => s.agents)
  const nodes = useRawTopology((s) => s.nodes)
  const services = useRawTopology((s) => s.services)
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [tier, setTier] = useState<AccessTier>(2)
  const [probe, setProbe] = useState(false)
  const [flows, setFlows] = useState(false)
  const [measure, setMeasure] = useState(false)
  const [scopeOn, setScopeOn] = useState(false)
  // On unless a person turns it off: setting up telemetry later should not be a second install.
  const [inc, setInc] = useState<string[]>([])
  const [exc, setExc] = useState<string[]>([])
  const [sel, setSel] = useState('')
  const [created, setCreated] = useState<CreatedToken | null>(null)
  // How the agent is asked for: the helm command, or the resource for the optional controller. Both are written from the same choices.
  const [via, setVia] = useState<'helm' | 'controller'>('helm')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // Whether the agent is taking longer than it usually does to show up: after two minutes of waiting, the card says what to look at.
  const [stalled, setStalled] = useState(false)

  // A default name so Create works the moment the wizard opens; still yours to change, and renameable later either way.
  // Deliberately keyed on `open` alone: it should fill in once per open, not re-fill while the count changes under it.
  useEffect(() => {
    if (open) setName((n) => n || `cluster-${clusters.length + 1}`)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])
  // Deliberately keyed on `open` alone too, for the same reason: seed once per open, not on every change.
  useEffect(() => {
    if (open && nodeProbe) setProbe(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const scope = useMemo(() => (scopeOn && tier >= 2 ? { ...emptyScope, namespaces: inc, exclude: exc, selector: sel } : emptyScope), [scopeOn, tier, inc, exc, sel])
  const problems = scopeProblems(scope)
  const max = Math.min(info?.implementedTier ?? 2, 2)
  const agent = useMemo(() => {
    if (!created) return undefined
    const since = new Date(created.meta.createdAt).getTime() - 1000
    return state?.agents?.find((a) => a.name === created.meta.name && new Date(a.requestedAt).getTime() >= since)
  }, [state, created])
  const mine = agents.find((a) => a.id === agent?.id)
  const cluster = clusters.find((c) => c.agentId === agent?.id)
  const counts = cluster
    ? { nodes: nodes.filter((n) => n.clusterId === cluster.id && !n.deletedAt).length, services: services.filter((s) => s.clusterId === cluster.id && !s.deletedAt).length }
    : null
  // Whether the agent has finished its first full read of the cluster (every watch synced), not just found
  // one thing: a big cluster can report its first node long before it is done looking around.
  const discovery = discoveryStatus(extrasOf(state?.agents, agent?.id ?? '').diagnostics)

  const close = () => {
    setCreated(null)
    setVia('helm')
    setName('')
    setProbe(false)
    setFlows(false)
    setMeasure(false)
    setScopeOn(false)
    setInc([])
    setExc([])
    setSel('')
    setError('')
    setStalled(false)
    onClose()
  }

  const create = async () => {
    const c = conn()
    if (!c) return setError('Not connected to the server.')
    setBusy(true)
    setError('')
    try {
      // What was chosen goes to the server, which writes the whole install command (and refuses a choice the access level cannot honour).
      const t = await api.createToken(c, name.trim(), tier, {
        collectors: { nodeProbe: probe && tier >= 1, flowObserver: flows && tier >= 2, measurements: measure && tier >= 2 },
        scope,
      })
      setCreated(t)
      await refresh()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not create the token.')
    } finally {
      setBusy(false)
    }
  }

  const phase = !created
    ? 'form'
    : !agent
      ? 'waiting'
      : agent.status === 'pending'
        ? 'approve'
        : agent.status === 'approved'
          ? (counts && discovery.state !== 'discovering' ? 'done' : 'discovering')
          : 'stopped'
  useEffect(() => {
    if (phase !== 'waiting') return
    const t = setTimeout(() => setStalled(true), STALLED_AFTER_MS)
    return () => clearTimeout(t)
  }, [phase])
  // Which step a stopped enrollment never got past. Rejected or expired both happen while still pending
  // approval; a revoke can only happen to something that was already approved, so it reads as having failed
  // one step further along (we don't know exactly when during discovery it was cut off, so this is our best guess).
  const stoppedAt = agent?.status === 'revoked' ? 3 : 2

  return (
    <Modal
      open={open}
      onClose={close}
      width="max-w-2xl"
      title="Connect a cluster"
      description="Install a small read-only agent in the cluster. It dials out to this server, so the cluster needs no inbound port and you hand over no kubeconfig."
      footer={
        phase === 'form' ? (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" onClick={create} disabled={busy || !name.trim() || problems.length > 0}>
              {busy ? 'Creating…' : 'Create install command'}
            </Button>
          </>
        ) : phase === 'done' ? (
          <>
            <Button onClick={close}>Close</Button>
            <Button
              variant="primary"
              onClick={() => {
                close()
                navigate('/topology')
              }}
            >
              View in topology
            </Button>
          </>
        ) : (
          <Button onClick={close}>{phase === 'stopped' ? 'Close' : 'Continue in background'}</Button>
        )
      }
    >
      <div className="mb-1">
        <WizardSteps
          steps={STEPS.map((s) => s.label)}
          currentIndex={phase === 'stopped' ? stoppedAt : STEPS.findIndex((s) => s.key === phase)}
          failedIndex={phase === 'stopped' ? stoppedAt : undefined}
        />
      </div>
      {phase === 'form' && (
        <div className="space-y-5">
          <Field label="Cluster name" hint="How it appears in Ikhnos. You can rename it later.">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="edge-patras" autoFocus />
          </Field>
          {max >= 1 && (
            <section className="space-y-2" data-testid="wizard-access">
              <SectionLabel>Access</SectionLabel>
              <TierLevels tiers={([1, 2] as AccessTier[]).filter((t) => t <= max)} value={tier} onSelect={(t) => setTier(t)} layout="cards" data-testid="wizard-tier" />
              <p className="text-xs text-nb-500">Always read-only: it can never read Secrets or ConfigMaps, or change anything in the cluster.</p>
            </section>
          )}
          {max >= 1 && (
            <section className="space-y-2" data-testid="wizard-collect">
              <SectionLabel>Collect more</SectionLabel>
              <OptionRow
                testId="node-probe-toggle"
                icon={<Cpu size={ICON_SM} />}
                checked={probe}
                needs={tier < 1 ? 'Needs Infrastructure access' : undefined}
                onChange={setProbe}
                title="Node hardware and network"
                tags={<Tag>pod on each node</Tag>}
                summary="Virtual machine or bare metal, device model, disks, uplinks and tunnels."
                why="Kubernetes cannot say whether a node is a virtual machine or a physical server, so Ikhnos otherwise guesses and marks the guess as uncertain. A small pod on every node reads the CPU model and flags, the hypervisor, the firmware and board vendor and product, the ARM board model, the disks (capacity and type), the network interfaces including uplinks and VPN tunnels, and the node's own addresses - never serial numbers, MAC addresses or processes. It needs the host's /sys and network, so a namespace that enforces the Pod Security baseline must be labelled privileged."
              />
              {max >= 2 && (
                <OptionRow
                  testId="flow-observer-toggle"
                  icon={<Network size={ICON_SM} />}
                  checked={flows}
                  needs={tier < 2 ? 'Needs Services access' : undefined}
                  onChange={setFlows}
                  title="Traffic between services"
                  tags={
                    <>
                      <Tag>pod on each node</Tag>
                      <Tag warn>extra privileges</Tag>
                    </>
                  }
                  summary="Counts connections and bytes on every node."
                  why="Kubernetes does not record who calls whom. A pod on every node counts connections and the bytes they carried (TCP via eBPF where the kernel supports it, TCP and UDP via the kernel's connection table otherwise) - never packets or contents. It needs extra privileges (root with CAP_BPF and CAP_PERFMON, and the host's process view for live byte totals), so read what it is allowed to do before you enable it."
                />
              )}
              {max >= 2 && (
                <OptionRow
                  testId="measurements-toggle"
                  icon={<Timer size={ICON_SM} />}
                  checked={measure}
                  needs={tier < 2 ? 'Needs Services access' : undefined}
                  onChange={setMeasure}
                  title="Round-trip times"
                  tags={<Tag>runs in the agent</Tag>}
                  summary="Times connections to a few outside addresses."
                  why="Times how long a TCP connection takes to open, and how often it fails, for placement advice. It is part of the agent, not a separate pod. Nothing is sent over the connection; loopback, link-local and metadata addresses are always refused, and the agent refuses any address the server did not issue."
                >
                  {measure && !flows && tier >= 2 && (
                    <p className="mt-2 pl-11 text-xs text-nb-500" data-testid="measurements-hint">
                      Times the addresses you add on the Sites page. Turn on <strong className="font-medium text-nb-300">Traffic between services</strong> to also offer the ones your workloads call most.
                    </p>
                  )}
                </OptionRow>
              )}
            </section>
          )}
          {max >= 2 && (
            <section className="space-y-2" data-testid="wizard-limit">
              <SectionLabel>Limit what it reports</SectionLabel>
              <OptionRow
                testId="scope-toggle"
                icon={<Activity size={ICON_SM} />}
                checked={scopeOn}
                needs={tier < 2 ? 'Needs Services access' : undefined}
                onChange={setScopeOn}
                title="Only some namespaces"
                summary="What is left out never leaves the cluster."
                why="This is a privacy boundary, not a permission: the agent's read-only access is unchanged. By default it reports every namespace. System namespaces are still read to recognise the cluster but never drawn. Any namespace labelled ikhnos.io/observe=false is left out whatever you choose here."
              >
                {scopeOn && tier >= 2 && (
                  <div className="mt-3 grid gap-3 pl-11 sm:grid-cols-2">
                    <Field label="Only these namespaces" hint="Empty: all.">
                      <TagsInput value={inc} onChange={setInc} placeholder="shop payments" data-testid="scope-include" />
                    </Field>
                    <Field label="Never these" hint="Wins over the other two.">
                      <TagsInput value={exc} onChange={setExc} placeholder="hr-data" data-testid="scope-exclude" />
                    </Field>
                    <Field label="Or namespaces with this label" hint="key=value, on a label starting ikhnos.io/; added to the names on the left.">
                      <Input value={sel} onChange={(e) => setSel(e.target.value)} placeholder="ikhnos.io/scope=yes" data-testid="scope-label" />
                    </Field>
                    <p className="self-end text-xs text-nb-500">
                      {scopeActive(scope) ? 'The install command below will carry this scope.' : 'Nothing narrowed yet: the agent would report every namespace.'}
                    </p>
                    {problems.length > 0 && <p role="alert" className="text-xs text-bad sm:col-span-2">{problems.join('. ')}.</p>}
                  </div>
                )}
              </OptionRow>
            </section>
          )}
          {error && <ErrorBanner>{error}</ErrorBanner>}
        </div>
      )}

      {created && phase !== 'form' && (
        <div className="space-y-5">
          {(phase === 'waiting' || phase === 'approve') && (
            <AfterRun collapsed={phase === 'approve'}>
              <Tabs
                label="How to install the agent"
                className="mb-3"
                value={via}
                onChange={setVia}
                items={[{ id: 'helm', label: 'helm', testId: 'install-tab-helm' }, { id: 'controller', label: 'kubectl (controller)', testId: 'install-tab-controller' }]}
              />
              {via === 'helm' ? (
                <>
                  <div className="mb-1 flex items-center justify-between text-sm text-nb-300">
                    <span>Run this where you use <code className="font-mono text-xs">kubectl</code> and <code className="font-mono text-xs">helm</code> for the cluster</span>
                    <CopyButton text={created.install} label="Copy command" />
                  </div>
                  <pre className="whitespace-pre-wrap break-all rounded-md border border-nb-850 bg-nb-950 px-3 py-3 font-mono text-xs leading-relaxed text-nb-300" data-testid="install-command">{created.install}</pre>
                </>
              ) : (
                <ControllerSteps c={created.controller} />
              )}
              <p className="mt-2 text-xs text-nb-500">
                {/nodeProbe\.enabled=true/.test(created.install) && (
                  <>The node probe mounts the host's <code className="font-mono">/sys</code> read-only. If the namespace enforces the Pod Security “baseline” profile, its pods are refused until you run <code className="font-mono">kubectl label namespace ikhnos-system pod-security.kubernetes.io/enforce=privileged --overwrite</code>. </>
                )}
                {/flowObserver\.enabled=true/.test(created.install) && (
                  <>The traffic observer runs as root with only CAP_BPF, CAP_PERFMON and CAP_SYS_RESOURCE and an unconfined seccomp profile, in the host network. A namespace that enforces the Pod Security “baseline” profile refuses it until you run <code className="font-mono">kubectl label namespace ikhnos-system pod-security.kubernetes.io/enforce=privileged --overwrite</code>. </>
                )}
                The token works once and expires at {new Date(created.meta.expiresAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}. It is shown only here and cannot be retrieved later.
              </p>
              {via === 'helm' && !imagesConfigured && <div className="mt-3"><NoRegistryNotice newTab /></div>}
              {via === 'helm' && (chartFile || !imagesConfigured) && (
                <div className="mt-3 rounded-md border border-nb-850 bg-nb-930 px-3 py-2.5 text-xs text-nb-400" data-testid="install-prereq">
                  <div className="mb-1 font-medium text-nb-200">Before you run it, on the machine that runs helm</div>
                  <ol className="list-decimal space-y-1.5 pl-4">
                    {chartFile && (
                      <li>
                        Get the chart: <a className="text-accent hover:underline" href={`${baseUrl}/charts/${chartFile}`} download data-testid="chart-download">download {chartFile}</a> and put it in the folder you run the command from (the command refers to <code className="font-mono">./{chartFile}</code>). From a shell that can reach this server:
                        <code className="mt-1 block break-all rounded border border-nb-850 bg-nb-950 px-2 py-1 font-mono text-[11px] text-nb-300">curl -fLO {shArg(`${baseUrl || window.location.origin}/charts/${chartFile}`)}</code>
                      </li>
                    )}
                    {!imagesConfigured && (
                      <li>
                        Make the image <code className="font-mono">{img.repository}</code> available to the cluster: set a registry above, or for a local cluster load it directly — <code className="font-mono">k3s ctr images import</code> (k3s), <code className="font-mono">kind load docker-image</code> (kind).
                      </li>
                    )}
                  </ol>
                </div>
              )}
              {via === 'helm' && chartRef && (
                <details className="mt-3 rounded-md border border-nb-850 bg-nb-930 px-3 py-2.5 text-xs text-nb-400" data-testid="install-source">
                  <summary className="cursor-pointer font-medium text-nb-200">{imagesConfigured ? 'Nothing to download or build: this pulls everything from your registry' : 'Where this command pulls the chart from'}</summary>
                  <ul className="mt-2 list-disc space-y-1 pl-4">
                    <li>The chart: <code className="font-mono">{chartRef}</code>, version {info?.install?.chartVersion}.</li>
                    {imagesConfigured && (
                      <li data-testid="install-image">
                        The image: <code className="break-all font-mono">{img.reference}</code>
                        {img.digest ? (
                          <span className="ml-1.5 inline-flex items-center gap-1 rounded border border-ok/30 bg-ok/10 px-1.5 py-px align-middle text-[11px] text-ok" data-testid="image-pinned" title="Pinned by digest: every install pulls exactly this image">
                            <Pin size={ICON_SM} aria-hidden /> pinned
                          </span>
                        ) : (
                          <span className="block text-nb-500" data-testid="image-mutable">
                            {img.tag ? 'The tag is mutable' : 'The chart’s own version tag is mutable'}: pin a digest in <SettingsLink newTab /> for reproducible installs.
                          </span>
                        )}
                        <span className="block">It must be public, or the cluster must be able to authenticate to the registry.</span>
                      </li>
                    )}
                  </ul>
                  <details className="mt-1.5">
                    <summary className="cursor-pointer text-nb-300">If helm says not found or unauthorized</summary>
                    <p className="mt-1">
                      They have not been published yet, or the registry is private. Ask whoever manages this Ikhnos server to publish them (see the deployment guide) or grant the cluster's registry credentials access.
                      {info?.install?.chartFile && (
                        <> To skip the registry for just the chart, use the copy this server already serves: <code className="font-mono">curl -fLO {shArg(`${baseUrl || window.location.origin}/charts/${info.install.chartFile}`)}</code>, then change the start of the command to <code className="font-mono">helm install ikhnos-agent ./{info.install.chartFile}</code> and drop <code className="font-mono">--version</code>.</>
                      )}
                    </p>
                  </details>
                </details>
              )}
            </AfterRun>
          )}

          {phase === 'waiting' && (
            <div>
              <p className="flex items-center gap-2 text-sm text-nb-400">
                <Loader2 size={ICON_SM} className="animate-spin text-accent" /> Waiting for the agent to start and connect. This can take a minute.
              </p>
              {stalled && <StillWaiting via={via} created={created} />}
            </div>
          )}
          {phase === 'approve' && mine && <ApprovalCard agent={mine} showAccess={false} />}
          {phase === 'discovering' && (
            <p className="flex items-center gap-2 text-sm text-nb-400">
              <Loader2 size={ICON_SM} className="animate-spin text-accent" />
              {discovery.state === 'discovering' ? `Approved. Looking at the cluster… ${discovery.synced} of ${discovery.total} watches read so far.` : 'Approved. Waiting for the first discovery…'}
            </p>
          )}
          {phase === 'done' && counts && (
            <>
              <p className="flex items-center gap-2 text-sm text-ok">
                <CheckCircle2 size={ICON_SM} /> Connected. Found {counts.nodes} {counts.nodes === 1 ? 'node' : 'nodes'} and {counts.services} {counts.services === 1 ? 'service' : 'services'}. Review the grouping suggestions in the Discovery inbox.
              </p>
              {cluster && !effective(cluster).siteId && <WhereIsThis cluster={cluster} />}
            </>
          )}
          {phase === 'stopped' && <p className="text-sm text-warn">This enrollment was {agent?.status}{agent?.reason ? ` (${agent.reason})` : ''}. Create a new install command to try again.</p>}
        </div>
      )}
    </Modal>
  )
}
