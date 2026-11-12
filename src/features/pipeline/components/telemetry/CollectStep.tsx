import clsx from 'clsx'
import { useState } from 'react'
import { Bug, Layers, Leaf, Server, type LucideIcon } from 'lucide-react'
import { Field, ICON_SM, InfoTip, Input, Select } from '@/components/ui/primitives'
import { Switch } from '@/components/ui/Switch'
import { CopyCommand } from '@/components/ui/CopyCommand'
import { LINK_REQUIREMENT } from '@/lib/clusterLinks'
import { otlpAddress } from '@/lib/exportHealth'
import { applyIntentPreset, PICKABLE_SIGNALS, TELEMETRY_INTENT_PRESETS, TELEMETRY_SIGNALS } from '@/lib/consent'
import { isKept, type TelemetryInput } from '@/lib/install'
import { LAYER_CARDS, type Layer } from '@/features/pipeline/lib/telemetryLayers'

export type SignalId = 'resourceUsage' | 'energy' | 'kubernetesState' | 'nodeRuntime' | 'networkLatency' | 'clusterLinks' | 'applicationMetrics' | 'systemLogs' | 'kubernetesEvents' | 'applicationLogs' | 'traces' | 'accelerators'

/**
 * Energy has no question to answer: turning it on deploys the bundled Kepler, like every other signal deploys what it needs. The one
 * exception is a configuration that already scrapes an existing Kepler (an install made before the choice was removed, or by the chart's
 * own flags): for that one the source and its endpoint stay editable, so nothing about it changes behind the person's back. Whether to
 * show them is decided once, when this mounts (energy turned on, or the draft loaded), so switching such a configuration to the bundled
 * Kepler does not make the picker vanish under the person's hand.
 */
export function EnergyFields({ value, onChange, testIdPrefix }: { value: TelemetryInput; onChange: (v: TelemetryInput) => void; testIdPrefix: string }) {
  const [offered] = useState(() => value.energySource === 'existing')
  if (!offered) return null
  const set = <K extends keyof TelemetryInput>(key: K, v: TelemetryInput[K]) => onChange({ ...value, [key]: v })
  return (
    <div className="grid gap-3 border-t border-nb-850 pt-3 sm:grid-cols-2">
      <Field label="Energy source">
        <Select
          value={value.energySource}
          onChange={(e) => set('energySource', e.target.value as TelemetryInput['energySource'])}
          data-testid={`${testIdPrefix}-energy-source`}
        >
          <option value="bundle-kepler">Deploy Kepler (privileged, one pod per node)</option>
          <option value="existing">Scrape one I already run</option>
        </Select>
      </Field>
      {value.energySource === 'existing' && (
        <Field label="Its Prometheus endpoint" hint="Host and port only. The collector adds /metrics itself; a scheme or a path stops it from starting.">
          <Input
            value={value.energyExistingEndpoint}
            onChange={(e) => set('energyExistingEndpoint', e.target.value)}
            placeholder="kepler.monitoring:9102"
            data-testid={`${testIdPrefix}-energy-endpoint`}
          />
        </Field>
      )}
    </div>
  )
}

/**
 * The agents report which signals are on but not the cluster-link options, so a draft opened on an installed agent cannot know them. The boxes
 * then show their defaults, and an empty box would read as "off": while the draft still leaves a group as installed they are drawn undetermined
 * and say so. Changing any box of the group makes it the draft's own (and states all of it).
 */
function KeptNote({ testId, children }: { testId: string; children: string }) {
  return (
    <p className="rounded-md border border-nb-850 bg-nb-930 px-2.5 py-1.5 text-xs text-nb-500" data-testid={testId}>
      {children}
    </p>
  )
}

const undetermined = (on: boolean) => (el: HTMLInputElement | null) => {
  if (el) el.indeterminate = on
}

/**
 * Cluster links' own choices, shown once `clusterLinks` is on. They decide what this cluster is able to do; which pairs of clusters actually
 * run what is chosen afterwards, per pair, on the Sites page (nothing is measured until an administrator pairs this cluster).
 */
