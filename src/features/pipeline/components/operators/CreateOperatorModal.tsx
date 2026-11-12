import { ChevronRight } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import OperatorCreated from '@/features/pipeline/components/operators/OperatorCreated'
import { type useFusion } from '@/features/fusion/components/useFusion'
import { HEARTBEAT_WHAT } from '@/features/pipeline/components/operators/HeartbeatCommands'
import { EXPOSURE_OPTIONS } from '@/features/pipeline/components/operators/OperatorAddress'
import DestinationPicker, { useEnableAndUse } from '@/features/pipeline/components/telemetry/DestinationPicker'
import { TagRows } from '@/features/pipeline/components/telemetry/ProcessOptions'
import ProcessorEditor from '@/features/pipeline/components/telemetry/ProcessorEditor'
import { Button, ComboField, ErrorBanner, Field, ICON_SM, Input, Modal, SectionLabel } from '@/components/ui/primitives'
import { Switch } from '@/components/ui/Switch'
import { api, ApiError, type CreatedOperator, type OperatorExposure } from '@/lib/api'
import { buildDestinationCatalog, catalogOperators, destinationKey, fusionForCatalog, layoutDestinations, type DestinationCatalogEntry } from '@/features/pipeline/lib/destinationCatalog'
import { EXPORT_PRESETS, unsupportedDestinationNote } from '@/lib/exportPresets'
import { CENTRAL_OPERATOR_ID, fusionSentence, fusionUsable } from '@/features/fusion/lib/fusionStatus'
import { cleanTags, tagProblems, type TagEntry } from '@/lib/install'
import { operatorProcessorProblems } from '@/features/pipeline/lib/operatorInstall'
import type { ProcessorEntry } from '@/lib/processorCatalog'
import { processorSpec } from '@/lib/telemetrySpec'
import type { OperatorDestination, RegionalOperator } from '@/lib/types'
import { useServer } from '@/store/server'

const emptyDestination: OperatorDestination = { kind: 'external', endpoint: '', insecure: false, authHeaderName: '', authSecretName: '', authSecretKey: '' }

/** The destination of an operator that sends to another one: the central operator (FUSION) or a regional one. */
const operatorDestination = (id: string): OperatorDestination => ({ kind: 'operator', endpoint: '', targetOperatorId: id })

/** What is wrong with the destination draft, in words a person can act on. */
function destinationProblems(d: OperatorDestination): string[] {
  if (d.kind === 'operator') return d.targetOperatorId ? [] : ['Choose where it sends']
  return d.endpoint.trim() ? [] : ['Choose where it sends, or give an endpoint']
}

/** The whole create form's shape in one place. */
interface Draft {
  name: string
  sourceClusterIds: string[]
  destination: OperatorDestination
  extraProcessors: ProcessorEntry[]
  /** Name = value tags this operator stamps on everything it forwards (a region, an environment). Fixed at creation: they are
   *  part of the operator's own install. */
  labels: TagEntry[]
  /** Opt in to the heartbeat that lets this server say online/offline. On by default - it is the point of asking - but always
   *  stated next to the box, and sent explicitly either way. */
  heartbeat: boolean
  /** How clusters other than the operator's own reach it; decides the Service type in the install command. Node port unless
   *  changed: it works on every cluster, where a load balancer waits forever on one without a provider. */
  exposure: OperatorExposure
}

const ALL_SIGNALS = new Set(['metrics', 'logs', 'traces'] as const)

/**
 * "New operator", on one screen: its name, where it sends (picked from a list - FUSION first, then the other operators; any other
 * backend is one disclosure away), and how other clusters reach it, with the settings few people change under "Advanced". It is a
 * controlled dialog so the telemetry wizard can open it in place and pick the new operator afterwards: nothing underneath it is lost.
 * After creating, the same dialog turns into the commands to run (OperatorCreated).
 *
 * Which clusters send to it is not asked here: a cluster picks its operator in its own telemetry wizard (and the server issues its
 * certificate then). Clusters are carried in only when the dialog is opened for one (initialSourceClusterIds), as before.
 */
