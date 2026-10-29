import { parseRoutedDestination, scopeWords1, seedTelemetryFromInstalled, type AgentDiagnostics } from '../../../lib/consent'
import { ROUTE_MODALITIES, TELEMETRY_SIGNALS, type Modality } from '../../../lib/install'
import { operatorReceiverEndpoint } from '../../../lib/operatorReceiver'
import type { OperatorDestination, TelemetryIntent } from '../../../lib/types'
import { intentScope } from './operatorIntent'

/*
 * What a local operator was asked to do, held against what its agent says is installed.
 *
 * The server keeps the request (the TelemetryIntent: which signals, which namespaces, where to send) and the agent reports the installation
 * (installedTelemetry, installedTelemetryConfig). Both are read from the agent's own release values, not from the running collectors, so a
 * difference here means "the install is not what was asked", never "the collectors are not doing what the install says". That second
 * comparison needs the controller to read the running ConfigMap, and is not made here.
 *
 * The intent has no time of its own for the last change, so a difference cannot be told to be a command not yet run from an install changed
 * afterwards: the words say both.
 */

type Config = NonNullable<AgentDiagnostics['installedTelemetryConfig']>

/** One thing that differs. `part` says which kind, for a caller that wants to group or filter. */
export interface IntentDifference {
  part: 'signals' | 'source' | 'destination' | 'scope'
  text: string
}

const signalLabel = (id: string) => TELEMETRY_SIGNALS.find((s) => s.id === id)?.label ?? id
const signalModality = (id: string): Modality | undefined => TELEMETRY_SIGNALS.find((s) => s.id === id)?.modality
/** A signal the chart has nothing that emits: never offered and never asked for, so it is no difference when one side lists it. */
const emitsNothing = (id: string) => !!TELEMETRY_SIGNALS.find((s) => s.id === id)?.noEmitter
const list = (ids: string[]) => ids.map(signalLabel).join(', ')