export function ClusterLinksFields({ value, onChange, testIdPrefix }: { value: TelemetryInput; onChange: (v: TelemetryInput) => void; testIdPrefix: string }) {
  const set = <K extends keyof TelemetryInput>(key: K, v: TelemetryInput[K]) => onChange({ ...value, [key]: v })
  const optionsKept = isKept(value, 'clusterLinksOptions')
  return (
    <div className="space-y-3 border-t border-nb-850 pt-3">
      <p className="text-xs text-nb-500" data-testid={`${testIdPrefix}-links-requirement`}>{LINK_REQUIREMENT}</p>
      {optionsKept && (
        <KeptNote testId={`${testIdPrefix}-links-kept`}>
          {value.clusterLinksRunning
            ? 'The boxes show what runs now, read from the cluster. Your own iperf3 credentials Secret, if any, is not reported, so the command leaves all of this as it is. Changing either box states both, with no Secret of your own unless you name one.'
            : 'As installed, not reported: the agents do not say whether probing and throughput tests are on, so the command leaves them as they are. Changing either box states both.'}
        </KeptNote>
      )}
      <label className="flex cursor-pointer items-start gap-2.5 text-sm">
        <input type="checkbox" className="mt-0.5 size-4 accent-[var(--color-accent)]" ref={undetermined(optionsKept && !value.clusterLinksRunning)} checked={value.clusterLinksProbe} onChange={(e) => set('clusterLinksProbe', e.target.checked)} data-testid={`${testIdPrefix}-links-probe`} />
        <span>
          <span className="text-nb-300">Probing</span>
          <span className="block text-xs text-nb-500">Small packets in both directions: delay, jitter and loss per direction, the path’s largest packet size and route changes. Runs a pod of its own and a responder on every node. The clusters of a pair must reach each other’s nodes on TCP 8861 and UDP 18900–18919 with no address translation (NAT) in between, on the same ports in every cluster. Without it only the real traffic is watched.</span>
        </span>
      </label>
      {value.clusterLinksProbe && (
        <label className="flex cursor-pointer items-start gap-2.5 text-sm">
          <input type="checkbox" className="mt-0.5 size-4 accent-[var(--color-accent)]" ref={undetermined(optionsKept && !value.clusterLinksRunning)} checked={value.clusterLinksThroughput} onChange={(e) => set('clusterLinksThroughput', e.target.checked)} data-testid={`${testIdPrefix}-links-throughput`} />
          <span>
            <span className="text-nb-300">Throughput tests</span>
            <span className="block text-xs text-nb-500">Lets this cluster run and answer iperf3 tests, which fill the link for a few seconds. They run only on the pairs that ask for them (on Sites, adding a pair between two clusters that both have this ticked offers them ticked), within a daily budget, and wait while the link is busy.</span>
          </span>
        </label>
      )}
      {value.clusterLinksProbe && value.clusterLinksThroughput && (
        <details className="rounded-lg border border-nb-850 px-3 py-2 text-sm" data-testid={`${testIdPrefix}-links-own-credentials`} open={value.clusterLinksAuthSecret !== ''}>
          <summary className="cursor-pointer text-nb-300">Advanced: use my own iperf3 credentials</summary>
          <div className="pt-2">
            <Field label="Secret with the iperf3 credentials (optional)" hint="Leave empty: Ikhnos issues the credentials, keeps them in memory in each cluster and rotates them on request, so there is nothing to create. To use your own, name a Secret that exists in every cluster of the mesh, with the keys private.pem, public.pem, users.csv, username and password.">
              <Input value={value.clusterLinksAuthSecret} onChange={(e) => set('clusterLinksAuthSecret', e.target.value)} placeholder="ikhnos-link-iperf" data-testid={`${testIdPrefix}-links-secret`} />
            </Field>
          </div>
        </details>
      )}
    </div>
  )
}

/** Accelerators' own source picker and "apply the install's scope to GPU metrics" toggle, shown once `accelerators` is on. */
export function AcceleratorsFields({ value, onChange, testIdPrefix }: { value: TelemetryInput; onChange: (v: TelemetryInput) => void; testIdPrefix: string }) {
  const set = <K extends keyof TelemetryInput>(key: K, v: TelemetryInput[K]) => onChange({ ...value, [key]: v })
  const accelerators = TELEMETRY_SIGNALS.find((s) => s.id === 'accelerators')
  return (
    <div className="grid gap-3 border-t border-nb-850 pt-3 sm:grid-cols-2">
      <Field label="Accelerators source">
        <Select
          value={value.acceleratorsSource}
          onChange={(e) => set('acceleratorsSource', e.target.value as TelemetryInput['acceleratorsSource'])}
          data-testid={`${testIdPrefix}-accelerators-source`}
        >
          <option value="bundle-dcgm">Deploy dcgm-exporter (GPU nodes only, needs the NVIDIA driver + Container Toolkit already on the node)</option>
          <option value="existing">Scrape one I already run</option>
        </Select>
      </Field>
      {value.acceleratorsSource === 'existing' && (
        <Field label="Its Prometheus endpoint" hint="Host and port only. The collector adds /metrics itself; a scheme or a path stops it from starting.">
          <Input
            value={value.acceleratorsExistingEndpoint}
            onChange={(e) => set('acceleratorsExistingEndpoint', e.target.value)}
            placeholder="dcgm-exporter.monitoring:9400"
            data-testid={`${testIdPrefix}-accelerators-endpoint`}
          />
        </Field>
      )}
      {accelerators?.namespaceScopable && (
        <label className="flex cursor-pointer items-start gap-2.5 text-sm sm:col-span-2">
          <input
            type="checkbox"
            className="mt-0.5 size-4 accent-[var(--color-accent)]"
            checked={value.acceleratorsApplyScope}
            onChange={(e) => set('acceleratorsApplyScope', e.target.checked)}
            data-testid={`${testIdPrefix}-accelerators-apply-scope`}
          />
          <span>
            <span className="text-nb-300">Apply the install's namespace scope to GPU metrics</span>
            <InfoTip>Off by default. Accelerators are deployed per node (infrastructure), but GPU metrics can carry the namespace/pod using the GPU - turning this on asks dcgm-exporter to attach that identity, so the install's namespace scope narrows GPU metrics the same way it narrows application data.</InfoTip>
            <span className="block text-xs text-nb-500">Only takes effect while deploying dcgm-exporter above, not when scraping one you already run.</span>
          </span>
        </label>
      )}
    </div>
  )
}