export default function CreateOperatorModal({
  operators,
  fusion,
  initialSourceClusterIds = [],
  onCreated,
  onConnect,
  onClose,
}: {
  /** The organisation's regional operators, for "send to another operator". */
  operators: RegionalOperator[]
  fusion: ReturnType<typeof useFusion>
  initialSourceClusterIds?: string[]
  /** Called as soon as the operator exists (not when this dialog closes), so the caller can reload its lists and pick it. */
  onCreated?: (created: CreatedOperator) => void
  /** Starts the telemetry wizard for a source cluster's agent with this operator chosen. */
  onConnect?: (agentId: string, operatorId: string) => void
  onClose: () => void
}) {
  const conn = useServer((s) => s.conn)
  const [draft, setDraft] = useState<Draft>({ name: '', sourceClusterIds: initialSourceClusterIds, destination: emptyDestination, extraProcessors: [], labels: [], heartbeat: true, exposure: 'nodeport' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [created, setCreated] = useState<{ result: CreatedOperator; exposure: OperatorExposure } | null>(null)
  const [otherOpen, setOtherOpen] = useState(false)
  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }))

  // The picker offers FUSION and the other operators; every other backend is the disclosure below it.
  const catalog = useMemo(() => {
    const full = buildDestinationCatalog({
      operators: catalogOperators({ operators, destinations: [], isAdmin: true }),
      enabledModalities: ALL_SIGNALS,
      isAdmin: true,
      fusion: fusionForCatalog({ status: fusion.status, operators, destinations: [], isAdmin: true }),
    })
    return { ...full, entries: full.entries.filter((e) => e.kind === 'fusion' || e.kind === 'operator') }
  }, [operators, fusion.status])
  const layout = layoutDestinations(catalog)
  const target = draft.destination.kind === 'operator' ? catalog.entries.find((e) => e.id === draft.destination.targetOperatorId) : undefined
  const activeKey = target ? destinationKey(target) : null

  const pick = (e: DestinationCatalogEntry) => {
    setOtherOpen(false)
    set({ destination: operatorDestination(e.id) })
  }
  const fusionControls = useEnableAndUse(
    catalog.entries,
    { busy: fusion.busy, error: fusion.error, enable: fusion.status?.available ? async () => void (await fusion.enable()) : undefined },
    pick,
  )

  // FUSION is the one thing worth picking for someone, once it can be sent to: preselected once, never while it is off.
  const preselected = useRef(false)
  const fusionRow = catalog.entries.find((e) => e.kind === 'fusion')
  const fusionReady = fusionRow?.kind === 'fusion' && fusionRow.fusion.usable
  useEffect(() => {
    if (preselected.current || !fusionReady) return
    preselected.current = true
    setDraft((d) => (d.destination.kind === 'external' && d.destination.endpoint === '' ? { ...d, destination: operatorDestination(CENTRAL_OPERATOR_ID) } : d))
  }, [fusionReady])

  const toCentral = draft.destination.kind === 'operator' && draft.destination.targetOperatorId === CENTRAL_OPERATOR_ID
  const note = draft.destination.kind === 'external' ? unsupportedDestinationNote(draft.destination.endpoint) : undefined
  const nameProblems = [draft.name.trim().length < 2 ? 'A name of at least two characters is required' : ''].filter(Boolean)
  const destProblems = [
    ...destinationProblems(draft.destination),
    toCentral && !fusionUsable(fusion.status) ? 'FUSION is not running, so nothing could receive this' : '',
  ].filter(Boolean)
  const advancedProblems = [...operatorProcessorProblems(draft.extraProcessors), ...tagProblems(draft.labels)]
  const problems = [...nameProblems, ...destProblems, ...advancedProblems]
  const canCreate = problems.length === 0 && !busy

  const create = async () => {
    const c = conn()
    if (!c || !canCreate) return
    setBusy(true)
    setError('')
    try {
      const r = await api.createOperator(c, draft.name.trim(), draft.sourceClusterIds, draft.destination, { heartbeat: draft.heartbeat, labels: cleanTags(draft.labels), exposure: draft.exposure, ...(draft.extraProcessors.length ? { extraProcessors: draft.extraProcessors.map(processorSpec) } : {}) })
      setCreated({ result: r, exposure: draft.exposure })
      onCreated?.(r)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not create the regional operator.')
    } finally {
      setBusy(false)
    }
  }

  if (created) {
    return (
      <OperatorCreated
        created={created.result}
        exposure={created.exposure}
        fusionKind={fusionSentence(fusion.status).kind}
        onConnect={onConnect ? (agentId) => onConnect(agentId, created.result.operator.id) : undefined}
        onClose={onClose}
      />
    )
  }

  const exposure = EXPOSURE_OPTIONS.find((o) => o.id === draft.exposure)

  return (
    <Modal
      open
      onClose={onClose}
      // While the request is out the credentials are being minted: closing now would lose them (the server shows them once, to this very call).
      dismissible={!busy}
      title="New operator"
      description="It gathers what its source clusters export and passes it on to one destination."
      width="max-w-2xl"
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>Cancel</Button>
          <Button variant="primary" onClick={() => void create()} disabled={!canCreate} data-testid="operator-create">{busy ? 'Creating…' : 'Create operator'}</Button>
        </>
      }
    >
      {/* No implicit submission. Enter in a field of this form (a label, a processor setting) used to submit all of it through the hidden button
          below, creating the operator and minting its credentials from a stray keypress; the only way to create is the Create button. */}
      <form
        className="space-y-5"
        onSubmit={(e) => e.preventDefault()}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && e.target instanceof HTMLInputElement) e.preventDefault() // a text area keeps its new line; a button its click
        }}
      >
        <div data-testid="operator-step-sources">
          <Field label="Name"><Input value={draft.name} onChange={(e) => set({ name: e.target.value })} maxLength={80} autoFocus data-testid="operator-name" /></Field>
        </div>

        <div className="space-y-2" data-testid="operator-step-destination">
          <SectionLabel>Sends to</SectionLabel>
          <DestinationPicker
            catalog={catalog}
            layout={layout}
            activeKey={activeKey}
            onPick={pick}
            testIdPrefix="operator"
            fusion={fusionControls}
            search={false}
            emptyText="There is no other operator to send to. Use another backend below."
          />
          <details className="group rounded-lg border border-nb-850" open={otherOpen} onToggle={(e) => setOtherOpen(e.currentTarget.open)} data-testid="operator-other-backend">
            <summary className="flex cursor-pointer select-none items-center gap-1.5 px-3 py-2.5 text-sm text-nb-300 marker:content-none">
              <ChevronRight size={ICON_SM} className="text-nb-500 transition-transform group-open:rotate-90" aria-hidden />
              Not listed? Another backend
            </summary>
            <div className="grid gap-3 border-t border-nb-850 p-3 sm:grid-cols-2">
              <Field label="Send aggregated telemetry to" hint="Pick a known backend to fill in its endpoint pattern and credential header, or type your own." className="sm:col-span-2">
                <ComboField
                  value={draft.destination.kind === 'external' ? draft.destination.endpoint : ''}
                  onChange={(v) => {
                    const preset = EXPORT_PRESETS.find((p) => p.endpointPattern === v)
                    const base = draft.destination.kind === 'external' ? draft.destination : emptyDestination
                    fusionControls?.cancel?.() // typed by hand: a pending "Enable and use" must not replace it later
                    set({ destination: { ...base, endpoint: v, authHeaderName: preset?.headerName ? preset.headerName : base.authHeaderName } })
                  }}
                  placeholder="otel-gateway.example.com:4317"
                  options={EXPORT_PRESETS.map((p) => ({ value: p.endpointPattern, label: p.label }))}
                />
              </Field>
              {note && <p role="alert" className="text-xs text-warn sm:col-span-2">{note}</p>}
              <label className="flex cursor-pointer items-center gap-2 text-sm sm:col-span-2">
                <input type="checkbox" className="size-4 accent-[var(--color-accent)]" checked={!!draft.destination.insecure} onChange={(e) => set({ destination: { ...draft.destination, kind: 'external', insecure: e.target.checked } })} data-testid="operator-export-insecure" />
                <span className="text-nb-300">Send without TLS (plain connection)</span>
              </label>
              <Field label="Credential header" hint="Which header the destination expects its credential in.">
                <Input value={draft.destination.authHeaderName ?? ''} onChange={(e) => set({ destination: { ...draft.destination, kind: 'external', authHeaderName: e.target.value } })} placeholder="Authorization" />
              </Field>
              <Field label="Secret holding it" hint="Just its name. You create the Secret in the release namespace; the credential itself never goes through this page.">
                <Input value={draft.destination.authSecretName ?? ''} onChange={(e) => set({ destination: { ...draft.destination, kind: 'external', authSecretName: e.target.value } })} placeholder="telemetry-export-token" />
              </Field>
            </div>
          </details>
          {toCentral && (
            <p className="text-sm text-nb-500" data-testid="operator-central">
              FUSION is the one door into this server&apos;s own stores: Prometheus for metrics, Loki for logs, Tempo for traces. It is reached over mutual TLS, with a client certificate issued when you create this operator.
            </p>
          )}
        </div>

        <fieldset data-testid="operator-exposure">
          <legend className="mb-2 text-xs font-medium uppercase tracking-wide text-nb-500">How other clusters reach it</legend>
          <div className="flex overflow-hidden rounded-lg border border-nb-850" role="radiogroup" aria-label="How other clusters reach it">
            {EXPOSURE_OPTIONS.map((o) => {
              const on = draft.exposure === o.id
              return (
                <label key={o.id} className={clsx('flex-1 cursor-pointer border-r border-nb-850 px-3 py-2 text-center text-sm transition-colors last:border-r-0 focus-within:ring-2 focus-within:ring-inset focus-within:ring-accent/60', on ? 'bg-accent-soft text-nb-200' : 'bg-nb-925 text-nb-400 hover:text-nb-300')}>
                  <input type="radio" name="operator-exposure" className="sr-only" checked={on} onChange={() => set({ exposure: o.id })} data-testid={`operator-exposure-${o.id}`} />
                  {o.label}
                </label>
              )
            })}
          </div>
          <p className="mt-2 text-sm text-nb-500">{exposure?.hint}</p>
          {draft.exposure === 'nodeport' && (
            <p className="mt-1 text-sm text-nb-500" data-testid="operator-exposure-note">
              Node port opens one port, 4317, on every node of the cluster you install it in. Senders must still present a certificate from this server.
            </p>
          )}
        </fieldset>

        <details className="group rounded-lg border border-nb-850" data-testid="operator-advanced">
          <summary className="flex cursor-pointer select-none items-center gap-1.5 px-3 py-2.5 text-sm text-nb-300 marker:content-none">
            <ChevronRight size={ICON_SM} className="text-nb-500 transition-transform group-open:rotate-90" aria-hidden />
            Advanced
          </summary>
          <div className="space-y-4 border-t border-nb-850 p-3">
            <label className="flex cursor-pointer items-start gap-3">
              <span className="min-w-0 flex-1 text-sm">
                <span className="text-nb-300">Report this operator&apos;s health to this server</span>
                <span className="mt-0.5 block text-xs leading-relaxed text-nb-500" data-testid="operator-heartbeat-explain">
                  Sends {HEARTBEAT_WHAT} It is the only thing this operator ever sends to this server, so this page can show online or offline. You can also turn it on later.
                </span>
              </span>
              <Switch testId="operator-heartbeat" checked={draft.heartbeat} onChange={(heartbeat) => set({ heartbeat })} label="Report this operator's health to this server" />
            </label>
            <div>
              <div className="mb-1 text-xs font-medium uppercase tracking-wide text-nb-500">Labels</div>
              <p className="mb-2 text-xs leading-relaxed text-nb-500" data-testid="operator-labels-explain">
                Added to every metric, log and trace it forwards, so the data can be told apart downstream (a region, an environment). They cannot be changed afterwards.
              </p>
              <TagRows tags={draft.labels} onChange={(labels) => set({ labels })} testIdPrefix="operator-label" noun="label" />
            </div>
            <div>
              <div className="mb-2 text-xs font-medium uppercase tracking-wide text-nb-500">Extra processors</div>
              <ProcessorEditor entries={draft.extraProcessors} onChange={(extraProcessors) => set({ extraProcessors })} testIdPrefix="operator" />
            </div>
          </div>
        </details>

        {/* What is still missing, said quietly: it is the state of an empty form, not an error, so it is neither red nor announced as an alert. */}
        {problems.length > 0 && <p className="text-xs text-nb-500" data-testid="operator-problems">{problems.join('. ')}.</p>}
        {error && <ErrorBanner>{error}</ErrorBanner>}
      </form>
    </Modal>
  )
}