/** An address as it is compared: without a scheme or trailing slash, in lower case. */
const bare = (e: string) => e.trim().replace(/^[a-z][a-z0-9+.-]*:\/\//i, '').replace(/\/+$/, '').toLowerCase()

/** Every address a destination can be installed as: the endpoint recorded, an operator's receiver, and the address a sender was told to dial. */
function accepted(d: OperatorDestination): string[] {
  const all = [d.endpoint, d.address, d.kind === 'operator' && d.targetOperatorId ? operatorReceiverEndpoint({ id: d.targetOperatorId }) : undefined]
  return all.filter((x): x is string => !!x).map(bare)
}

const destinationWords = (d: OperatorDestination) => (d.kind === 'operator' && d.targetOperatorId ? `${d.targetOperatorId} (${d.address || d.endpoint})` : d.endpoint)

const MODALITY_WORDS: Record<Modality, string> = { metrics: 'metrics', logs: 'logs', traces: 'traces' }
const andList = (xs: string[]) => (xs.length <= 1 ? xs.join('') : `${xs.slice(0, -1).join(', ')} and ${xs[xs.length - 1]}`)

function signalDifferences(intent: TelemetryIntent, installed: string[]): IntentDifference[] {
  const asked = intent.signals.map((s) => s.id).filter((id) => !emitsNothing(id))
  const have = installed.filter((id) => !emitsNothing(id))
  const missing = asked.filter((id) => !have.includes(id))
  const extra = have.filter((id) => !asked.includes(id))
  const out: IntentDifference[] = []
  if (missing.length) out.push({ part: 'signals', text: `Asked for ${list(missing)}, which ${missing.length === 1 ? 'is' : 'are'} not installed` })
  if (extra.length) out.push({ part: 'signals', text: `Running ${list(extra)}, which ${extra.length === 1 ? 'was' : 'were'} not asked for` })
  return out
}

/** Energy and accelerators can read the bundled exporter or one the cluster already has; the agent says which, and the grant says which was asked. */
function sourceDifferences(intent: TelemetryIntent, installed: string[], config: Config): IntentDifference[] {
  const out: IntentDifference[] = []
  const words = (v: string) => (v.startsWith('bundle') ? 'the bundled exporter' : 'an existing one')
  for (const [id, have] of [['energy', config.energySource], ['accelerators', config.acceleratorsSource]] as const) {
    const grant = intent.signals.find((s) => s.id === id)
    if (!grant || !installed.includes(id) || !have || grant.source === 'builtin' || !grant.source) continue
    if (grant.source !== have) out.push({ part: 'source', text: `${signalLabel(id)} reads ${words(have)}; asked for ${words(grant.source)}` })
  }
  return out
}

/** Where each signal type goes, installed and asked, for the signal types that are running. */
function destinationDifferences(intent: TelemetryIntent, installed: string[], config: Config): IntentDifference[] {
  const installedRaw = config.exportEndpoint.trim()
  if (!installedRaw) return []
  const routed = parseRoutedDestination(installedRaw)
  const have = (m: Modality) => (routed ? (routed.routes[m] ?? routed.fallback) : installedRaw)
  // Whatever a signal type has no route of its own for goes to the default, which is what an empty `routes` means.
  const want = (m: Modality): OperatorDestination => intent.routes?.[m] ?? intent.destination
  const running = new Set(installed.map(signalModality).filter((m): m is Modality => !!m))
  const groups = new Map<string, { from: string; to: string; kinds: Modality[] }>()
  for (const m of ROUTE_MODALITIES) {
    if (!running.has(m)) continue
    const from = have(m)
    const d = want(m)
    if (!from || accepted(d).includes(bare(from))) continue
    const key = `${from}\u0000${destinationWords(d)}`
    const g = groups.get(key) ?? { from, to: destinationWords(d), kinds: [] }
    g.kinds.push(m)
    groups.set(key, g)
  }
  const everyKind = [...running].length
  return [...groups.values()].map((g) => ({
    part: 'destination' as const,
    text: `Sends ${g.kinds.length === everyKind ? 'everything' : andList(g.kinds.map((m) => MODALITY_WORDS[m]))} to ${g.from}; asked for ${g.to}`,
  }))
}

/** Where the application signals are collected from. The intent records one scope for them all (the rule that wrote it is `intentScope`), so
 *  the installed scopes are put through the same rule. Without the scope the agent reports there is nothing to compare. */
function scopeDifferences(intent: TelemetryIntent, installed: string[], config: Config): IntentDifference[] {
  if (!config.scope) return []
  const have = intentScope(seedTelemetryFromInstalled(installed, config))
  const asked = { namespaces: [...intent.namespaces].sort(), exclude: [...intent.exclude].sort() }
  const same = (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x === b[i])
  if (same(have.namespaces, asked.namespaces) && same(have.exclude, asked.exclude)) return []
  return [{ part: 'scope', text: `Collects from ${scopeWords1(have)}; asked for ${scopeWords1(asked)}` }]
}

/**
 * Everything that differs between the active intent and what is installed. Empty when there is no intent, nothing is installed yet (that is
 * "asked, not yet running", said elsewhere), or the two agree. The scope is compared only while the signals agree: with a signal more or
 * less, the scope of the others would differ for that reason alone and the first line already says so.
 */
export function compareIntent(intent: TelemetryIntent | undefined, installed: string[], config: Config | undefined): IntentDifference[] {
  if (!intent || installed.length === 0) return []
  const signals = signalDifferences(intent, installed)
  if (!config) return signals
  return [...signals, ...sourceDifferences(intent, installed, config), ...destinationDifferences(intent, installed, config), ...(signals.length === 0 ? scopeDifferences(intent, installed, config) : [])]
}

/** The differences as one sentence for a row's reason. */
export function differenceSentence(diffs: IntentDifference[]): string {
  return `${diffs.map((d) => d.text).join('. ')}.`
}