/** One line of what a signal collects, for its row: the fuller account and what it needs are behind the info tip. */
const SUMMARY: Record<string, string> = {
  resourceUsage: 'CPU, memory, disk and network per node and container.',
  energy: 'Power draw per node and pod.',
  kubernetesState: 'Pod, deployment and replica status.',
  nodeRuntime: 'Pod lifecycle and volume metrics.',
  clusterLinks: 'Delay, jitter and loss to paired clusters.',
  accelerators: 'GPU use, memory, temperature and power.',
  systemLogs: "Each node's operating system and runtime logs.",
  kubernetesEvents: 'Cluster events.',
  applicationMetrics: 'Pushed over OTLP or scraped from Prometheus.',
  applicationLogs: 'Logs your applications push over OTLP.',
  traces: 'Traces your applications push over OTLP.',
}

/** What sets a signal apart from the rest, said on its row: only energy asks for more than reading. */
const TAG: Record<string, string> = { energy: 'privileged pod' }

/** One signal as a row: its name, one line of what it collects (the fuller account and the access it needs behind the info tip), and a switch. */
export function SignalRow({
  signal,
  checked,
  onChange,
  testIdPrefix,
}: {
  signal: (typeof TELEMETRY_SIGNALS)[number]
  checked: boolean
  onChange: (v: boolean) => void
  testIdPrefix: string
}) {
  const tag = TAG[signal.id]
  return (
    <label className="flex cursor-pointer items-center gap-3 px-4 py-2.5">
      <span className="min-w-0 flex-1 text-sm">
        <span className={clsx('font-medium', checked ? 'text-nb-200' : 'text-nb-300')}>{signal.label}</span>
        {tag && <span className="ml-2 rounded border border-warn/40 px-1.5 py-px text-[11px] text-warn">{tag}</span>}
        <span className="ml-2 text-nb-500">{SUMMARY[signal.id] ?? signal.what}</span>
        <InfoTip>{`${signal.what} Access: ${signal.permissions}`}</InfoTip>
      </span>
      <Switch testId={`${testIdPrefix}-${signal.id}`} checked={checked} onChange={onChange} label={signal.label} />
    </label>
  )
}

type Modality = 'metrics' | 'logs' | 'traces'
const MODALITY_ORDER: Modality[] = ['metrics', 'logs', 'traces']
const MODALITY_LABEL: Record<Modality, string> = { metrics: 'Metrics', logs: 'Logs', traces: 'Traces' }
const LAYER_LABEL: Record<Layer, string> = { infrastructure: 'Infrastructure', application: 'Your applications' }

const PRESET_ICON: Record<string, LucideIcon> = { minimal: Leaf, 'full-infra': Server, 'full-infra-app': Layers, 'debug-everything': Bug }

const sameSet = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x))

/**
 * Everything this wizard can collect, on one screen and always in view: four starting points as cards, then every signal as one row with a
 * switch, grouped by layer (infrastructure, then your applications). A starting point sets the whole list in one click and the switches adjust
 * it. The few choices that belong to one signal (the source behind energy or GPU, the cluster-link options) open under its row while it is on.
 *
 * Every switch writes straight into `value`, so leaving the screen never loses a change.
 */
export default function CollectStep({
  value,
  onChange,
  testIdPrefix,
  namespace,
}: {
  value: TelemetryInput
  onChange: (v: TelemetryInput) => void
  testIdPrefix: string
  /** The namespace the agent is installed in, which the address applications send to is in. */
  namespace?: string
}) {
  const rec = value as unknown as Record<string, boolean>
  const on = PICKABLE_SIGNALS.filter((s) => rec[s.id])
  const onIds = on.map((s) => s.id)
  const setOne = (id: string, v: boolean) => onChange({ ...value, [id]: v } as TelemetryInput)

  return (
    <div className="space-y-5" data-testid={`${testIdPrefix}-guided-step-collect`}>
      <div className="grid gap-2 sm:grid-cols-2" role="group" aria-label="Start from" data-testid={`${testIdPrefix}-collect-presets`}>
        {TELEMETRY_INTENT_PRESETS.map((p) => {
          const active = sameSet(onIds, p.signals)
          const Icon = PRESET_ICON[p.id] ?? Layers
          return (
            <button
              key={p.id}
              type="button"
              aria-pressed={active}
              onClick={() => onChange(applyIntentPreset(value, p))}
              data-testid={`${testIdPrefix}-collect-preset-${p.id}`}
              className={clsx('flex items-start gap-3 rounded-lg border px-4 py-3 text-left transition-colors', active ? 'border-accent/60 bg-accent-soft' : 'border-nb-850 bg-nb-925 hover:border-nb-800')}
            >
              <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-nb-850 bg-nb-930 text-nb-500" aria-hidden>
                <Icon size={ICON_SM} />
              </span>
              <span className="min-w-0">
                <span className="block text-sm font-medium text-nb-300">{p.label}</span>
                <span className="block text-sm text-nb-500">{p.description}</span>
              </span>
            </button>
          )
        })}
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between text-xs">
          <span className="font-medium uppercase tracking-wide text-nb-500">Signals</span>
          <span className="text-nb-500" data-testid={`${testIdPrefix}-collect-count`}>{on.length} of {PICKABLE_SIGNALS.length}</span>
        </div>
        {LAYER_CARDS.map((layer: Layer) => (
          <section key={layer} className="overflow-hidden rounded-lg border border-nb-850 bg-nb-925" data-testid={`${testIdPrefix}-collect-layer-${layer}`} aria-label={LAYER_LABEL[layer]}>
            <h3 className="border-b border-nb-850 bg-nb-930 px-4 py-1.5 text-[11px] font-medium uppercase tracking-wide text-nb-500">{LAYER_LABEL[layer]}</h3>
            <div className="divide-y divide-nb-850">
              {MODALITY_ORDER.map((m) => {
                const signals = PICKABLE_SIGNALS.filter((s) => s.layer === layer && s.modality === m)
                if (signals.length === 0) return null
                const count = signals.filter((s) => rec[s.id]).length
                const base = `${testIdPrefix}-collect-${layer}-${m}`
                return (
                  <div key={m} className="divide-y divide-nb-850" data-testid={base}>
                    {signals.length > 1 && (
                      <div className="flex items-center gap-2 px-4 pt-2 pb-1 text-xs text-nb-500">
                        {MODALITY_LABEL[m]}
                        <span className="text-nb-600" data-testid={`${base}-count`}>{count} of {signals.length}</span>
                      </div>
                    )}
                    {signals.map((s) => (
                      <div key={s.id}>
                        <SignalRow signal={s} checked={rec[s.id]} onChange={(v) => setOne(s.id, v)} testIdPrefix={testIdPrefix} />
                        {s.id === 'energy' && value.energy && <div className="px-4 pb-3 empty:hidden"><EnergyFields value={value} onChange={onChange} testIdPrefix={testIdPrefix} /></div>}
                        {s.id === 'clusterLinks' && value.clusterLinks && <div className="px-4 pb-3 empty:hidden"><ClusterLinksFields value={value} onChange={onChange} testIdPrefix={testIdPrefix} /></div>}
                        {s.id === 'accelerators' && value.accelerators && <div className="px-4 pb-3 empty:hidden"><AcceleratorsFields value={value} onChange={onChange} testIdPrefix={testIdPrefix} /></div>}
                      </div>
                    ))}
                  </div>
                )
              })}
            </div>
          </section>
        ))}
      </div>

      {on.some((s) => s.layer === 'application') && (
        <div className="space-y-1.5 rounded-lg border border-nb-850 bg-nb-925 px-4 py-3 text-sm text-nb-400" data-testid={`${testIdPrefix}-collect-otlp`}>
          <span>Point your applications' OTLP exporter at this address (gRPC; HTTP is port 4318 on the same name).</span>
          <CopyCommand text={otlpAddress(namespace ?? 'ikhnos-system').grpc} label="Copy the address (gRPC)" testId={`${testIdPrefix}-collect-otlp-address`} />
        </div>
      )}
    </div>
  )
}
